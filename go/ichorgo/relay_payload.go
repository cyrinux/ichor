package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cyrinux/ichor/go/relay"
)

// Bounds of a decrypted relay payload. A Web Push message is at most 4096 bytes, so a valid one
// is far below them: past them it is not the relay's.
const (
	relayMaxPayloadBytes  = 64 << 10
	relayMaxPayloadAlerts = 200
	relayMaxFingerprint   = 64
	relayMaxAlertname     = 256
	relayMaxNamespace     = 253
	relayMaxSummary       = 1000
)

// relayDecoded is a payload as the apps read it.
type relayDecoded struct {
	Cluster   string            `json:"cluster"`
	Alerts    []relay.PushAlert `json:"alerts"`
	Truncated bool              `json:"truncated"`
	Omitted   int               `json:"omitted"`
	SentAt    int64             `json:"sentAt"`
}

// RelayDecodePayload checks a payload the push distributor decrypted (a relay.Payload) and
// returns it for the apps: {cluster, alerts:[{fingerprint, alertname, severity, status,
// namespace, summary, startsAt (unix ms)}], truncated, omitted, sentAt}. cluster is the
// fingerprint of the cluster the device was paired from (see RelayRegister); severity is
// critical, warning, info or other; status firing or resolved; a long summary is clipped.
func RelayDecodePayload(payloadJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	if len(payloadJSON) > relayMaxPayloadBytes {
		return "", errors.New("the relay payload is too large")
	}

	var p relay.Payload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return "", errors.New("not a relay payload")
	}

	switch {
	case p.V != relay.PayloadVersion:
		return "", fmt.Errorf("relay payload version %d: update Ichor", p.V)
	case len(p.Alerts) > relayMaxPayloadAlerts:
		return "", fmt.Errorf("the relay payload has more than %d alerts", relayMaxPayloadAlerts)
	}

	res := relayDecoded{Cluster: p.Cluster, Alerts: make([]relay.PushAlert, 0, len(p.Alerts)), Truncated: p.Truncated, Omitted: max(p.Omitted, 0), SentAt: p.SentAt}

	for i, a := range p.Alerts {
		a, err := normalizePushAlert(a)
		if err != nil {
			return "", fmt.Errorf("relay payload alert %d: %w", i+1, err)
		}

		res.Alerts = append(res.Alerts, a)
	}

	return toJSON(res)
}

func normalizePushAlert(a relay.PushAlert) (relay.PushAlert, error) {
	switch {
	case a.Fingerprint == "" || len(a.Fingerprint) > relayMaxFingerprint:
		return a, fmt.Errorf("fingerprint: 1 to %d bytes", relayMaxFingerprint)
	case len(a.Alertname) > relayMaxAlertname:
		return a, fmt.Errorf("alertname: at most %d bytes", relayMaxAlertname)
	case len(a.Namespace) > relayMaxNamespace:
		return a, fmt.Errorf("namespace: at most %d bytes", relayMaxNamespace)
	case a.Status != relay.StatusFiring && a.Status != relay.StatusResolved:
		return a, fmt.Errorf("status: %s or %s", relay.StatusFiring, relay.StatusResolved)
	}

	a.Severity = relay.Severity(a.Severity)
	a.Summary = clipUTF8(a.Summary, relayMaxSummary-len("…"))
	a.StartsAt = max(a.StartsAt, 0)

	return a, nil
}
