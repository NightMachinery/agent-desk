package stt

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HandySocket mirrors Handy's Unix control socket naming convention. An
// explicit path is useful when Desk and Handy run with different TMPDIRs.
func HandySocket(name string) string {
	base := os.TempDir()
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		if info, err := os.Stat(runtime); err == nil && info.IsDir() {
			base = runtime
		}
	}
	dir := "handy-" + strconv.Itoa(os.Geteuid())
	if name != "" {
		suffix := strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				return r
			}
			return -1
		}, name)
		if len(suffix) > 32 {
			suffix = suffix[:32]
		}
		dir += "-" + suffix
	}
	path := filepath.Join(base, dir, "ipc.sock")
	if len(path) > 100 {
		path = filepath.Join("/tmp", "handy-"+strconv.Itoa(os.Geteuid()), "ipc.sock")
	}
	return path
}

// Handy speaks the control socket's single-job streaming transcription protocol.
func Handy(path string) Backend {
	return func(ctx context.Context, audio io.Reader, partial func(string)) (string, error) {
		dir := filepath.Dir(path)
		info, err := os.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("Handy control socket: %w", err)
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("Handy socket must be in a private directory")
		}
		// Use the kernel's access check as well as the mode check. A private
		// directory that another user owns cannot be traversed by the desk.
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "unix", path)
		if err != nil {
			return "", fmt.Errorf("Handy control socket: %w", err)
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		type frame struct {
			Type, Committed, Tentative, Message string
			Body                                struct{ Kind, Text string }
		}
		scan := bufio.NewScanner(conn)
		scan.Buffer(make([]byte, 4096), 1<<20)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var hello frame
		if !scan.Scan() || json.Unmarshal(scan.Bytes(), &hello) != nil || hello.Type != "hello" {
			return "", errors.New("Handy did not greet on its control socket")
		}
		_ = conn.SetReadDeadline(time.Time{})
		if _, err := io.WriteString(conn, "{\"type\":\"transcribe_stream\"}\n"); err != nil {
			return "", err
		}
		go func() {
			err := sendHandyAudio(conn, audio)
			if err != nil {
				_ = conn.Close()
			}
		}()
		for scan.Scan() {
			var f frame
			if err := json.Unmarshal(scan.Bytes(), &f); err != nil {
				return "", err
			}
			switch f.Type {
			case "partial":
				partial(f.Committed + f.Tentative)
			case "result":
				if f.Body.Kind == "stream_transcript" {
					return f.Body.Text, nil
				}
			case "error":
				return "", fmt.Errorf("Handy: %s", f.Message)
			}
		}
		if err := scan.Err(); err != nil {
			return "", err
		}
		return "", errors.New("Handy disconnected without a final transcript")
	}
}

// Give the recognizer up to 250 ms per feed. ReadFull keeps PCM sample pairs
// intact when the input is a pipe or the source uses arbitrary chunk sizes.
func sendHandyAudio(dst io.Writer, audio io.Reader) error {
	var size [4]byte
	buf := make([]byte, BytesPerSecond/4)
	for {
		n, err := io.ReadFull(audio, buf)
		if n%2 != 0 {
			return errors.New("truncated PCM sample")
		}
		if n != 0 {
			binary.LittleEndian.PutUint32(size[:], uint32(n))
			if _, e := dst.Write(size[:]); e != nil {
				return e
			}
			if _, e := dst.Write(buf[:n]); e != nil {
				return e
			}
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			_, err = dst.Write([]byte{0, 0, 0, 0})
			return err
		}
		if err != nil {
			return err
		}
	}
}
