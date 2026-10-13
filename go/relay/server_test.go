package relay

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWebhookPushesEncryptedPayloadPerDevice(t *testing.T) {
	tr := newTestRelay(t)
	push := newPushRecorder(t)
	phones := map[string]phone{"pixel": newPhone(t), "tablet": newPhone(t)}
	for id, p := range phones {
		tr.pair(t, p.device(id, push.srv.URL+"/up/"+id))
	}

	if code, body := tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader()); code != http.StatusAccepted {
		t.Fatalf("webhook: %d %s", code, body)
	}
	tr.drain(t)

	reqs := push.requests()
	if len(reqs) != len(phones) {
		t.Fatalf("got %d pushes, want %d", len(reqs), len(phones))
	}

	for _, req := range reqs {
		id := strings.TrimPrefix(req.path, "/up/")
		for header, want := range map[string]string{"Content-Encoding": "aes128gcm", "TTL": "86400", "Urgency": "high"} {
			if got := req.header.Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", id, header, got, want)
			}
		}
		if !strings.HasPrefix(req.header.Get("Authorization"), "vapid t=") {
			t.Errorf("%s: no VAPID authorization", id)
		}
		if len(req.body) > 4096 {
			t.Errorf("%s: encrypted body is %d bytes", id, len(req.body))
		}

		var p Payload
		if err := json.Unmarshal(phones[id].decrypt(t, req.body), &p); err != nil {
			t.Fatal(err)
		}
		if p.V != PayloadVersion || p.Cluster != "fp-"+id || p.SentAt != testNow.UnixMilli() || p.Truncated {
			t.Errorf("%s: payload header %+v", id, p)
		}
		if len(p.Alerts) != 2 || p.Alerts[0].Alertname != "NodeDown" || p.Alerts[0].Severity != SeverityCritical {
			t.Fatalf("%s: alerts %+v, want NodeDown (critical) first", id, p.Alerts)
		}
		want := PushAlert{
			Fingerprint: "a1b2c3d4e5f60718", Alertname: "KubePodCrashLooping", Severity: SeverityWarning,
			Status: StatusFiring, Namespace: "shop", Summary: "Pod shop/cart-7d9 is crash looping",
			StartsAt: time.Date(2026, 10, 13, 11, 50, 0, 0, time.UTC).UnixMilli(),
		}
		if p.Alerts[1] != want {
			t.Errorf("%s: alert %+v, want %+v", id, p.Alerts[1], want)
		}
	}
}

func TestWebhookNtfy(t *testing.T) {
	tr := newTestRelay(t)
	push := newPushRecorder(t)
	tr.pair(t, Device{ID: "iphone", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/ichor-abc", Label: "prod-eu", Click: "ichor://open?target=alerts"})
	tr.pair(t, Device{ID: "verbose", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/verbose", Verbose: true})

	tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader())
	tr.drain(t)

	got := map[string]recorded{}
	for _, r := range push.requests() {
		got[r.path] = r
	}

	plain := got["/ichor-abc"]
	title, err := new(mime.WordDecoder).DecodeHeader(plain.header.Get("Title"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Ichor · prod-eu: 2 firing (1 critical)"; title != want {
		t.Errorf("title %q, want %q", title, want)
	}
	for header, want := range map[string]string{"Priority": "5", "Tags": "rotating_light", "Click": "ichor://open?target=alerts"} {
		if got := plain.header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if want := "critical NodeDown\nwarning KubePodCrashLooping (shop)"; string(plain.body) != want {
		t.Errorf("body %q, want %q", plain.body, want)
	}

	if body := string(got["/verbose"].body); !strings.Contains(body, "NodeDown: worker-2 is down") {
		t.Errorf("verbose body %q lacks the summary", body)
	}
}

func TestTokenRefusals(t *testing.T) {
	tr := newTestRelay(t)
	state := tr.store.Snapshot()
	device := Device{ID: "x", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t"}

	cases := []struct {
		name, method, path string
		body               any
		headers            map[string]string
	}{
		{"pairing without token", http.MethodGet, "/v1/pairing", nil, nil},
		{"pairing with webhook token", http.MethodGet, "/v1/pairing", nil, map[string]string{pairingHeader: state.WebhookToken}},
		{"put device wrong token", http.MethodPut, "/v1/devices", device, map[string]string{pairingHeader: "nope"}},
		{"delete device without token", http.MethodDelete, "/v1/devices/x", nil, nil},
		{"webhook without token", http.MethodPost, "/alertmanager", sampleWebhook, nil},
		{"webhook with pairing token", http.MethodPost, "/alertmanager", sampleWebhook, map[string]string{"Authorization": "Bearer " + state.PairingToken}},
		{"webhook token not bearer", http.MethodPost, "/alertmanager", sampleWebhook, map[string]string{"Authorization": state.WebhookToken}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := tr.call(t, c.method, c.path, c.body, c.headers)
			if code != http.StatusUnauthorized {
				t.Errorf("status %d, want 401", code)
			}
			if strings.Contains(body, state.WebhookToken) || strings.Contains(body, state.PairingToken) {
				t.Errorf("body %q echoes a token", body)
			}
			var e struct{ Error string }
			if json.Unmarshal([]byte(body), &e) != nil || e.Error == "" {
				t.Errorf("body %q is not a JSON error", body)
			}
		})
	}
}

func TestPutDeviceValidation(t *testing.T) {
	tr := newTestRelay(t)
	good := newPhone(t).device("ok", "https://up.example/x")

	cases := []struct {
		name   string
		edit   func(*Device)
		status int
	}{
		{"valid", func(*Device) {}, http.StatusNoContent},
		{"id with a slash", func(d *Device) { d.ID = "a/b" }, http.StatusBadRequest},
		{"id too long", func(d *Device) { d.ID = strings.Repeat("a", 65) }, http.StatusBadRequest},
		{"unknown platform", func(d *Device) { d.Platform = "apns" }, http.StatusBadRequest},
		{"ftp endpoint", func(d *Device) { d.Endpoint = "ftp://up.example/x" }, http.StatusBadRequest},
		{"endpoint without host", func(d *Device) { d.Endpoint = "https:///x" }, http.StatusBadRequest},
		{"missing auth", func(d *Device) { d.Auth = "" }, http.StatusBadRequest},
		{"p256dh not a point", func(d *Device) { d.P256DH = "AAAA" }, http.StatusBadRequest},
		{"label too long", func(d *Device) { d.Label = strings.Repeat("l", 101) }, http.StatusBadRequest},
		{"click javascript", func(d *Device) { d.Click = "javascript:alert(1)" }, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := good
			c.edit(&d)
			if code, body := tr.call(t, http.MethodPut, "/v1/devices", d, tr.pairingHeader()); code != c.status {
				t.Errorf("status %d (%s), want %d", code, body, c.status)
			}
		})
	}

	if code, _ := tr.call(t, http.MethodPut, "/v1/devices", `{"id":"`+strings.Repeat("a", 20<<10)+`"}`, tr.pairingHeader()); code != http.StatusRequestEntityTooLarge {
		t.Errorf("large body: %d, want 413", code)
	}
	if code, _ := tr.call(t, http.MethodPut, "/v1/devices", `{`, tr.pairingHeader()); code != http.StatusBadRequest {
		t.Errorf("bad JSON: %d, want 400", code)
	}
}

func TestDeviceCapAndUpsert(t *testing.T) {
	tr := newTestRelay(t)
	err := tr.store.Update(t.Context(), func(s *State) error {
		for i := range MaxDevices {
			s.Devices = append(s.Devices, Device{ID: fmt.Sprintf("d%02d", i), Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t", CreatedAt: 42})
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	extra := Device{ID: "one-more", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t"}
	if code, _ := tr.call(t, http.MethodPut, "/v1/devices", extra, tr.pairingHeader()); code != http.StatusRequestEntityTooLarge {
		t.Errorf("51st device: %d, want 413", code)
	}

	existing := tr.store.Snapshot().Devices[0]
	existing.Label = "renamed"
	tr.pair(t, existing)

	got := tr.store.Snapshot().Devices[0]
	if got.Label != "renamed" || got.CreatedAt != 42 {
		t.Errorf("upsert: %+v, want the label replaced and CreatedAt kept", got)
	}
}

func TestDeleteDevice(t *testing.T) {
	tr := newTestRelay(t)
	tr.pair(t, Device{ID: "gone-soon", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t"})

	for range 2 { // deleting twice is fine
		if code, body := tr.call(t, http.MethodDelete, "/v1/devices/gone-soon", nil, tr.pairingHeader()); code != http.StatusNoContent {
			t.Fatalf("delete: %d %s", code, body)
		}
	}
	if devices := tr.kube.data(t, keyDevices); devices != "[]" {
		t.Errorf("devices.json = %s, want []", devices)
	}
}

func TestGoneEndpointDropsDevice(t *testing.T) {
	tr := newTestRelay(t)
	push := newPushRecorder(t)
	tr.pair(t, newPhone(t).device("old", push.srv.URL+"/gone/old"))
	tr.pair(t, newPhone(t).device("live", push.srv.URL+"/up/live"))

	tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader())
	tr.drain(t)

	var devices []Device
	if err := json.Unmarshal([]byte(tr.kube.data(t, keyDevices)), &devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != "live" {
		t.Errorf("devices after a 410: %+v, want only live", devices)
	}
}

func TestRepeatedWebhookIsDeduped(t *testing.T) {
	tr := newTestRelay(t)
	push := newPushRecorder(t)
	tr.pair(t, Device{ID: "n", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/t"})

	tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader())
	push.wait(t, 1)
	tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader())
	resolved := strings.Replace(sampleWebhook, `"status": "firing",
      "labels": {"alertname": "NodeDown"`, `"status": "resolved",
      "labels": {"alertname": "NodeDown"`, 1)
	tr.call(t, http.MethodPost, "/alertmanager", resolved, tr.webhookHeader())
	tr.drain(t)

	reqs := push.requests()
	if len(reqs) != 2 {
		t.Fatalf("got %d pushes, want 2 (the first, then NodeDown resolved)", len(reqs))
	}
	if body := string(reqs[1].body); body != "resolved: critical NodeDown" {
		t.Errorf("second push %q, want only the resolved alert", body)
	}
}

func TestWebhookBodyRefusals(t *testing.T) {
	tr := newTestRelay(t)
	if code, _ := tr.call(t, http.MethodPost, "/alertmanager", "{", tr.webhookHeader()); code != http.StatusBadRequest {
		t.Errorf("bad JSON: %d, want 400", code)
	}
	huge := `{"alerts":[],"receiver":"` + strings.Repeat("x", 2<<20) + `"}`
	if code, _ := tr.call(t, http.MethodPost, "/alertmanager", huge, tr.webhookHeader()); code != http.StatusRequestEntityTooLarge {
		t.Errorf("2 MiB body: %d, want 413", code)
	}
	if code, _ := tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader()); code != http.StatusAccepted {
		t.Errorf("no device: %d, want 202", code)
	}
}

func TestPairingAndHealth(t *testing.T) {
	tr := newTestRelay(t)
	if code, _ := tr.call(t, http.MethodGet, "/healthz", nil, nil); code != http.StatusOK {
		t.Errorf("healthz: %d", code)
	}

	code, body := tr.call(t, http.MethodGet, "/v1/pairing", nil, tr.pairingHeader())
	var got struct{ VAPIDPublicKey string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil || got.VAPIDPublicKey != tr.store.Snapshot().VAPIDPublicKey {
		t.Errorf("pairing: %d %s", code, body)
	}
}

func TestStaleDeliveryKeepsARepairedDevice(t *testing.T) {
	tr := newTestRelay(t)
	push := newPushRecorder(t)
	p := newPhone(t)
	oldEndpoint, newEndpoint := push.srv.URL+"/gone/old", push.srv.URL+"/up/new"
	tr.pair(t, p.device("pixel", oldEndpoint))

	// The phone re-pairs with a new endpoint before the 410 of the old one is handled: the
	// removal keyed on the old endpoint must leave the new pairing alone.
	tr.pair(t, p.device("pixel", newEndpoint))
	if err := tr.relay.removeDevice(t.Context(), "pixel", oldEndpoint); err != nil {
		t.Fatal(err)
	}
	if devices := tr.store.Snapshot().Devices; len(devices) != 1 || devices[0].Endpoint != newEndpoint {
		t.Fatalf("devices %+v, want the re-paired one kept", devices)
	}

	// A delivery for a device removed since the webhook sends nothing.
	tr.relay.deliver(delivery{deviceID: "unknown", alerts: []PushAlert{{Fingerprint: "f", Status: StatusFiring}}})
	if n := len(push.requests()); n != 0 {
		t.Errorf("%d pushes for an unknown device", n)
	}
}

func TestWebhookAfterCloseIsRefused(t *testing.T) {
	tr := newTestRelay(t)
	tr.pair(t, Device{ID: "n", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t"})
	tr.drain(t)

	if code, _ := tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader()); code != http.StatusServiceUnavailable {
		t.Errorf("webhook after Close: %d, want 503", code)
	}
}
