package relay

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testNS     = "ichor-relay"
	testSecret = "ichor-relay"
)

// fakeKube serves one namespace's Secrets with resourceVersion checks. conflicts makes the
// next PUTs fail with 409 after another writer's change (it bumps the version and runs
// sideWrite on the stored data first).
type fakeKube struct {
	mu        sync.Mutex
	secrets   map[string]map[string]any
	rv        int
	conflicts int
	sideWrite func(data map[string]any)
	calls     []string
}

func newFakeKube(t *testing.T) (*fakeKube, *Kube) {
	t.Helper()
	f := &fakeKube{secrets: map[string]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	return f, LocalKube(srv.URL)
}

func (f *fakeKube) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method)

	prefix := "/api/v1/namespaces/" + testNS + "/secrets"
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, `{"message":"wrong path"}`, http.StatusNotFound)

		return
	}

	var body map[string]any
	if r.Method != http.MethodGet {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	switch r.Method {
	case http.MethodGet:
		obj, ok := f.secrets[name]
		if !ok {
			http.Error(w, `{"message":"secrets not found"}`, http.StatusNotFound)

			return
		}
		_ = json.NewEncoder(w).Encode(obj)
	case http.MethodPost:
		meta := body["metadata"].(map[string]any)
		name = meta["name"].(string)
		if _, ok := f.secrets[name]; ok {
			http.Error(w, `{"message":"already exists"}`, http.StatusConflict)

			return
		}
		f.store(name, body)
		_ = json.NewEncoder(w).Encode(body)
	case http.MethodPut:
		cur, ok := f.secrets[name]
		if !ok {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)

			return
		}
		if f.conflicts > 0 {
			f.conflicts--
			if f.sideWrite != nil {
				f.sideWrite(cur["data"].(map[string]any))
			}
			f.rv++
			cur["metadata"].(map[string]any)["resourceVersion"] = strconv.Itoa(f.rv)
		}
		if body["metadata"].(map[string]any)["resourceVersion"] != cur["metadata"].(map[string]any)["resourceVersion"] {
			http.Error(w, `{"message":"the object has been modified"}`, http.StatusConflict)

			return
		}
		f.store(name, body)
		_ = json.NewEncoder(w).Encode(body)
	}
}

func (f *fakeKube) store(name string, obj map[string]any) {
	f.rv++
	obj["metadata"].(map[string]any)["resourceVersion"] = strconv.Itoa(f.rv)
	f.secrets[name] = obj
}

// data returns a stored key, decoded.
func (f *fakeKube) data(t *testing.T, key string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	v, _ := f.secrets[testSecret]["data"].(map[string]any)[key].(string)
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		t.Fatalf("key %s: %v", key, err)
	}

	return string(raw)
}

// pushRecorder is a push server: it records requests and answers status (200 by default),
// or 410 under /gone.
type pushRecorder struct {
	mu   sync.Mutex
	reqs []recorded
	srv  *httptest.Server
}

type recorded struct {
	path   string
	header http.Header
	body   []byte
}

func newPushRecorder(t *testing.T) *pushRecorder {
	t.Helper()
	p := &pushRecorder{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.reqs = append(p.reqs, recorded{path: r.URL.Path, header: r.Header.Clone(), body: body})
		p.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusGone)

			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(p.srv.Close)

	return p
}

func (p *pushRecorder) requests() []recorded {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]recorded(nil), p.reqs...)
}

// wait waits for n requests.
func (p *pushRecorder) wait(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(p.requests()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("got %d pushes, want %d", len(p.requests()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// phone is a UnifiedPush subscription's key pair, as the phone holds it.
type phone struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newPhone(t *testing.T) phone {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)

	return phone{key: key, auth: auth}
}

func (p phone) device(id, endpoint string) Device {
	return Device{
		ID:       id,
		Platform: PlatformUnifiedPush,
		Endpoint: endpoint,
		P256DH:   base64.RawURLEncoding.EncodeToString(p.key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(p.auth),
		Cluster:  "fp-" + id,
		Label:    "homelab",
	}
}

// decrypt undoes RFC 8291 aes128gcm the way the phone does.
func (p phone) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body too short: %d", len(body))
	}
	salt, idLen := body[:16], int(body[20])
	if rs := binary.BigEndian.Uint32(body[16:20]); rs != 4096 {
		t.Fatalf("record size %d", rs)
	}
	serverPub, ciphertext := body[21:21+idLen], body[21+idLen:]

	pub, err := ecdh.P256().NewPublicKey(serverPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := p.key.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}

	info := append([]byte("WebPush: info\x00"), p.key.PublicKey().Bytes()...)
	info = append(info, serverPub...)
	ikm := must(hkdf.Key(sha256.New, shared, p.auth, string(info), 32))
	cek := must(hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16))
	nonce := must(hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12))

	block := must(aes.NewCipher(cek))
	gcm := must(cipher.NewGCM(block))
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 0x02 {
		t.Fatal("no last-record delimiter")
	}

	return plain[:len(plain)-1]
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}

	return v
}

// testRelay is a loaded relay behind an httptest server.
type testRelay struct {
	relay *Relay
	kube  *fakeKube
	store *Store
	srv   *httptest.Server
	// offset moves the relay's clock forward.
	offset atomic.Int64
}

var testNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func newTestRelay(t *testing.T) *testRelay {
	t.Helper()
	fk, kube := newFakeKube(t)
	store := NewStore(kube, testNS, testSecret)
	if err := store.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	tr := &testRelay{kube: fk, store: store}
	now := func() time.Time { return testNow.Add(time.Duration(tr.offset.Load())) }
	tr.relay = New(Options{Store: store, Now: now, Client: &http.Client{Timeout: 5 * time.Second}})
	tr.relay.Start()
	tr.srv = httptest.NewServer(tr.relay.Handler())
	t.Cleanup(tr.srv.Close)

	return tr
}

// drain waits for the queued deliveries.
func (tr *testRelay) drain(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tr.relay.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// call sends a request with the given headers and returns the status and body.
func (tr *testRelay) call(t *testing.T, method, path string, body any, headers map[string]string) (int, string) {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		reader = bytes.NewReader(must(json.Marshal(b)))
	}

	req := must(http.NewRequest(method, tr.srv.URL+path, reader))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	return resp.StatusCode, string(must(io.ReadAll(resp.Body)))
}

func (tr *testRelay) pairingHeader() map[string]string {
	return map[string]string{pairingHeader: tr.store.Snapshot().PairingToken}
}

func (tr *testRelay) webhookHeader() map[string]string {
	return map[string]string{"Authorization": "Bearer " + tr.store.Snapshot().WebhookToken}
}

func (tr *testRelay) pair(t *testing.T, d Device) {
	t.Helper()
	if code, body := tr.call(t, http.MethodPut, "/v1/devices", d, tr.pairingHeader()); code != http.StatusNoContent {
		t.Fatalf("pair %s: %d %s", d.ID, code, body)
	}
}

// sampleWebhook is the Alertmanager docs' example body, with invented names.
const sampleWebhook = `{
  "version": "4",
  "groupKey": "{}:{alertname=\"KubePodCrashLooping\"}",
  "truncatedAlerts": 0,
  "status": "firing",
  "receiver": "ichor-relay",
  "groupLabels": {"alertname": "KubePodCrashLooping"},
  "commonLabels": {"alertname": "KubePodCrashLooping"},
  "commonAnnotations": {},
  "externalURL": "http://alertmanager.monitoring:9093",
  "alerts": [
    {
      "status": "firing",
      "labels": {"alertname": "KubePodCrashLooping", "severity": "warning", "namespace": "shop"},
      "annotations": {"summary": "Pod shop/cart-7d9 is crash looping", "description": "Restarted 5 times"},
      "startsAt": "2026-10-13T11:50:00Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "http://prometheus.monitoring:9090/graph",
      "fingerprint": "a1b2c3d4e5f60718"
    },
    {
      "status": "firing",
      "labels": {"alertname": "NodeDown", "severity": "page", "namespace": ""},
      "annotations": {"summary": "worker-2 is down"},
      "startsAt": "2026-10-13T11:55:00Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "http://prometheus.monitoring:9090/graph",
      "fingerprint": "0f1e2d3c4b5a6978"
    }
  ]
}`
