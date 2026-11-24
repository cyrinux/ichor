package relay

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestQuietWindowUntil(t *testing.T) {
	at := func(value string) time.Time {
		t.Helper()

		return must(time.Parse(time.RFC3339, value))
	}
	night := QuietHours{Start: "23:00", End: "07:00", TZ: "Europe/Paris"}
	lunch := QuietHours{Start: "12:00", End: "14:00", TZ: "UTC"}

	cases := []struct {
		name       string
		quiet      QuietHours
		now, until string
	}{
		{"over midnight, the evening", night, "2026-06-01T21:30:00Z", "2026-06-02T05:00:00Z"},
		{"over midnight, the morning", night, "2026-06-02T04:59:00Z", "2026-06-02T05:00:00Z"},
		{"the end is outside", night, "2026-06-02T05:00:00Z", ""},
		{"by day", night, "2026-06-01T12:00:00Z", ""},
		{"within a day", lunch, "2026-06-01T12:00:00Z", "2026-06-01T14:00:00Z"},
		{"before it", lunch, "2026-06-01T11:59:00Z", ""},
		{"the spring DST night", night, "2026-03-28T22:30:00Z", "2026-03-29T05:00:00Z"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, err := c.quiet.Window()
			if err != nil || w == nil {
				t.Fatalf("window = %v, %v", w, err)
			}

			var want time.Time
			if c.until != "" {
				want = at(c.until)
			}

			if got := w.Until(at(c.now)); !got.Equal(want) {
				t.Errorf("until = %s, want %s", got, want)
			}
		})
	}
}

func TestQuietHoursWindowErrors(t *testing.T) {
	if w, err := (QuietHours{Start: "07:00", End: "07:00"}).Window(); w != nil || err != nil {
		t.Errorf("start == end: %v, %v; want no window", w, err)
	}

	cases := []struct {
		quiet QuietHours
		want  string
	}{
		{QuietHours{Start: "25:00", End: "07:00", TZ: "UTC"}, `start "25:00": want HH:MM`},
		{QuietHours{Start: "23:00", End: "7:00", TZ: "UTC"}, `end "7:00": want HH:MM`},
		{QuietHours{Start: "23:00", End: "07:00"}, "no time zone"},
		{QuietHours{Start: "23:00", End: "07:00", TZ: "Mars/Olympus"}, `"Mars/Olympus" is not an IANA zone`},
		{QuietHours{Start: "23:00", End: "07:00", TZ: "Local"}, `"Local" is not an IANA zone`},
	}

	for _, c := range cases {
		if _, err := c.quiet.Window(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err = %v, want it to contain %q", c.quiet, err, c.want)
		}
	}
}

func TestQuietAlerts(t *testing.T) {
	alerts := []PushAlert{
		{Fingerprint: "a", Severity: SeverityCritical, Status: StatusFiring},
		{Fingerprint: "b", Severity: SeverityWarning, Status: StatusFiring},
		{Fingerprint: "c", Severity: SeverityCritical, Status: StatusResolved},
	}
	inside := QuietHours{Start: "11:00", End: "13:00", TZ: "UTC"}
	letCritical := QuietHours{Start: "11:00", End: "13:00", TZ: "UTC", ExceptCritical: true}

	cases := []struct {
		name  string
		quiet *QuietHours
		want  string
	}{
		{"no quiet hours", nil, "abc"},
		{"outside the window", &QuietHours{Start: "23:00", End: "07:00", TZ: "UTC"}, "abc"},
		{"inside", &inside, ""},
		{"inside, critical let through with its resolution", &letCritical, "ac"},
		{"a window that cannot be read drops nothing", &QuietHours{Start: "11:00", End: "13:00"}, "abc"},
	}

	for _, c := range cases {
		var got string
		for _, a := range quietAlerts(c.quiet, alerts, testNow) {
			got += a.Fingerprint
		}

		if got != c.want {
			t.Errorf("%s: kept %q, want %q", c.name, got, c.want)
		}
	}
}

func TestQuietHoursHoldBackADevicesPushes(t *testing.T) {
	tr := newTestRelay(t)
	push := newPushRecorder(t)
	tr.pair(t, Device{ID: "quiet", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/quiet"})
	tr.pair(t, Device{ID: "critical", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/critical"})
	tr.pair(t, Device{ID: "awake", Platform: PlatformNtfy, Endpoint: push.srv.URL + "/awake"})

	// testNow is 12:00 UTC, 14:00 in Paris.
	set := func(id string, quiet QuietHours) {
		t.Helper()

		if code, body := tr.call(t, http.MethodPut, "/v1/devices/"+id+"/quiet", quiet, tr.pairingHeader()); code != http.StatusNoContent {
			t.Fatalf("quiet hours of %s: %d %s", id, code, body)
		}
	}
	set("quiet", QuietHours{Start: "11:00", End: "13:00", TZ: "UTC"})
	set("critical", QuietHours{Start: "13:00", End: "15:00", TZ: "Europe/Paris", ExceptCritical: true})

	tr.call(t, http.MethodPost, "/alertmanager", sampleWebhook, tr.webhookHeader())
	tr.drain(t)

	got := map[string]string{}
	for _, r := range push.requests() {
		got[r.path] = string(r.body)
	}

	if _, ok := got["/quiet"]; ok {
		t.Errorf("the quiet device was pushed %q", got["/quiet"])
	}
	if want := "critical NodeDown"; got["/critical"] != want {
		t.Errorf("except critical: %q, want %q", got["/critical"], want)
	}
	if !strings.Contains(got["/awake"], "warning KubePodCrashLooping") {
		t.Errorf("no quiet hours: %q, want every alert", got["/awake"])
	}
}

func TestPutQuiet(t *testing.T) {
	tr := newTestRelay(t)
	tr.pair(t, Device{ID: "iphone", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t", Label: "prod-eu"})
	night := QuietHours{Start: "23:00", End: "07:00", TZ: "Europe/Paris", ExceptCritical: true}
	path := "/v1/devices/iphone/quiet"
	stored := func() *QuietHours { return tr.store.Snapshot().Devices[0].Quiet }

	if code, _ := tr.call(t, http.MethodPut, path, night, nil); code != http.StatusUnauthorized {
		t.Errorf("no pairing token: %d, want 401", code)
	}
	if code, body := tr.call(t, http.MethodPut, "/v1/devices/nobody/quiet", night, tr.pairingHeader()); code != http.StatusNotFound ||
		!strings.Contains(body, `"error":"unknown device"`) {
		t.Errorf("unknown device: %d %s, want 404 with a JSON error", code, body)
	}
	if code, body := tr.call(t, http.MethodPut, path, QuietHours{Start: "23:00", End: "07:00"}, tr.pairingHeader()); code != http.StatusBadRequest ||
		!strings.Contains(body, "no time zone") {
		t.Errorf("no zone: %d %s, want 400", code, body)
	}
	if stored() != nil {
		t.Fatalf("refused quiet hours were stored: %+v", stored())
	}

	for range 2 { // the second time changes nothing and is not an error
		if code, body := tr.call(t, http.MethodPut, path, night, tr.pairingHeader()); code != http.StatusNoContent {
			t.Fatalf("set: %d %s", code, body)
		}
	}
	if got := stored(); got == nil || *got != night {
		t.Fatalf("stored %+v, want %+v", got, night)
	}

	// Pairing again (a new label) keeps them: a pairing carries none, even when it sends some.
	tr.pair(t, Device{ID: "iphone", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/t", Label: "renamed", Quiet: &QuietHours{Start: "01:00", End: "02:00", TZ: "UTC"}})
	if got := stored(); got == nil || *got != night {
		t.Errorf("after pairing again: %+v, want %+v", got, night)
	}

	if code, body := tr.call(t, http.MethodPut, path, QuietHours{}, tr.pairingHeader()); code != http.StatusNoContent {
		t.Fatalf("clear: %d %s", code, body)
	}
	if stored() != nil {
		t.Errorf("cleared, still %+v", stored())
	}

	tr.pair(t, Device{ID: "new", Platform: PlatformNtfy, Endpoint: "https://ntfy.example/n", Quiet: &night})
	if got := tr.store.Snapshot().Devices[1].Quiet; got != nil {
		t.Errorf("a first pairing set quiet hours: %+v", got)
	}
}
