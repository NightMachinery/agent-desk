package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ok fails t on an error and returns the bytes.
func ok(t *testing.T) func([]byte, error) []byte {
	return func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
}

// TestRFC8291Vector checks the coding against RFC 8291, Appendix A.
func TestRFC8291Vector(t *testing.T) {
	asPriv := ok(t)(decode64("yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	uaPublic := ok(t)(decode64("BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"))
	auth := ok(t)(decode64("BTBZMqHH6r4Tts7J_aSIgg"))
	salt := ok(t)(decode64("DGv6ra1nlYgDCS1FRnbzlw"))
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	as, err := ecdh.P256().NewPrivateKey(asPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt(uaPublic, auth, []byte("When I grow up, I want to be a watermelon"), as, salt)
	if err != nil {
		t.Fatal(err)
	}
	if b64.EncodeToString(got) != want {
		t.Fatalf("got  %s\nwant %s", b64.EncodeToString(got), want)
	}
}

func TestSendToAFakeService(t *testing.T) {
	ua, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	dir := Dir(t.TempDir())
	key, public, err := dir.Keys()
	if err != nil {
		t.Fatal(err)
	}
	// The key pair is made once.
	if _, again, _ := dir.Keys(); again != public {
		t.Fatal("Keys made a second key pair")
	}
	var got []byte
	var hdr http.Header
	gone := false
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gone {
			w.WriteHeader(http.StatusGone)
			return
		}
		hdr = r.Header.Clone()
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	var s Sub
	s.Endpoint = ts.URL + "/send/abc"
	s.Keys.P256dh = b64.EncodeToString(ua.PublicKey().Bytes())
	s.Keys.Auth = b64.EncodeToString(auth) + "==" // browsers may pad
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	x := Sender{Key: key, Public: public, Subject: DefaultSubject, Client: ts.Client(), Now: func() time.Time { return now }}
	msg := []byte(`{"type":"show","tag":"q-abcd"}`)
	if err := x.Send(context.Background(), s, Message{Payload: msg, TTL: time.Hour, Urgency: "high", Topic: "q-abcd"}); err != nil {
		t.Fatal(err)
	}
	if p := ok(t)(Decrypt(ua, auth, got)); !bytes.Equal(p, msg) {
		t.Fatalf("decrypted %q", p)
	}
	for k, v := range map[string]string{"Content-Encoding": "aes128gcm", "Ttl": "3600", "Urgency": "high", "Topic": "q-abcd"} {
		if hdr.Get(k) != v {
			t.Errorf("header %s = %q, want %q", k, hdr.Get(k), v)
		}
	}

	// The VAPID token: signed by the key it names, for the service's origin.
	a := hdr.Get("Authorization")
	if !strings.HasPrefix(a, "vapid t=") || !strings.HasSuffix(a, ", k="+public) {
		t.Fatalf("Authorization %q", a)
	}
	tok := strings.TrimSuffix(strings.TrimPrefix(a, "vapid t="), ", k="+public)
	parts := strings.Split(tok, ".")
	var claims map[string]any
	json.Unmarshal(ok(t)(decode64(parts[1])), &claims)
	if claims["aud"] != ts.URL || claims["sub"] != DefaultSubject || claims["exp"].(float64) != float64(now.Add(12*time.Hour).Unix()) {
		t.Fatalf("claims %v", claims)
	}
	sig := ok(t)(decode64(parts[2]))
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, ss := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&key.PublicKey, sum[:], r, ss) {
		t.Fatal("the VAPID signature does not verify")
	}

	gone = true
	if err := x.Send(context.Background(), s, Message{Payload: msg, TTL: time.Hour}); err != ErrGone {
		t.Fatalf("410: %v, want ErrGone", err)
	}
	if err := x.Send(context.Background(), s, Message{Payload: make([]byte, MaxPayload+1)}); err == nil {
		t.Fatal("an oversized payload was sent")
	}
}

func TestSubsUpdate(t *testing.T) {
	dir := Dir(t.TempDir())
	if subs, err := dir.Subs(); err != nil || len(subs) != 0 {
		t.Fatalf("%v %v", subs, err)
	}
	dir.Update(func(s []Sub) []Sub { return append(s, Sub{Endpoint: "https://x/1"}) })
	dir.Update(func(s []Sub) []Sub { return append(s, Sub{Endpoint: "https://x/2"}) })
	subs, _ := dir.Subs()
	if len(subs) != 2 || ID(subs[0].Endpoint) == ID(subs[1].Endpoint) || len(ID("a")) != 12 {
		t.Fatalf("%v", subs)
	}
	var bad Sub
	bad.Endpoint = "http://x/1"
	if bad.Check() == nil {
		t.Fatal("an http endpoint was accepted")
	}
}
