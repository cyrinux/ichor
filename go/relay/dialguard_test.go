package relay

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestEndpointAddressPolicy(t *testing.T) {
	cases := []struct {
		addr               string
		strict, permissive bool // allowed without, and with, RELAY_ALLOW_PRIVATE
	}{
		{"203.0.113.10", true, true},
		{"2001:db8::1", true, true},
		{"127.0.0.1", false, true},
		{"::1", false, true},
		{"10.96.0.10", false, true},
		{"172.16.4.2", false, true},
		{"192.168.1.20", false, true},
		{"100.64.0.7", false, true},
		{"fd00::7", false, true},
		{"169.254.169.254", false, false}, // the cloud metadata service
		{"fe80::1", false, false},
		{"0.0.0.0", false, false},
		{"::", false, false},
		{"224.0.0.1", false, false},
		{"ff02::1", false, false},
		{"::ffff:127.0.0.1", false, true},
		{"::ffff:169.254.169.254", false, false},
	}
	for _, c := range cases {
		ip := netip.MustParseAddr(c.addr)
		if got := (addrPolicy{}).check(ip) == nil; got != c.strict {
			t.Errorf("%s by default: allowed=%v, want %v", c.addr, got, c.strict)
		}
		if got := (addrPolicy{allowPrivate: true}).check(ip) == nil; got != c.permissive {
			t.Errorf("%s with RELAY_ALLOW_PRIVATE: allowed=%v, want %v", c.addr, got, c.permissive)
		}
	}
}

func TestEndpointDialRefusesLoopbackUnlessAllowed(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	post := func(c *http.Client) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		return err
	}

	if err := post(newPushClient(addrPolicy{})); !errors.Is(err, errAddressRefused) {
		t.Errorf("loopback by default: %v, want errAddressRefused", err)
	}
	if hits != 0 {
		t.Fatalf("the refused dial reached the server %d times", hits)
	}
	if err := post(newPushClient(addrPolicy{allowPrivate: true})); err != nil {
		t.Errorf("loopback with RELAY_ALLOW_PRIVATE: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
}

func TestEndpointRefusedDeliveryLogsTheDeviceOnly(t *testing.T) {
	var logs bytes.Buffer
	tr := newTestRelay(t)
	// Set before any delivery is queued: the workers read them only after the channel send.
	tr.relay.log = slog.New(slog.NewTextHandler(&logs, nil))
	tr.relay.sender.client = newPushClient(addrPolicy{})

	push := newPushRecorder(t)
	tr.pair(t, Device{ID: "phone-1", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/topic"})
	if code, body := tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader()); code != http.StatusAccepted {
		t.Fatalf("webhook: %d %s", code, body)
	}
	tr.drain(t)

	if n := len(push.requests()); n != 0 {
		t.Fatalf("%d pushes reached a loopback endpoint", n)
	}
	out := logs.String()
	if !strings.Contains(out, "push endpoint address refused") || !strings.Contains(out, "device=phone-1") {
		t.Errorf("log %q: want the refusal with the device id", out)
	}
	if strings.Contains(out, "127.0.0.1") || strings.Contains(out, push.srv.URL) {
		t.Errorf("log %q names the endpoint", out)
	}
}
