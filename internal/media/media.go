// Package media keeps the files attached to what agents file and to what
// the user sends them: images, audio and video, which the dashboard plays,
// and any other file, which it offers as a download.
//
// A file is copied into <desk>/media, named by the SHA-256 of its content
// plus an extension for its type, so the same file attached twice is stored
// once. The event history records only a reference (Ref) in the event's
// meta; the files themselves are not versioned and may be collected (see
// the gc command) or be missing on a host that has not fetched them yet.
package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Ref is one attachment as an event records it.
type Ref struct {
	SHA256  string `json:"sha256"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Size    int64  `json:"size"`
	Caption string `json:"caption"`
}

// MaxPerItem is how many files one event may carry.
const MaxPerItem = 8

// Limits by kind of file, in bytes.
const (
	MaxImage = 10 << 20
	MaxAudio = 50 << 20
	MaxVideo = 100 << 20
	MaxFile  = 25 << 20
	// MaxAny is the largest of them, the most an upload may send.
	MaxAny = MaxVideo
)

// Generic is the type of a file of no known type. It is stored as .bin and
// only ever served as a download.
const Generic = "application/octet-stream"

// exts maps each accepted type to the extension its stored file gets.
var exts = map[string]string{
	"image/png":        ".png",
	"image/jpeg":       ".jpg",
	"image/gif":        ".gif",
	"image/webp":       ".webp",
	"image/svg+xml":    ".svg",
	"image/heic":       ".heic",
	"image/avif":       ".avif",
	"audio/mpeg":       ".mp3",
	"audio/mp4":        ".m4a",
	"audio/aac":        ".aac",
	"audio/ogg":        ".ogg",
	"audio/opus":       ".opus",
	"audio/wav":        ".wav",
	"audio/flac":       ".flac",
	"audio/webm":       ".weba",
	"video/mp4":        ".mp4",
	"video/webm":       ".webm",
	"video/quicktime":  ".mov",
	"application/pdf":  ".pdf",
	"text/plain":       ".txt",
	"text/markdown":    ".md",
	"text/csv":         ".csv",
	"application/json": ".json",
	"application/zip":  ".zip",
	"application/gzip": ".gz",
	Generic:            ".bin",
}

// byExt is the type each accepted extension names; it wins over sniffing,
// which cannot tell most audio formats apart.
var byExt = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".heic": "image/heic", ".avif": "image/avif",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".ogg": "audio/ogg",
	".oga": "audio/ogg", ".opus": "audio/opus", ".wav": "audio/wav", ".flac": "audio/flac",
	".weba": "audio/webm",
	".mp4":  "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
	".pdf": "application/pdf", ".txt": "text/plain", ".log": "text/plain", ".md": "text/markdown",
	".org": "text/plain", ".csv": "text/csv", ".json": "application/json", ".jsonl": "text/plain",
	".zip": "application/zip", ".gz": "application/gzip", ".bin": Generic,
}

var fileRE = regexp.MustCompile(`^[0-9a-f]{64}\.[a-z0-9]{2,5}$`)

// Dir is where a desk keeps its media.
func Dir(home string) string { return filepath.Join(home, "media") }

// File is the stored file's name: the content hash and the type's extension.
func (r Ref) File() string { return r.SHA256 + exts[r.MIME] }

// Path is the stored file's path in the desk at home.
func (r Ref) Path(home string) string { return filepath.Join(Dir(home), r.File()) }

// Present reports whether the stored file is on this host.
func (r Ref) Present(home string) bool {
	st, err := os.Stat(r.Path(home))
	return err == nil && st.Mode().IsRegular()
}

// Kind is "image", "audio" or "video", which the dashboard plays, or
// "file", which it offers as a download.
func (r Ref) Kind() string { return KindOf(r.MIME) }

// KindOf is the Kind of type mimeType.
func KindOf(mimeType string) string {
	switch k, _, _ := strings.Cut(mimeType, "/"); k {
	case "image", "audio", "video":
		return k
	}
	return "file"
}

// ValidFile reports whether name has the form of a stored file's name, so
// that it is safe to join to the media directory.
func ValidFile(name string) bool { return fileRE.MatchString(name) }

// Limit is the largest file of type mime accepted.
func Limit(mimeType string) int64 {
	switch KindOf(mimeType) {
	case "image":
		return MaxImage
	case "audio":
		return MaxAudio
	case "video":
		return MaxVideo
	}
	return MaxFile
}

// Limits says what may be attached, for messages.
func Limits() string {
	return fmt.Sprintf("any file: images up to %s, audio %s, video %s, anything else %s",
		Size(MaxImage), Size(MaxAudio), Size(MaxVideo), Size(MaxFile))
}

// typeOf names the type of a file called name, from its extension, else
// from its first bytes, else Generic.
func typeOf(name string, head []byte) string {
	if t, ok := byExt[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	t, _, _ := mime.ParseMediaType(http.DetectContentType(head))
	if _, ok := exts[t]; ok {
		return t
	}
	return Generic
}

// Add copies the file at path into the desk at home and returns its Ref.
func Add(home, path, caption string) (Ref, error) {
	caption = strings.TrimSpace(caption)
	if caption == "" {
		return Ref{}, fmt.Errorf("%s: an attachment needs a caption (--caption 'what it shows and why it matters')", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Ref{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Ref{}, err
	}
	if !st.Mode().IsRegular() {
		return Ref{}, fmt.Errorf("%s is not a regular file", path)
	}
	r, err := AddReader(home, filepath.Base(path), caption, f)
	if err != nil {
		return Ref{}, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// AddReader stores the content of src, a file called name, and returns its
// Ref. An empty caption becomes the name.
func AddReader(home, name, caption string, src io.Reader) (Ref, error) {
	name = CleanName(name)
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Ref{}, err
	}
	head = head[:n]
	t := typeOf(name, head)
	r := Ref{Name: name, MIME: t, Caption: strings.TrimSpace(caption)}
	if r.Caption == "" {
		r.Caption = name
	}
	max := Limit(t)
	sum, size, err := store1(home, "", func(sum string) string { return sum + exts[t] }, io.MultiReader(bytes.NewReader(head), src), max)
	if err != nil {
		return Ref{}, fmt.Errorf("%w (the limit for %s is %s)", err, KindOf(t), Size(max))
	}
	r.SHA256, r.Size = sum, size
	return r, nil
}

// CleanName is a file name safe to show and to offer as a download name:
// its last path element, without control characters, at most 200 bytes.
func CleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	return name
}

// store1 copies src into the media directory under the name file(sum), by
// way of a temporary file, and returns its hash and size. When want is set,
// the content must hash to it.
func store1(home, want string, file func(string) string, src io.Reader, max int64) (string, int64, error) {
	if err := os.MkdirAll(Dir(home), 0o700); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(Dir(home), ".in-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, max+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	if n > max {
		return "", 0, fmt.Errorf("larger than %s", Size(max))
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if want != "" && sum != want {
		return "", 0, fmt.Errorf("content hashes to %s, not %s", sum[:12], want[:12])
	}
	if err := os.Rename(tmp.Name(), filepath.Join(Dir(home), file(sum))); err != nil {
		return "", 0, err
	}
	return sum, n, nil
}

// Put stores content received for the stored file name file, checking that
// it hashes to the name. It is how hosts exchange media.
func Put(home, file string, src io.Reader) error {
	if !ValidFile(file) {
		return fmt.Errorf("%q is not a media file name", file)
	}
	want, ext := file[:64], file[64:]
	var max int64
	if t, ok := byExt[ext]; ok {
		max = Limit(t)
	} else {
		return fmt.Errorf("%q: unknown extension", file)
	}
	_, _, err := store1(home, want, func(string) string { return file }, src, max)
	return err
}

// TypeOfFile is the type a stored file's name says it holds.
func TypeOfFile(file string) string { return byExt[filepath.Ext(file)] }

// List returns the names of the stored files on this host.
func List(home string) ([]string, error) {
	ents, err := os.ReadDir(Dir(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.Type().IsRegular() && ValidFile(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// Of returns the attachments an event records.
func Of(e store.Event) []Ref {
	var m struct {
		Media []Ref `json:"media"`
	}
	if json.Unmarshal(e.Meta, &m) != nil {
		return nil
	}
	out := m.Media[:0]
	for _, r := range m.Media {
		// An event from elsewhere is checked before its refs name a path.
		if len(r.SHA256) == 64 && exts[r.MIME] != "" && ValidFile(r.File()) {
			out = append(out, r)
		}
	}
	return out
}

// Size formats a byte count for people.
func Size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
