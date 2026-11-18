// Package push sends Web Push messages (RFC 8030) to the browsers that
// subscribed to them: the installed dashboard on a phone, which shows them
// as notifications even when it is closed.
//
// A message is encrypted for its subscription with the aes128gcm content
// coding (RFC 8291), so the push service that relays it (Google's, for
// Chrome on Android) sees only ciphertext. The desk identifies itself to
// that service with a VAPID key pair (RFC 8292), made once and kept in the
// push directory. Everything here is the standard library.
package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// DefaultSubject is the VAPID contact sent to push services when the
// config names none. It names the software, not its user: the subject is
// readable by the push service, and a dashboard address would tell it where
// the desk lives.
const DefaultSubject = "https://github.com/NightMachinery/agent-desk"

// MaxPayload is the most plaintext one message carries. A push service
// takes a body of 4096 bytes; the coding adds 86 bytes of header, a
// delimiter and a 16-byte tag.
const MaxPayload = 4096 - 86 - 1 - 16

var b64 = base64.RawURLEncoding

// decode64 reads base64url with or without padding, as browsers vary.
func decode64(s string) ([]byte, error) {
	return b64.DecodeString(strings.TrimRight(s, "="))
}

// Sub is one browser's subscription.
type Sub struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Label   string `json:"label,omitempty"`
	Created string `json:"created,omitempty"`
	LastOK  string `json:"last_ok,omitempty"`
	LastErr string `json:"last_error,omitempty"`
}

// ID names a subscription without revealing its endpoint, which is a
// capability: whoever holds it (and the VAPID key) can push to the device.
func ID(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:6])
}

// Check validates a subscription a browser sent.
func (s Sub) Check() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("the subscription's endpoint must be an https URL")
	}
	if p, err := decode64(s.Keys.P256dh); err != nil || len(p) != 65 || p[0] != 4 {
		return errors.New("the subscription's p256dh key must be an uncompressed P-256 point")
	} else if _, err := ecdh.P256().NewPublicKey(p); err != nil {
		return fmt.Errorf("the subscription's p256dh key: %v", err)
	}
	if a, err := decode64(s.Keys.Auth); err != nil || len(a) != 16 {
		return errors.New("the subscription's auth secret must be 16 bytes")
	}
	return nil
}

// Dir holds the key pair (vapid.json) and the subscriptions (subs.json).
type Dir string

type vapidFile struct {
	Private string `json:"private"` // the raw scalar, base64url
	Public  string `json:"public"`  // the uncompressed point, base64url
}

// Keys returns the VAPID key pair, making it the first time.
func (d Dir) Keys() (*ecdsa.PrivateKey, string, error) {
	var key *ecdsa.PrivateKey
	err := d.locked(func() error {
		path := filepath.Join(string(d), "vapid.json")
		if b, err := os.ReadFile(path); err == nil {
			var v vapidFile
			if err := json.Unmarshal(b, &v); err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			raw, err := decode64(v.Private)
			if err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			key, err = ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
			if err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		raw, err := k.Bytes()
		if err != nil {
			return err
		}
		pub, err := k.PublicKey.Bytes()
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(vapidFile{Private: b64.EncodeToString(raw), Public: b64.EncodeToString(pub)}, "", "  ")
		key = k
		return writeFile(path, append(b, '\n'))
	})
	if err != nil {
		return nil, "", err
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, "", err
	}
	return key, b64.EncodeToString(pub), nil
}

// Subs lists the subscriptions.
func (d Dir) Subs() ([]Sub, error) {
	var out []Sub
	err := d.locked(func() error {
		var err error
		out, err = d.read()
		return err
	})
	return out, err
}

// Update rewrites the subscriptions with f, under the directory's lock.
func (d Dir) Update(f func([]Sub) []Sub) error {
	return d.locked(func() error {
		subs, err := d.read()
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(f(subs), "", "  ")
		return writeFile(filepath.Join(string(d), "subs.json"), append(b, '\n'))
	})
}

func (d Dir) read() ([]Sub, error) {
	b, err := os.ReadFile(filepath.Join(string(d), "subs.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []Sub{}, nil
	}
	if err != nil {
		return nil, err
	}
	var subs []Sub
	if err := json.Unmarshal(b, &subs); err != nil {
		return nil, fmt.Errorf("%s: %v", filepath.Join(string(d), "subs.json"), err)
	}
	if subs == nil {
		subs = []Sub{}
	}
	return subs, nil
}

// locked runs f holding the directory's lock: the server adds
// subscriptions while notify-run sends and drops them.
func (d Dir) locked(f func() error) error {
	if err := os.MkdirAll(string(d), 0o700); err != nil {
		return err
	}
	l, err := os.OpenFile(filepath.Join(string(d), "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer l.Close()
	if err := syscall.Flock(int(l.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(l.Fd()), syscall.LOCK_UN)
	return f()
}

// writeFile replaces path atomically, readable by its owner only.
func writeFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Encrypt codes plaintext for s with aes128gcm, as one record.
func Encrypt(s Sub, plaintext []byte) ([]byte, error) {
	ua, err := decode64(s.Keys.P256dh)
	if err != nil {
		return nil, err
	}
	auth, err := decode64(s.Keys.Auth)
	if err != nil {
		return nil, err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return encrypt(ua, auth, plaintext, as, salt)
}

// encrypt is Encrypt with the sender's key and the salt given, as the
// RFC's test vector needs.
func encrypt(uaPublic, auth, plaintext []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > MaxPayload {
		return nil, fmt.Errorf("push payload is %d bytes; the most is %d", len(plaintext), MaxPayload)
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := as.PublicKey().Bytes()
	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Write(salt)
	out.Write([]byte{0, 0, 0x10, 0}) // record size 4096
	out.WriteByte(byte(len(asPublic)))
	out.Write(asPublic)
	// The last (and only) record ends with the delimiter 2 and no padding.
	out.Write(gcm.Seal(nil, nonce, append(append([]byte{}, plaintext...), 2), nil))
	return out.Bytes(), nil
}

// Decrypt is the browser's side of Encrypt: ua is the subscription's
// private key. Tests use it to read what was sent.
func Decrypt(ua *ecdh.PrivateKey, auth, body []byte) ([]byte, error) {
	if len(body) < 21 || len(body) < 21+int(body[20]) {
		return nil, errors.New("push body too short")
	}
	salt, idlen := body[:16], int(body[20])
	asPublic := body[21 : 21+idlen]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, err
	}
	shared, err := ua.ECDH(as)
	if err != nil {
		return nil, err
	}
	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(asPublic), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		return nil, err
	}
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		return nil, errors.New("push record lacks its last-record delimiter")
	}
	return plain[:len(plain)-1], nil
}

// vapid is the Authorization header value for a request to endpoint.
func vapid(key *ecdsa.PrivateKey, public, endpoint, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": subject,
	})
	signing := head + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + public, nil
}

// Message is what one push carries, and how the push service treats it.
type Message struct {
	Payload []byte
	// TTL is how long the service keeps it for a device that is offline.
	TTL time.Duration
	// Urgency is very-low, low, normal or high; high wakes a sleeping phone.
	Urgency string
	// Topic replaces an undelivered message with the same topic, so taking
	// a notification back can overtake it (up to 32 base64url characters).
	Topic string
}

// Sender sends messages as one desk.
type Sender struct {
	Key     *ecdsa.PrivateKey
	Public  string
	Subject string
	Client  *http.Client
	Now     func() time.Time
}

// ErrGone means the subscription no longer exists: drop it.
var ErrGone = errors.New("the subscription has expired or was removed")

// Send pushes m to s.
func (x Sender) Send(ctx context.Context, s Sub, m Message) error {
	body, err := Encrypt(s, m.Payload)
	if err != nil {
		return err
	}
	now := time.Now
	if x.Now != nil {
		now = x.Now
	}
	auth, err := vapid(x.Key, x.Public, s.Endpoint, x.Subject, now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", fmt.Sprint(int(m.TTL.Seconds())))
	if m.Urgency != "" {
		req.Header.Set("Urgency", m.Urgency)
	}
	if m.Topic != "" {
		req.Header.Set("Topic", m.Topic)
	}
	client := x.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("push service: %s: %s", resp.Status, bytes.TrimSpace(text))
	}
	return nil
}
