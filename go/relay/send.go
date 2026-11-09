package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	// pushTTL is how long a push server keeps a message for an offline phone.
	pushTTL = 24 * 60 * 60
	// vapidSubscriber is the VAPID "sub" claim: who runs the application server.
	vapidSubscriber = "https://github.com/cyrinux/ichor"
	// maxNtfyBody keeps an ntfy message under ntfy's 4096-byte limit, past which it would
	// become an attachment.
	maxNtfyBody = 3800
)

// errGone is a push endpoint's 404 or 410: the subscription is gone and the device is dropped.
var errGone = errors.New("push endpoint gone")

// sender delivers one device's alerts.
type sender struct {
	client *http.Client
	now    func() time.Time
}

// send delivers alerts (sorted most severe first, not empty) to device; the answer's status
// is returned for the log.
func (s *sender) send(ctx context.Context, state State, device Device, alerts []PushAlert) (int, error) {
	switch device.Platform {
	case PlatformUnifiedPush:
		return s.sendWebPush(ctx, state, device, alerts)
	case PlatformNtfy:
		return s.sendNtfy(ctx, device, alerts)
	default:
		return 0, fmt.Errorf("unknown platform %q", device.Platform)
	}
}

func (s *sender) sendNtfy(ctx context.Context, device Device, alerts []PushAlert) (int, error) {
	req, err := ntfyRequest(ctx, device, alerts)
	if err != nil {
		return 0, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}

	return pushStatus(resp)
}

func (s *sender) sendWebPush(ctx context.Context, state State, device Device, alerts []PushAlert) (int, error) {
	body, err := buildPayload(device.Cluster, alerts, s.now())
	if err != nil {
		return 0, err
	}

	urgency := webpush.UrgencyNormal
	if hasFiringCritical(alerts) {
		urgency = webpush.UrgencyHigh
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body,
		&webpush.Subscription{Endpoint: device.Endpoint, Keys: webpush.Keys{Auth: device.Auth, P256dh: device.P256DH}},
		&webpush.Options{
			HTTPClient:      s.client,
			Subscriber:      vapidSubscriber,
			TTL:             pushTTL,
			Urgency:         urgency,
			VAPIDPublicKey:  state.VAPIDPublicKey,
			VAPIDPrivateKey: state.VAPIDPrivateKey,
		})
	if err != nil {
		return 0, err
	}

	return pushStatus(resp)
}

// pushStatus maps a push server's answer: 2xx is delivered, 404/410 is errGone.
func pushStatus(resp *http.Response) (int, error) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()

	switch code := resp.StatusCode; {
	case code/100 == 2:
		return code, nil
	case code == http.StatusNotFound || code == http.StatusGone:
		return code, errGone
	default:
		return code, fmt.Errorf("push server answered %d", code)
	}
}

// ntfyRequest is a plain ntfy publish: counts in the title, one line per alert, and the
// summaries only for a device paired as verbose. It is not end-to-end encrypted.
func ntfyRequest(ctx context.Context, device Device, alerts []PushAlert) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, device.Endpoint, strings.NewReader(ntfyBody(device, alerts)))
	if err != nil {
		return nil, err
	}

	priority, tags := "3", "white_check_mark"
	if firing, _ := counts(alerts); firing > 0 {
		tags = "rotating_light"
	}
	if hasFiringCritical(alerts) {
		priority = "5"
	}

	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("Title", mime.BEncoding.Encode("utf-8", ntfyTitle(device, alerts)))
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)
	if device.Click != "" {
		req.Header.Set("Click", device.Click)
	}

	return req, nil
}

func ntfyTitle(device Device, alerts []PushAlert) string {
	label := device.Label
	if label == "" {
		label = "Alertmanager"
	}

	firing, critical := counts(alerts)
	if firing == 0 {
		return fmt.Sprintf("Ichor · %s: %d resolved", label, len(alerts))
	}

	return fmt.Sprintf("Ichor · %s: %d firing (%d critical)", label, firing, critical)
}

// ntfyBody is one line per alert, "severity alertname (namespace)", cut to maxNtfyBody.
func ntfyBody(device Device, alerts []PushAlert) string {
	// reserve leaves room for the "… and N more" line.
	const reserve = 32

	var b strings.Builder
	for i, a := range alerts {
		line := ntfyLine(device, a)
		if b.Len()+len(line)+reserve > maxNtfyBody {
			fmt.Fprintf(&b, "… and %d more", len(alerts)-i)

			break
		}
		b.WriteString(line + "\n")
	}

	return strings.TrimSuffix(b.String(), "\n")
}

func ntfyLine(device Device, a PushAlert) string {
	line := a.Severity + " " + a.Alertname
	if a.Namespace != "" {
		line += " (" + a.Namespace + ")"
	}
	if a.Status == StatusResolved {
		line = "resolved: " + line
	}
	if device.Verbose && a.Summary != "" {
		line += ": " + a.Summary
	}

	return line
}

// counts returns the firing alerts and, among them, the critical ones.
func counts(alerts []PushAlert) (firing, critical int) {
	for _, a := range alerts {
		if a.Status != StatusFiring {
			continue
		}
		firing++
		if a.Severity == SeverityCritical {
			critical++
		}
	}

	return firing, critical
}

func hasFiringCritical(alerts []PushAlert) bool {
	_, critical := counts(alerts)

	return critical > 0
}
