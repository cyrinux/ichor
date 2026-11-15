package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRelayDecodePayload(t *testing.T) {
	alert := func(fp, status string) string {
		return `{"fingerprint":"` + fp + `","alertname":"DiskFull","severity":"crit","status":"` + status + `","namespace":"prod-eu","summary":"disk at 97%","startsAt":1700000000000}`
	}
	many := make([]string, relayMaxPayloadAlerts+1)
	for i := range many {
		many[i] = alert("fp", "firing")
	}

	for _, tc := range []struct {
		name, payload, want, err string
	}{
		{
			name:    "normalised",
			payload: `{"v":1,"cluster":"homelab-fp","alerts":[` + alert("a1", "firing") + `,` + alert("a2", "resolved") + `],"sentAt":1700000001000,"extra":true}`,
			want: `{"cluster":"homelab-fp","alerts":[` +
				`{"fingerprint":"a1","alertname":"DiskFull","severity":"critical","status":"firing","namespace":"prod-eu","summary":"disk at 97%","startsAt":1700000000000},` +
				`{"fingerprint":"a2","alertname":"DiskFull","severity":"critical","status":"resolved","namespace":"prod-eu","summary":"disk at 97%","startsAt":1700000000000}],` +
				`"truncated":false,"omitted":0,"sentAt":1700000001000}`,
		},
		{
			name:    "truncated",
			payload: `{"v":1,"cluster":"homelab-fp","alerts":[` + alert("a1", "firing") + `],"sentAt":1,"truncated":true,"omitted":12}`,
			want:    `"truncated":true,"omitted":12`,
		},
		{name: "no alerts", payload: `{"v":1,"alerts":[],"sentAt":1}`, want: `"alerts":[]`},
		{name: "not json", payload: `{"v":1`, err: "not a relay payload"},
		{name: "version", payload: `{"v":2,"alerts":[],"sentAt":1}`, err: "version 2"},
		{name: "too many", payload: `{"v":1,"alerts":[` + strings.Join(many, ",") + `],"sentAt":1}`, err: "more than 200 alerts"},
		{name: "status", payload: `{"v":1,"alerts":[` + alert("a1", "pending") + `],"sentAt":1}`, err: "status"},
		{name: "no fingerprint", payload: `{"v":1,"alerts":[` + alert("", "firing") + `],"sentAt":1}`, err: "fingerprint"},
		{name: "long name", payload: `{"v":1,"alerts":[{"fingerprint":"a","alertname":"` + strings.Repeat("x", 300) + `","status":"firing"}],"sentAt":1}`, err: "alertname"},
		{name: "too large", payload: `{"v":1,"cluster":"` + strings.Repeat("x", relayMaxPayloadBytes) + `","alerts":[],"sentAt":1}`, err: "too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RelayDecodePayload(tc.payload)

			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("got %s %v, want error %q", out, err, tc.err)
				}

				return
			}

			if err != nil || !strings.Contains(out, tc.want) {
				t.Fatalf("got %s %v, want %s", out, err, tc.want)
			}
		})
	}
}

func TestRelayDecodePayloadClipsSummary(t *testing.T) {
	out, err := RelayDecodePayload(`{"v":1,"alerts":[{"fingerprint":"a","alertname":"A","status":"firing","summary":"` + strings.Repeat("é", 1000) + `"}],"sentAt":1}`)
	if err != nil {
		t.Fatal(err)
	}

	var got relayDecoded
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if s := got.Alerts[0].Summary; len(s) > relayMaxSummary || !strings.HasPrefix(s, "é") || got.Alerts[0].Severity != "other" {
		t.Errorf("summary of %d bytes, severity %q", len(s), got.Alerts[0].Severity)
	}
}
