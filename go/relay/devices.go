package relay

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Field limits of a Device.
const (
	maxURLLen     = 2048
	maxLabelLen   = 100
	maxClusterLen = 128
	authSecretLen = 16
)

var (
	deviceIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	errTooManyDevices = fmt.Errorf("at most %d devices can be paired", MaxDevices)
)

// validateDevice checks a device sent by a phone. Push endpoints are https unless allowHTTP
// (an in-cluster ntfy over plain http): the pairing token must not turn the relay into a
// cleartext client of any address. The messages quote no value.
func validateDevice(d Device, allowHTTP bool) error {
	if !deviceIDPattern.MatchString(d.ID) {
		return errors.New("id: 1 to 64 characters among A-Z a-z 0-9 _ -")
	}

	schemes := []string{"https"}
	if allowHTTP {
		schemes = append(schemes, "http")
	}
	if err := validateURL(d.Endpoint, schemes...); err != nil {
		if !allowHTTP && strings.HasPrefix(d.Endpoint, "http:") {
			return errors.New("endpoint: plain http is refused (the relay runs with RELAY_ALLOW_HTTP off)")
		}

		return fmt.Errorf("endpoint: %w", err)
	}

	if d.Click != "" {
		if err := validateURL(d.Click, "ichor", "https"); err != nil {
			return fmt.Errorf("click: %w", err)
		}
	}

	switch {
	case len(d.Label) > maxLabelLen:
		return fmt.Errorf("label: at most %d bytes", maxLabelLen)
	case len(d.Cluster) > maxClusterLen:
		return fmt.Errorf("cluster: at most %d bytes", maxClusterLen)
	}

	switch d.Platform {
	case PlatformNtfy:
		return nil
	case PlatformUnifiedPush:
		return validateWebPushKeys(d.P256DH, d.Auth)
	default:
		return fmt.Errorf("platform: %q or %q", PlatformUnifiedPush, PlatformNtfy)
	}
}

// validateURL accepts an absolute URL of one of schemes, with a host for http(s).
func validateURL(raw string, schemes ...string) error {
	if len(raw) > maxURLLen {
		return fmt.Errorf("at most %d bytes", maxURLLen)
	}

	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return errors.New("not a URL")
	case !slices.Contains(schemes, u.Scheme):
		return fmt.Errorf("the scheme must be one of %s", strings.Join(schemes, ", "))
	case strings.HasPrefix(u.Scheme, "http") && u.Host == "":
		return errors.New("no host")
	}

	return nil
}

// validateWebPushKeys checks the subscription's keys: an uncompressed P-256 point and a
// 16-byte auth secret, base64 (URL-safe or not, padded or not).
func validateWebPushKeys(p256dh, auth string) error {
	pub, err := decodeKey(p256dh)
	if err != nil {
		return errors.New("p256dh: not base64")
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return errors.New("p256dh: not a P-256 public key")
	}

	secret, err := decodeKey(auth)
	if err != nil || len(secret) != authSecretLen {
		return fmt.Errorf("auth: %d bytes, base64", authSecretLen)
	}

	return nil
}

func decodeKey(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}

	return base64.RawStdEncoding.DecodeString(s)
}

// findDevice returns the device id of the state.
func findDevice(s State, id string) (Device, bool) {
	i := slices.IndexFunc(s.Devices, func(d Device) bool { return d.ID == id })
	if i < 0 {
		return Device{}, false
	}

	return s.Devices[i], true
}

// upsertDevice adds or replaces d, keeping the first pairing's CreatedAt.
func upsertDevice(s *State, d Device, now time.Time) error {
	i := slices.IndexFunc(s.Devices, func(x Device) bool { return x.ID == d.ID })
	if i >= 0 {
		d.CreatedAt = s.Devices[i].CreatedAt
		s.Devices[i] = d

		return nil
	}

	if len(s.Devices) >= MaxDevices {
		return errTooManyDevices
	}

	d.CreatedAt = now.UnixMilli()
	s.Devices = append(s.Devices, d)

	return nil
}

// deleteDevice removes the device id, only while its endpoint is endpoint when that is not
// empty (a re-pairing since the delivery keeps its new endpoint); errUnchanged when nothing
// matched.
func deleteDevice(s *State, id, endpoint string) error {
	n := len(s.Devices)
	s.Devices = slices.DeleteFunc(s.Devices, func(d Device) bool {
		return d.ID == id && (endpoint == "" || d.Endpoint == endpoint)
	})
	if len(s.Devices) == n {
		return errUnchanged
	}

	return nil
}
