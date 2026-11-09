// Package relay is ichor-relay's core: it receives Alertmanager's webhook in the cluster and
// pushes the alerts to the paired phones, through UnifiedPush (RFC 8291 Web Push, encrypted
// end to end) or a plain ntfy topic.
//
// It holds the wire types the apps share with the relay (Device, Payload, PushAlert), so it
// must stay free of gomobile and of ichorgo: the standard library and webpush-go only.
package relay

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
	"time"
)

// Platforms a device is paired on.
const (
	PlatformUnifiedPush = "unifiedpush"
	PlatformNtfy        = "ntfy"
)

// Alert statuses, as Alertmanager's webhook names them.
const (
	StatusFiring   = "firing"
	StatusResolved = "resolved"
)

// Severity levels the alerts are folded into, most severe first.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
	SeverityOther    = "other"
)

// PayloadVersion is Payload.V; the phone ignores a payload of a version it does not know.
const PayloadVersion = 1

// Device is one paired phone. ID is chosen by the phone; Cluster is the phone's fingerprint of
// the cluster, opaque to the relay and sent back in every Payload. Click (ntfy) is the deep
// link opened when the notification is tapped; Verbose (ntfy) adds the alerts' summaries to
// the plain-text message.
type Device struct {
	ID        string `json:"id"`
	Platform  string `json:"platform"`
	Endpoint  string `json:"endpoint"`
	P256DH    string `json:"p256dh,omitempty"`
	Auth      string `json:"auth,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	Label     string `json:"label,omitempty"`
	Click     string `json:"click,omitempty"`
	Verbose   bool   `json:"verbose,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// Payload is what a UnifiedPush device decrypts. Times are unix ms. Truncated is set when the
// most severe alerts were kept to fit a push message; Omitted counts the ones left out.
type Payload struct {
	V         int         `json:"v"`
	Cluster   string      `json:"cluster,omitempty"`
	Alerts    []PushAlert `json:"alerts"`
	SentAt    int64       `json:"sentAt"`
	Truncated bool        `json:"truncated,omitempty"`
	Omitted   int         `json:"omitted,omitempty"`
}

// PushAlert is one alert in a Payload. StartsAt is unix ms (0: unset).
type PushAlert struct {
	Fingerprint string `json:"fingerprint"`
	Alertname   string `json:"alertname"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	Namespace   string `json:"namespace,omitempty"`
	Summary     string `json:"summary,omitempty"`
	StartsAt    int64  `json:"startsAt,omitempty"`
}

// WebhookMessage is the body Alertmanager's webhook_config posts (version 4).
type WebhookMessage struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []WebhookAlert    `json:"alerts"`
}

// WebhookAlert is one alert of a WebhookMessage.
type WebhookAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// severityAliases folds the usual spellings of the severity label into four levels (the same
// rules as the app's Alertmanager reader).
var severityAliases = map[string]string{
	"critical": SeverityCritical, "crit": SeverityCritical, "page": SeverityCritical, "emergency": SeverityCritical, "fatal": SeverityCritical,
	"warning": SeverityWarning, "warn": SeverityWarning, "error": SeverityWarning, "high": SeverityWarning, "major": SeverityWarning,
	"info": SeverityInfo, "informational": SeverityInfo, "notice": SeverityInfo, "low": SeverityInfo, "minor": SeverityInfo, "none": SeverityInfo,
}

var severityOrder = []string{SeverityCritical, SeverityWarning, SeverityInfo, SeverityOther}

// Severity folds a severity label into one of the four levels.
func Severity(label string) string {
	if s, ok := severityAliases[strings.ToLower(strings.TrimSpace(label))]; ok {
		return s
	}

	return SeverityOther
}

func severityRank(s string) int {
	if i := slices.Index(severityOrder, s); i >= 0 {
		return i
	}

	return len(severityOrder)
}

// pushAlert converts a webhook alert. A missing fingerprint (an old Alertmanager) is derived
// from the labels, so dedupe still works.
func pushAlert(a WebhookAlert) PushAlert {
	status := StatusFiring
	if a.Status == StatusResolved {
		status = StatusResolved
	}

	var startsAt int64
	if !a.StartsAt.IsZero() {
		startsAt = a.StartsAt.UnixMilli()
	}

	return PushAlert{
		Fingerprint: cmp.Or(a.Fingerprint, labelsFingerprint(a.Labels)),
		Alertname:   a.Labels["alertname"],
		Severity:    Severity(a.Labels["severity"]),
		Status:      status,
		Namespace:   a.Labels["namespace"],
		Summary:     a.Annotations["summary"],
		StartsAt:    startsAt,
	}
}

func labelsFingerprint(labels map[string]string) string {
	h := sha256.New()
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		h.Write([]byte(k + "\x00" + labels[k] + "\x00"))
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}
