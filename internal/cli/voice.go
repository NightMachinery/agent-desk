package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
	"github.com/NightMachinery/agent-desk/internal/stt"
)

const voiceIdle = 90 * time.Second
const voiceFinalTimeout = 3 * time.Minute
const maxVoiceJobs = 8

type voiceHub struct {
	mu    sync.Mutex
	runs  map[string]*voiceRun
	store *store.Store
}

type voiceRun struct {
	rec      recording
	file     *os.File
	pipe     *io.PipeWriter
	chunks   chan []byte
	cancel   context.CancelFunc
	idle     *time.Timer
	deadline time.Time
	done     bool
	final    string
	err      error
}

func (s *server) voiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recordings", s.apiRecordings)
	mux.HandleFunc("GET /api/recordings/{id}", s.apiRecording)
	mux.HandleFunc("GET /api/recordings/{id}/audio", s.apiRecordingAudio)
	mux.HandleFunc("POST /api/recordings", s.apiRecordingStart)
	mux.HandleFunc("POST /api/recordings/{id}", s.apiRecordingAction)
	mux.HandleFunc("POST /api/recordings/{id}/chunk", s.apiRecordingChunk)
}

func (s *server) voiceStore() (*store.Store, error) {
	if s.voice.store == nil {
		st, err := store.Open(s.a.Home)
		if err != nil {
			return nil, err
		}
		s.voice.store = st
		s.voice.runs = make(map[string]*voiceRun)
	}
	return s.voice.store, nil
}

func (s *server) voiceConfig() (config.Voice, error) {
	cfg, err := config.Load(config.Path(s.a.Env.Getenv, s.a.Env.Home))
	return cfg.Voice, err
}

func (s *server) voiceBackend(cfg config.Voice, name string) (stt.Backend, error) {
	switch name {
	case "paseo":
		return stt.Paseo(s.a.paseoHome()), nil
	case "handy":
		path := cfg.HandySocket
		if path == "" {
			name := ""
			if s.a.Env.Getenv != nil {
				name = s.a.Env.Getenv("HANDY_IPC_NAME")
			}
			path = stt.HandySocket(name)
		}
		return stt.Handy(path), nil
	case "command":
		if cfg.Command != "" {
			return stt.Command(cfg.Command), nil
		}
	}
	return nil, fmt.Errorf("transcription backend %q is unavailable", name)
}

func (s *server) voiceSnapshot(rec recording) recording {
	if run := s.voice.runs[rec.ID]; run != nil {
		rec = run.rec
	} else if rec.Status == voiceRecording || rec.Status == voiceTranscribing {
		rec.Status, rec.Error = voiceFailed, "Transcription was interrupted. Retry the saved audio."
	}
	if rec.Status != voiceDeleted {
		if file, err := os.Stat(voiceFile(s.a.Home, rec.ID)); err == nil && file.Size() > stt.WAVHeaderSize {
			rec.Bytes = file.Size() - stt.WAVHeaderSize
			rec.Audio = "./api/recordings/" + rec.ID + "/audio"
		}
	}
	return rec
}

func (s *server) apiRecordings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cfg, err := s.voiceConfig()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	st, err := s.voiceStore()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	node := r.URL.Query().Get("node")
	recs, err := listRecordings(st, node, s.a.Host)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	used, err := st.Query("events", "kind = 'recording-used'")
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	messages := make(map[string][]string)
	for _, e := range used {
		messages[e.Ref] = append(messages[e.Ref], e.Body)
	}
	where := "kind = 'delivered' AND json_type(meta, '$.voice_recordings') = 'array'"
	var args []any
	if node != "" {
		where += " AND path = ?"
		args = append(args, node)
	}
	deliveries, err := st.Query("events", where, args...)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	nodes := make(map[string]string, len(recs))
	for _, rec := range recs {
		nodes[rec.ID] = rec.Node
	}
	for _, delivery := range deliveries {
		for _, id := range voiceReferences(delivery) {
			if nodes[id] != delivery.Path {
				continue
			}
			msg, err := st.Get(delivery.Ref)
			if err != nil {
				continue
			}
			display := sendDisplayID(st, msg)
			if !slices.Contains(messages[id], display) {
				messages[id] = append(messages[id], display)
			}
		}
	}
	for i := range recs {
		recs[i] = s.voiceSnapshot(recs[i])
		recs[i].Messages = messages[recs[i].ID]
	}
	backends := []string{"paseo", "handy"}
	if cfg.Command != "" {
		backends = append(backends, "command")
	}
	writeJSON(w, map[string]any{"recordings": recs, "backends": backends, "backend": cfg.Backend(), "keep_days": cfg.Days()})
}

func (s *server) getVoice(id string) (recording, error) {
	if !voiceID.MatchString(id) {
		return recording{}, os.ErrNotExist
	}
	st, err := s.voiceStore()
	if err != nil {
		return recording{}, err
	}
	rec, err := loadRecording(st, id)
	if err != nil {
		return rec, err
	}
	if rec.Status == voiceDeleted {
		return recording{}, os.ErrNotExist
	}
	return s.voiceSnapshot(rec), nil
}

func voiceAPIError(w http.ResponseWriter, err error) {
	code := http.StatusUnprocessableEntity
	if errors.Is(err, os.ErrNotExist) {
		code = http.StatusNotFound
		err = errors.New("Recording is unavailable")
	}
	jsonError(w, code, err.Error())
}

func (s *server) apiRecording(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	rec, err := s.getVoice(r.PathValue("id"))
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	writeJSON(w, rec)
}

func (s *server) apiRecordingAudio(w http.ResponseWriter, r *http.Request) {
	s.voice.mu.Lock()
	rec, err := s.getVoice(r.PathValue("id"))
	if err == nil && s.voice.runs[rec.ID] != nil && s.voice.runs[rec.ID].file != nil {
		err = errors.New("Stop recording before playing it")
	}
	var f *os.File
	if err == nil {
		f, err = os.Open(voiceFile(s.a.Home, rec.ID))
	}
	s.voice.mu.Unlock()
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, rec.ID+".wav", rec.Created, f)
}

type voiceRequest struct {
	Node    string `json:"node"`
	Action  string `json:"action"`
	Backend string `json:"backend"`
	Message string `json:"message"`
}

func readVoiceRequest(w http.ResponseWriter, r *http.Request, s *server) (voiceRequest, bool) {
	var body voiceRequest
	if err := s.sameOrigin(r); err != nil {
		jsonError(w, 403, err.Error())
		return body, false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		jsonError(w, 400, "Invalid recording request")
		return body, false
	}
	return body, true
}

func (s *server) apiRecordingStart(w http.ResponseWriter, r *http.Request) {
	body, ok := readVoiceRequest(w, r, s)
	if !ok {
		return
	}
	cfg, err := s.voiceConfig()
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	backend, err := s.voiceBackend(cfg, cfg.Backend())
	if err != nil {
		voiceAPIError(w, err)
		return
	}

	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	st, err := s.voiceStore()
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	d, err := desk.Load(st, s.a.Host, s.a.Now())
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	_, _, node, _, err := replyTarget(d, body.Node)
	if err != nil {
		jsonError(w, 404, "Unknown recording destination")
		return
	}
	if len(s.voice.runs) >= maxVoiceJobs {
		jsonError(w, 409, "Too many active recordings")
		return
	}
	var random [6]byte
	rand.Read(random[:])
	id := "v-" + hex.EncodeToString(random[:])
	rec := recording{ID: id, Node: node, Created: s.a.Now(), Backend: cfg.Backend(), Status: voiceRecording}
	if err := os.MkdirAll(voiceDir(s.a.Home), 0700); err != nil {
		voiceAPIError(w, err)
		return
	}
	f, err := os.OpenFile(voiceFile(s.a.Home, id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	if _, err = f.Write(stt.WAVHeader(0)); err == nil {
		err = s.a.appendRecording(st, rec)
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		voiceAPIError(w, err)
		return
	}
	input, pipe := io.Pipe()
	run := &voiceRun{rec: rec, file: f, pipe: pipe, chunks: make(chan []byte, 128)}
	s.voice.runs[id] = run
	s.armVoice(run, voiceIdle)
	go func() {
		defer pipe.Close()
		for chunk := range run.chunks {
			if _, err := pipe.Write(chunk); err != nil {
				return
			}
		}
	}()
	s.runVoice(run, backend, input)
	writeJSON(w, rec)
}

func (s *server) runVoice(run *voiceRun, backend stt.Backend, input io.ReadCloser) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute+voiceFinalTimeout)
	run.cancel = cancel
	go func() {
		defer cancel()
		defer input.Close()
		text, err := backend(ctx, input, func(partial string) {
			s.voice.mu.Lock()
			defer s.voice.mu.Unlock()
			if !run.done {
				run.rec.Partial = partial
			}
		})
		s.voice.mu.Lock()
		defer s.voice.mu.Unlock()
		if run.done || s.voice.runs[run.rec.ID] != run {
			return
		}
		if err == nil && strings.TrimSpace(text) == "" {
			err = errors.New("No speech was recognized")
		}
		run.done, run.final, run.err = true, text, err
		if run.file == nil {
			s.completeVoice(run)
		}
	}()
}

// completeVoice commits the result once capture has stopped. Keeping the
// recorder open across backend failures preserves all audio for a later retry.
func (s *server) completeVoice(run *voiceRun) {
	if run.idle != nil {
		run.idle.Stop()
	}
	run.rec.Status, run.rec.Text, run.rec.Partial = voiceReady, run.final, ""
	if run.err != nil {
		run.rec.Status, run.rec.Error = voiceFailed, run.err.Error()
	}
	if err := s.a.appendRecording(s.voice.store, run.rec); err != nil {
		fmt.Fprintf(s.a.Stderr, "agent-desk: saving voice result: %v\n", err)
	} else {
		s.wakeSendQueue()
	}
	delete(s.voice.runs, run.rec.ID)
}

func (s *server) stopVoice(run *voiceRun, abandoned error) {
	if run.file == nil {
		return
	}
	run.file.Close()
	run.file = nil
	close(run.chunks)
	run.rec.Status = voiceTranscribing
	if abandoned != nil || run.rec.Bytes == 0 {
		if abandoned == nil {
			abandoned = errors.New("The recording contains no audio")
		}
		run.done, run.err = true, abandoned
		run.cancel()
		run.pipe.CloseWithError(abandoned)
	}
	if run.done {
		s.completeVoice(run)
		return
	}
	run.idle.Stop()
	s.armVoice(run, voiceFinalTimeout)
	if err := s.a.appendRecording(s.voice.store, run.rec); err != nil {
		fmt.Fprintf(s.a.Stderr, "agent-desk: saving voice state: %v\n", err)
	}
}

// A timer callback already waiting for the mutex can outlive Reset or Stop.
func (s *server) armVoice(run *voiceRun, after time.Duration) {
	run.deadline = time.Now().Add(after)
	if run.idle == nil {
		run.idle = time.AfterFunc(after, func() { s.expireVoice(run) })
	} else {
		run.idle.Reset(after)
	}
}

func (s *server) expireVoice(run *voiceRun) {
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	if s.voice.runs[run.rec.ID] != run || time.Now().Before(run.deadline) {
		return
	}
	if run.file != nil {
		s.stopVoice(run, errors.New("Audio upload was interrupted. Retry the saved recording."))
		return
	}
	run.done, run.err = true, errors.New("Transcription timed out. Retry the saved recording.")
	run.cancel()
	s.completeVoice(run)
}

func (s *server) apiRecordingChunk(w http.ResponseWriter, r *http.Request) {
	if err := s.sameOriginAny(r); err != nil {
		jsonError(w, 403, err.Error())
		return
	}
	audio, err := io.ReadAll(http.MaxBytesReader(w, r.Body, stt.MaxChunk))
	if err != nil || len(audio) == 0 || len(audio)%2 != 0 {
		jsonError(w, 400, "Invalid PCM chunk")
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		jsonError(w, 400, "Invalid PCM offset")
		return
	}
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	run := s.voice.runs[r.PathValue("id")]
	if run == nil || run.file == nil {
		jsonError(w, 409, "Recording has stopped")
		return
	}
	if offset != run.rec.Bytes {
		jsonError(w, 409, "Audio arrived out of order")
		return
	}
	if run.rec.Bytes+int64(len(audio)) > stt.MaxAudio {
		s.stopVoice(run, nil)
		jsonError(w, 413, "Recording reached the 30-minute limit")
		return
	}
	n, err := run.file.Write(audio)
	if err == nil {
		_, err = run.file.WriteAt(stt.WAVHeader(run.rec.Bytes+int64(n)), 0)
	}
	if err != nil {
		s.stopVoice(run, err)
		voiceAPIError(w, err)
		return
	}
	run.rec.Bytes += int64(n)
	s.armVoice(run, voiceIdle)
	if !run.done {
		select {
		case run.chunks <- audio:
		default:
			run.done, run.err = true, errors.New("Transcription backend fell behind. Retry the saved recording.")
			run.cancel()
			run.pipe.CloseWithError(run.err)
		}
	}
	writeJSON(w, run.rec)
}

func (s *server) apiRecordingAction(w http.ResponseWriter, r *http.Request) {
	body, ok := readVoiceRequest(w, r, s)
	if !ok {
		return
	}
	s.voice.mu.Lock()
	defer s.voice.mu.Unlock()
	rec, err := s.getVoice(r.PathValue("id"))
	if err != nil {
		voiceAPIError(w, err)
		return
	}
	run := s.voice.runs[rec.ID]
	switch body.Action {
	case "finish":
		if run != nil {
			s.stopVoice(run, nil)
			rec = run.rec
		}
	case "retry":
		if run != nil || len(s.voice.runs) >= maxVoiceJobs {
			jsonError(w, 409, "A transcription is already active or the server is busy")
			return
		}
		if rec.Bytes == 0 {
			jsonError(w, 422, "This recording has no audio")
			return
		}
		cfg, e := s.voiceConfig()
		if e != nil {
			voiceAPIError(w, e)
			return
		}
		name := body.Backend
		if name == "" {
			name = rec.Backend
		}
		backend, e := s.voiceBackend(cfg, name)
		if e != nil {
			voiceAPIError(w, e)
			return
		}
		f, e := os.Open(voiceFile(s.a.Home, rec.ID))
		if e != nil {
			voiceAPIError(w, e)
			return
		}
		if _, e = f.Seek(stt.WAVHeaderSize, io.SeekStart); e != nil {
			f.Close()
			voiceAPIError(w, e)
			return
		}
		rec.Status, rec.Backend, rec.Text, rec.Error, rec.Partial = voiceTranscribing, name, "", "", ""
		if e = s.a.appendRecording(s.voice.store, rec); e != nil {
			f.Close()
			voiceAPIError(w, e)
			return
		}
		s.wakeSendQueue()
		run = &voiceRun{rec: rec}
		s.voice.runs[rec.ID] = run
		s.armVoice(run, voiceFinalTimeout)
		s.runVoice(run, backend, f)
	case "used":
		d, err := desk.Load(s.voice.store, s.a.Host, s.a.Now())
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		msg, err := d.Resolve(body.Message)
		if err != nil || (msg.Kind != "answer" && msg.Kind != "reply") || msg.Path != rec.Node {
			jsonError(w, 422, "Message does not belong to this recording's destination")
			return
		}
		display := d.Display(msg.ID)
		used, err := s.voice.store.Query("events", "kind = 'recording-used' AND ref = ? AND body = ?", rec.ID, display)
		if err != nil {
			voiceAPIError(w, err)
			return
		}
		if len(used) == 0 {
			e := s.a.event("recording-used", rec.Node, ident.From{}, "web", nil)
			e.Ref, e.Body = rec.ID, display
			if err := s.voice.store.Append(e); err != nil {
				voiceAPIError(w, err)
				return
			}
		}
	case "delete":
		if run != nil {
			jsonError(w, 409, "Stop recording and wait for transcription before deleting")
			return
		}
		if err := os.Remove(voiceFile(s.a.Home, rec.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			voiceAPIError(w, err)
			return
		}
		rec.Status, rec.Audio, rec.Text, rec.Error = voiceDeleted, "", "", ""
		if err := s.a.appendRecording(s.voice.store, rec); err != nil {
			voiceAPIError(w, err)
			return
		}
		s.wakeSendQueue()
	default:
		jsonError(w, 400, "Unknown recording action")
		return
	}
	writeJSON(w, rec)
}

// recoverVoice gives interrupted captures a retryable state before delivery
// starts on a fresh server process.
func (s *server) recoverVoice() error {
	st, err := s.a.store()
	if err != nil {
		return err
	}
	records, err := listRecordings(st, "", s.a.Host)
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.Status != voiceRecording && rec.Status != voiceTranscribing {
			continue
		}
		rec = s.voiceSnapshot(rec)
		if err := s.a.appendRecording(st, rec); err != nil {
			return err
		}
	}
	return nil
}
