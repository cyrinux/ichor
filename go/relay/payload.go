package relay

import (
	"cmp"
	"encoding/json"
	"slices"
	"sync"
	"time"
)

const (
	// maxPlaintext bounds a Payload's JSON: Web Push allows 4096 bytes once encrypted, and
	// RFC 8291's header, padding delimiter and tag take about 100 of them.
	maxPlaintext = 3800
	// dedupeWindow is how long a (device, alert, status) delivery silences the same one, so
	// Alertmanager's repeat_interval resends do not notify again.
	dedupeWindow = 5 * time.Minute
)

// webhookAlerts converts the webhook's alerts, most severe first, firing before resolved, then
// newest first.
func webhookAlerts(msg WebhookMessage) []PushAlert {
	alerts := make([]PushAlert, 0, len(msg.Alerts))
	for _, a := range msg.Alerts {
		alerts = append(alerts, pushAlert(a))
	}

	slices.SortStableFunc(alerts, func(a, b PushAlert) int {
		return cmp.Or(
			cmp.Compare(severityRank(a.Severity), severityRank(b.Severity)),
			cmp.Compare(statusRank(a.Status), statusRank(b.Status)),
			cmp.Compare(b.StartsAt, a.StartsAt),
		)
	})

	return alerts
}

func statusRank(status string) int {
	if status == StatusFiring {
		return 0
	}

	return 1
}

// buildPayload returns the payload JSON for alerts (sorted most severe first), keeping as many
// as fit in maxPlaintext.
func buildPayload(cluster string, alerts []PushAlert, now time.Time) ([]byte, error) {
	encode := func(n int) ([]byte, error) {
		p := Payload{V: PayloadVersion, Cluster: cluster, Alerts: alerts[:n], SentAt: now.UnixMilli()}
		if n < len(alerts) {
			p.Truncated, p.Omitted = true, len(alerts)-n
		}

		return json.Marshal(p)
	}

	full, err := encode(len(alerts))
	if err != nil || len(full) <= maxPlaintext {
		return full, err
	}

	// The largest n that fits: encode(0) always does (a cluster fingerprint is short).
	var fitErr error
	n := largestFitting(len(alerts), func(n int) bool {
		buf, err := encode(n)
		if err != nil {
			fitErr = err
		}

		return err == nil && len(buf) <= maxPlaintext
	})
	if fitErr != nil {
		return nil, fitErr
	}

	return encode(n)
}

// largestFitting is the largest n in [0, limit] for which fits holds, fits being monotonic (true then
// false) and fits(0) assumed.
func largestFitting(limit int, fits func(int) bool) int {
	lo, hi := 0, limit
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	return lo
}

// dedupe remembers the (device, fingerprint, status) deliveries of the last dedupeWindow.
type dedupe struct {
	mu   sync.Mutex
	seen map[dedupeKey]time.Time
	now  func() time.Time
}

type dedupeKey struct{ device, fingerprint, status string }

func newDedupe(now func() time.Time) *dedupe {
	return &dedupe{seen: map[dedupeKey]time.Time{}, now: now}
}

// claim returns the alerts not delivered to device within the window and marks them as
// delivered; release undoes it for a delivery that failed.
func (d *dedupe) claim(device string, alerts []PushAlert) []PushAlert {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	for k, at := range d.seen {
		if now.Sub(at) >= dedupeWindow {
			delete(d.seen, k)
		}
	}

	fresh := make([]PushAlert, 0, len(alerts))
	for _, a := range alerts {
		key := dedupeKey{device, a.Fingerprint, a.Status}
		if _, ok := d.seen[key]; ok {
			continue
		}
		d.seen[key] = now
		fresh = append(fresh, a)
	}

	return fresh
}

func (d *dedupe) release(device string, alerts []PushAlert) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, a := range alerts {
		delete(d.seen, dedupeKey{device, a.Fingerprint, a.Status})
	}
}

// forget drops a removed device's entries.
func (d *dedupe) forget(device string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for k := range d.seen {
		if k.device == device {
			delete(d.seen, k)
		}
	}
}
