package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestPaseoStreamingProtocol(t *testing.T) {
	credential := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done <- fakePaseo(r.Context(), w, r, credential)
	}))
	defer server.Close()
	home := t.TempDir()
	lock, _ := json.Marshal(map[string]string{"listen": strings.TrimPrefix(server.URL, "http://")})
	for name, value := range map[string][]byte{"paseo.pid": lock, "local-credential": []byte(credential)} {
		if err := os.WriteFile(filepath.Join(home, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var partial string
	final, err := Paseo(home)(ctx, bytes.NewReader([]byte{1, 2}), func(text string) { partial = text })
	if err != nil || partial != "part" || final != "final" {
		t.Fatalf("partial %q, final %q, error %v", partial, final, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func fakePaseo(ctx context.Context, w http.ResponseWriter, r *http.Request, token string) error {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	var hello struct {
		Type string
		Auth struct{ Kind, Token string }
	}
	if err := wsjson.Read(ctx, ws, &hello); err != nil {
		return err
	}
	if hello.Type != "hello" || hello.Auth.Kind != "localCredential" || hello.Auth.Token != token {
		return fmt.Errorf("unexpected Paseo admission")
	}
	send := func(kind string, payload map[string]any) error {
		return wsjson.Write(ctx, ws, map[string]any{
			"type": "session", "message": map[string]any{"type": kind, "payload": payload},
		})
	}
	if err := send("status", map[string]any{"status": "server_info"}); err != nil {
		return err
	}
	read := func(kind string) (map[string]any, error) {
		var frame struct {
			Type    string
			Message map[string]any
		}
		if err := wsjson.Read(ctx, ws, &frame); err != nil {
			return nil, err
		}
		if frame.Type != "session" || frame.Message["type"] != "dictation_stream_"+kind {
			return nil, fmt.Errorf("expected %s, received %v", kind, frame.Message)
		}
		return frame.Message, nil
	}
	start, err := read("start")
	if err != nil {
		return err
	}
	id := start["dictationId"]
	if err := send("dictation_stream_ack", map[string]any{"dictationId": id, "ackSeq": -1}); err != nil {
		return err
	}
	chunk, err := read("chunk")
	if err != nil {
		return err
	}
	if chunk["audio"] != "AQI=" || chunk["seq"] != float64(0) || chunk["format"] != Format {
		return fmt.Errorf("unexpected audio chunk %v", chunk)
	}
	if err := send("dictation_stream_partial", map[string]any{"dictationId": id, "text": "part"}); err != nil {
		return err
	}
	finish, err := read("finish")
	if err != nil {
		return err
	}
	if finish["finalSeq"] != float64(0) {
		return fmt.Errorf("unexpected final sequence %v", finish)
	}
	return send("dictation_stream_final", map[string]any{"dictationId": id, "text": "final"})
}
