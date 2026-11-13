package ichorgo

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	relayTestProxy  = "/api/v1/namespaces/ichor-relay/services/ichor-relay:8080/proxy"
	relayTestSecret = "/api/v1/namespaces/ichor-relay/secrets/ichor-relay"
	relayTestSource = `{"mode":"proxy","kind":"ichor-relay","namespace":"ichor-relay","service":"ichor-relay","port":8080}`
	relayTestToken  = "pairing-token-of-the-test"
)

// relayTestServices are a relay, an Alertmanager and an unrelated Service.
const relayTestServices = `{"items":[
	{"metadata":{"name":"alertmanager-operated","namespace":"monitoring"},"spec":{"ports":[{"name":"web","port":9093}]}},
	{"metadata":{"name":"notifier","namespace":"homelab","labels":{"app.kubernetes.io/name":"ichor-relay"}},"spec":{"ports":[{"name":"metrics","port":9100},{"name":"http","port":80}]}},
	{"metadata":{"name":"ichor-relay","namespace":"ichor-relay"},"spec":{"ports":[{"name":"http","port":8080}]}},
	{"metadata":{"name":"web","namespace":"prod-eu"},"spec":{"ports":[{"name":"http","port":8080}]}}]}`

// relayTestAPI is a fake API server holding a relay: its Services, its Secret and the relay
// API behind the service proxy. pairing records the X-Ichor-Pairing header of each request.
func relayTestAPI(t *testing.T, allowed bool) (*fakeKubeAPI, func() []string) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/services":                                      relayTestServices,
		"GET " + relayTestSecret:                                    `{"type":"Opaque","data":{"pairingToken":"` + base64.StdEncoding.EncodeToString([]byte(relayTestToken)) + `"}}`,
		"GET " + relayTestProxy + "/v1/pairing":                     `{"vapidPublicKey":"BPublicKeyOfTheRelay"}`,
		"PUT " + relayTestProxy + "/v1/devices":                     ``,
		"DELETE " + relayTestProxy + "/v1/devices/0123456789abcdef": ``,
	})
	f.answerFunc = func(method, path, body string) (string, bool) {
		if method != http.MethodPost || path != ssarPath {
			return "", false
		}

		if allowed {
			return `{"status":{"allowed":true}}`, true
		}

		return `{"status":{"allowed":false,"reason":"RBAC: no rule"}}`, true
	}

	var (
		mu      sync.Mutex
		headers []string
	)

	f.beforeAnswer = func(r *http.Request) {
		if strings.Contains(r.URL.Path, "/proxy/") {
			mu.Lock()
			headers = append(headers, r.Header.Get("X-Ichor-Pairing"))
			mu.Unlock()
		}
	}
	useFakeKube(t, f)

	return f, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), headers...)
	}
}

func TestRelayDiscover(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
		want    []string
	}{
		{"admin", true, []string{`"canPair":true`, `"reason":""`}},
		{"viewer", false, []string{`"canPair":false`, `get on the Secret ichor-relay/ichor-relay (RBAC: no rule)`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := relayTestAPI(t, tc.allowed)

			out, err := RelayDiscover("cfg", "ctx", "")
			if err != nil {
				t.Fatal(err)
			}

			var got relayDiscovery
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("%v: %s", err, out)
			}

			// The Service named ichor-relay first, then the one labelled so; never Alertmanager.
			if len(got.Sources) != 2 || got.Sources[0].Service != "ichor-relay" || got.Sources[0].Port != 8080 || got.Sources[0].Kind != relayKind ||
				got.Sources[1].Service != "notifier" || got.Sources[1].Port != 80 {
				t.Fatalf("sources %+v", got.Sources)
			}

			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %s in %s", w, out)
				}
			}

			var review ssarRequest
			for _, r := range f.recorded() {
				if r.path == ssarPath {
					_ = json.Unmarshal([]byte(r.body), &review)
				}
			}

			if a := review.Spec.ResourceAttributes; a.Verb != "get" || a.Resource != "secrets" || a.Namespace != "ichor-relay" || a.Name != "ichor-relay" {
				t.Errorf("access review %+v", a)
			}
		})
	}
}

func TestRelayDiscoverNone(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/services": `{"items":[{"metadata":{"name":"web","namespace":"prod-eu"},"spec":{"ports":[{"name":"http","port":8080}]}}]}`,
	})
	useFakeKube(t, f)

	out, err := RelayDiscover("cfg", "ctx", "")
	if err != nil || !strings.Contains(out, `"sources":[]`) || !strings.Contains(out, `"canPair":false`) || !strings.Contains(out, "no ichor-relay Service") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestRelayDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := RelayDiscover(demo, "", "")
	if err != nil || !strings.Contains(out, `"service":"ichor-relay"`) || !strings.Contains(out, `"canPair":false`) || !strings.Contains(out, `"reason":"demo"`) {
		t.Fatalf("discover: %s %v", out, err)
	}

	if _, err := RelayPairing(demo, "", "", relayTestSource); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("pairing: %v", err)
	}

	if _, err := RelayRegister(demo, "", "", relayTestSource, `{"platform":"ntfy","endpoint":"https://ntfy.example/topic"}`); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("register: %v", err)
	}

	if err := RelayUnregister(demo, "", "", relayTestSource, "0123456789abcdef"); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("unregister: %v", err)
	}
}

func TestRelayPairing(t *testing.T) {
	_, headers := relayTestAPI(t, true)

	out, err := RelayPairing("cfg", "ctx", "", relayTestSource)
	if err != nil {
		t.Fatal(err)
	}

	if out != `{"vapidPublicKey":"BPublicKeyOfTheRelay","relayVersion":""}` {
		t.Errorf("got %s", out)
	}

	if strings.Contains(out, relayTestToken) {
		t.Error("the pairing token reached the app")
	}

	if h := headers(); len(h) != 1 || h[0] != relayTestToken {
		t.Errorf("pairing headers %q", h)
	}
}

func TestRelayPairingRefused(t *testing.T) {
	f, _ := relayTestAPI(t, true)
	f.answerWith("GET "+relayTestProxy+"/v1/pairing", http.StatusUnauthorized, `{"error":"missing or wrong pairing token"}`)

	_, err := RelayPairing("cfg", "ctx", "", relayTestSource)
	if err == nil || !strings.Contains(err.Error(), "the relay refused the pairing token: redeploy the relay or check RBAC") {
		t.Fatalf("got %v", err)
	}

	if strings.Contains(err.Error(), relayTestToken) {
		t.Error("the error quotes the token")
	}
}

func TestRelayPairingBadSource(t *testing.T) {
	relayTestAPI(t, true)

	for _, src := range []string{`not json`, `{"mode":"url","url":"https://relay.example"}`, `{"mode":"proxy","namespace":"ichor-relay","port":8080}`} {
		if _, err := RelayPairing("cfg", "ctx", "", src); err == nil {
			t.Errorf("%s: accepted", src)
		}
	}
}

// relayTestKeys is a valid Web Push subscription: a P-256 public key and a 16-byte secret.
func relayTestKeys(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(make([]byte, 16))
}

func TestRelayRegister(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f, headers := relayTestAPI(t, true)
	cfg := f.kubeconfigFor(f.URL)
	p256dh, auth := relayTestKeys(t)

	device := `{"platform":"unifiedpush","endpoint":"https://push.example/up/abc","p256dh":"` + p256dh + `","auth":"` + auth + `","label":"Pixel","id":"chosen-by-app","click":"https://elsewhere.example"}`

	out, err := RelayRegister(cfg, "admin@test", "", relayTestSource, device)
	if err != nil {
		t.Fatal(err)
	}

	var res struct{ ID string }
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.ID) != 16 {
		t.Fatalf("result %s %v", out, err)
	}

	// Registering again is idempotent: the same id for the same cluster and endpoint.
	again, err := RelayRegister(cfg, "admin@test", "", relayTestSource, device)
	if err != nil || again != out {
		t.Fatalf("again: %s %v", again, err)
	}

	var put fakeKubeRequest
	for _, r := range f.recorded() {
		if r.method == http.MethodPut {
			put = r
		}
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(put.body), &sent); err != nil || put.contentType != "application/json" {
		t.Fatalf("put %+v", put)
	}

	fingerprint, _ := contextIdentity(cfg, "admin@test")
	if sent["id"] != res.ID || sent["platform"] != "unifiedpush" || sent["cluster"] != fingerprint || fingerprint == "" || sent["label"] != "Pixel" || sent["click"] != nil {
		t.Errorf("sent %v", sent)
	}

	if h := headers(); len(h) != 2 || h[0] != relayTestToken {
		t.Errorf("pairing headers %q", h)
	}

	entries := readAudit(t, "", "")
	if len(entries) != 2 || entries[0].Action != "relay-pair" || entries[0].Object != "Service/ichor-relay" || entries[0].Namespace != "ichor-relay" ||
		entries[0].Outcome != auditOK || strings.Contains(entries[0].Params, "push.example") {
		t.Errorf("audit %+v", entries)
	}
}

func TestRelayRegisterNtfy(t *testing.T) {
	withDataDir(t)

	f, _ := relayTestAPI(t, true)
	cfg := f.kubeconfigFor(f.URL)

	if _, err := RelayRegister(cfg, "admin@test", "", relayTestSource, `{"platform":"ntfy","endpoint":"https://ntfy.example/topic","verbose":true}`); err != nil {
		t.Fatal(err)
	}

	var sent map[string]any
	for _, r := range f.recorded() {
		if r.method == http.MethodPut {
			_ = json.Unmarshal([]byte(r.body), &sent)
		}
	}

	_, id := contextIdentity(cfg, "admin@test")
	click, _ := sent["click"].(string)
	if !strings.HasPrefix(click, shareLinkWeb+"#") || !strings.Contains(click, "c="+id) || !strings.Contains(click, "t=alerts") || sent["verbose"] != true {
		t.Errorf("sent %v", sent)
	}
}

func TestRelayRegisterValidation(t *testing.T) {
	withDataDir(t)

	f, _ := relayTestAPI(t, true)
	cfg := f.kubeconfigFor(f.URL)
	p256dh, auth := relayTestKeys(t)
	keys := `,"p256dh":"` + p256dh + `","auth":"` + auth + `"`

	for _, tc := range []struct{ name, device, want string }{
		{"not json", `{`, "invalid device"},
		{"platform", `{"platform":"apns","endpoint":"https://push.example/x"}`, "platform"},
		{"plain http", `{"platform":"ntfy","endpoint":"http://ntfy.example/topic"}`, "https"},
		{"no host", `{"platform":"ntfy","endpoint":"https:///topic"}`, "endpoint"},
		{"too long", `{"platform":"ntfy","endpoint":"https://ntfy.example/` + strings.Repeat("a", relayMaxEndpoint) + `"}`, "2048"},
		{"no keys", `{"platform":"unifiedpush","endpoint":"https://push.example/x"}`, "p256dh"},
		{"bad key", `{"platform":"unifiedpush","endpoint":"https://push.example/x","p256dh":"!!","auth":"` + auth + `"}`, "p256dh"},
		{"keys for ntfy", `{"platform":"ntfy","endpoint":"https://ntfy.example/topic"` + keys + `}`, "ntfy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RelayRegister(cfg, "admin@test", "", relayTestSource, tc.device)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}

	for _, r := range f.recorded() {
		if r.method == http.MethodPut {
			t.Fatalf("an invalid device reached the relay: %s", r.body)
		}
	}
}

func TestRelayUnregister(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f, headers := relayTestAPI(t, true)
	cfg := f.kubeconfigFor(f.URL)

	if err := RelayUnregister(cfg, "admin@test", "", relayTestSource, " 0123456789abcdef "); err != nil {
		t.Fatal(err)
	}

	if h := headers(); len(h) != 1 || h[0] != relayTestToken {
		t.Errorf("pairing headers %q", h)
	}

	if err := RelayUnregister(cfg, "admin@test", "", relayTestSource, "../secrets"); err == nil {
		t.Error("a path in the id was accepted")
	}

	entries := readAudit(t, "", "")
	// Newest first: the refused id, then the removal.
	if len(entries) != 2 || entries[1].Action != "relay-unpair" || entries[1].Params != "device=0123456789abcdef" || entries[1].Outcome != auditOK ||
		entries[0].Outcome != auditFailed {
		t.Errorf("audit %+v", entries)
	}
}

func TestRelayDeviceID(t *testing.T) {
	a := relayDeviceID("fp-one", "https://push.example/a")

	switch {
	case len(a) != 16 || strings.Trim(a, "0123456789abcdef") != "":
		t.Errorf("not 16 hex characters: %q", a)
	case a != relayDeviceID("fp-one", "https://push.example/a"):
		t.Error("not stable")
	case a == relayDeviceID("fp-two", "https://push.example/a"), a == relayDeviceID("fp-one", "https://push.example/b"):
		t.Error("the cluster or the endpoint does not change it")
	}
}
