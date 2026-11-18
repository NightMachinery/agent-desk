package stt

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Paseo sends dictation through the running local daemon. The local credential
// is used only with the daemon endpoint recorded in its own runtime file.
func Paseo(home string) Backend {
	return func(ctx context.Context, audio io.Reader, partial func(string)) (string, error) {
		endpoint, client, err := paseoEndpoint(home)
		if err != nil {
			return "", err
		}
		credential, err := os.ReadFile(filepath.Join(home, "local-credential"))
		if err != nil {
			return "", fmt.Errorf("read Paseo local credential: %w", err)
		}
		token := strings.TrimSpace(string(credential))
		if b, e := base64.RawURLEncoding.DecodeString(token); e != nil || len(b) != 32 {
			return "", errors.New("invalid Paseo local credential")
		}
		setup, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(setup, endpoint, &websocket.DialOptions{HTTPClient: client})
		if err != nil {
			return "", fmt.Errorf("connect to Paseo dictation: %w", err)
		}
		defer ws.CloseNow()
		ws.SetReadLimit(2 << 20)
		id := "agent-desk-" + rand.Text()
		if err := wsjson.Write(setup, ws, map[string]any{
			"type": "hello", "clientId": id, "clientType": "cli", "protocolVersion": 1,
			"auth": map[string]string{"kind": "localCredential", "token": token},
		}); err != nil {
			return "", err
		}
		for {
			msg, err := readPaseo(setup, ws)
			if err != nil {
				return "", fmt.Errorf("Paseo hello: %w", err)
			}
			if msg.Type == "hello.rejected" {
				return "", errors.New("Paseo rejected the local credential")
			}
			if msg.Message.Type == "status" && msg.Message.Payload.Status == "server_info" {
				break
			}
		}
		if err := writePaseo(setup, ws, id, "start", map[string]any{"format": Format}); err != nil {
			return "", err
		}
		for {
			msg, err := readPaseo(setup, ws)
			if err != nil {
				return "", fmt.Errorf("start Paseo dictation: %w", err)
			}
			if msg.Message.Payload.DictationID != id {
				continue
			}
			if msg.Message.Type == "dictation_stream_error" {
				return "", errors.New(msg.Message.Payload.Error)
			}
			if msg.Message.Type == "dictation_stream_ack" && msg.Message.Payload.AckSeq == -1 {
				break
			}
		}
		go func() {
			if err := sendPaseoAudio(ctx, ws, id, audio); err != nil {
				_ = ws.CloseNow()
			}
		}()
		for {
			msg, err := readPaseo(ctx, ws)
			if err != nil {
				return "", fmt.Errorf("Paseo dictation: %w", err)
			}
			p := msg.Message.Payload
			if p.DictationID != id {
				continue
			}
			switch msg.Message.Type {
			case "dictation_stream_partial":
				partial(p.Text)
			case "dictation_stream_final":
				return p.Text, nil
			case "dictation_stream_error":
				return "", errors.New(p.Error)
			}
		}
	}
}

func paseoEndpoint(home string) (string, *http.Client, error) {
	raw, err := os.ReadFile(filepath.Join(home, "paseo.pid"))
	if err != nil {
		return "", nil, fmt.Errorf("read running Paseo endpoint: %w", err)
	}
	var lock struct{ Listen string }
	if err := json.Unmarshal(raw, &lock); err != nil {
		return "", nil, fmt.Errorf("read running Paseo endpoint: %w", err)
	}
	addr := strings.TrimPrefix(lock.Listen, "unix://")
	d := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{DialContext: d.DialContext}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if filepath.IsAbs(addr) {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) { return d.DialContext(ctx, "unix", addr) }
		return "ws://localhost/ws", client, nil
	}
	addr = strings.TrimPrefix(lock.Listen, "tcp://")
	if strings.IndexFunc(addr, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		addr = "127.0.0.1:" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", nil, errors.New("invalid local Paseo endpoint")
	}
	ip := net.ParseIP(host)
	if host == "localhost" || host == "" || (ip != nil && ip.IsUnspecified()) {
		host, ip = "127.0.0.1", net.IPv4(127, 0, 0, 1)
	}
	if ip == nil || !ip.IsLoopback() {
		return "", nil, errors.New("Paseo dictation requires a local Unix socket or loopback listener")
	}
	return "ws://" + net.JoinHostPort(host, port) + "/ws", client, nil
}

type paseoFrame struct {
	Type    string
	Message struct {
		Type    string
		Payload struct {
			DictationID, Status, Error, Text string
			AckSeq                           int
		}
	}
}

func readPaseo(ctx context.Context, ws *websocket.Conn) (paseoFrame, error) {
	var msg paseoFrame
	err := wsjson.Read(ctx, ws, &msg)
	return msg, err
}

func writePaseo(ctx context.Context, ws *websocket.Conn, id, kind string, fields map[string]any) error {
	fields["type"], fields["dictationId"] = "dictation_stream_"+kind, id
	return wsjson.Write(ctx, ws, map[string]any{"type": "session", "message": fields})
}

func sendPaseoAudio(ctx context.Context, ws *websocket.Conn, id string, audio io.Reader) error {
	seq := 0
	buf := make([]byte, BytesPerSecond/4)
	for {
		n, err := io.ReadFull(audio, buf)
		if n%2 != 0 {
			return errors.New("truncated PCM sample")
		}
		if n > 0 {
			if e := writePaseo(ctx, ws, id, "chunk", map[string]any{
				"seq": seq, "format": Format, "audio": base64.StdEncoding.EncodeToString(buf[:n]),
			}); e != nil {
				return e
			}
			seq++
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if seq == 0 {
				return errors.New("no audio to transcribe")
			}
			return writePaseo(ctx, ws, id, "finish", map[string]any{"finalSeq": seq - 1})
		}
		if err != nil {
			return err
		}
	}
}
