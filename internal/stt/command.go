package stt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Command runs an operator-configured command receiving raw PCM on stdin.
// stdout contains one {"type":"partial"|"final","text":"..."} per line.
func Command(script string) Backend {
	return func(ctx context.Context, audio io.Reader, partial func(string)) (string, error) {
		cmd := exec.CommandContext(ctx, "sh", "-c", script)
		cmd.Env = append(os.Environ(), "AGENT_DESK_AUDIO_FORMAT="+Format)
		cmd.WaitDelay = 2 * time.Second
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return "", err
		}
		if err := cmd.Start(); err != nil {
			return "", err
		}
		stop := context.AfterFunc(ctx, func() { _ = stdout.Close(); _ = stdin.Close() })
		defer stop()
		go func() { defer stdin.Close(); _, _ = io.Copy(stdin, audio) }()
		var final string
		var complete bool
		var protocolErr error
		scan := bufio.NewScanner(stdout)
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			var result struct{ Type, Text string }
			if err := json.Unmarshal(scan.Bytes(), &result); err != nil {
				protocolErr = errors.New("command emitted malformed transcription JSON")
				break
			}
			switch result.Type {
			case "partial":
				partial(result.Text)
			case "final":
				final, complete = result.Text, true
			default:
				protocolErr = fmt.Errorf("unknown transcription event %q", result.Type)
			}
			if protocolErr != nil {
				break
			}
		}
		if protocolErr == nil {
			protocolErr = scan.Err()
		}
		if protocolErr != nil {
			_ = cmd.Process.Kill()
		}
		err = cmd.Wait()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if protocolErr != nil {
			return "", protocolErr
		}
		if err != nil {
			return "", fmt.Errorf("transcription command: %w", err)
		}
		if !complete {
			return "", errors.New("command exited without a final transcript")
		}
		return final, nil
	}
}
