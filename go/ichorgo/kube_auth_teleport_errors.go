package ichorgo

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// What the Teleport proxy's Kubernetes endpoint answers, in sentences the apps show as they
// are (lib/kube/proxy/forwarder.go): an HTTP refusal, or a certificate refused at the TLS
// handshake, which gets no HTTP answer at all.

const (
	teleportMFAMessage    = "MFA is required to access Kubernetes cluster"
	teleportALPNPrefix    = "kube-teleport-proxy-alpn."
	sentenceTeleportMFA   = "This cluster requires per-session MFA, which the phone cannot do yet. Ask your Teleport administrator, or use tsh proxy kube on a computer."
	sentenceTeleportRoles = "Your Teleport roles grant no access to this Kubernetes cluster."
	sentenceTeleportLB    = "The Teleport proxy did not answer as a Kubernetes endpoint (a load balancer in front of it may terminate TLS); use the proxy's own address."
)

var teleportClusterNotFound = regexp.MustCompile(`kubernetes cluster "([^"]*)" not found`)

// teleportCertRefusals are the alerts a server sends for a client certificate it does not
// take (RFC 8446 §6.2): bad, revoked, expired, unknown, required.
var teleportCertRefusals = []tls.AlertError{42, 44, 45, 46, 116}

// teleportCertRefusalWords are those alerts as text, for errors that keep only the message (a
// WebSocket dial error does not unwrap).
var teleportCertRefusalWords = []string{
	"tls: bad certificate", "tls: revoked certificate", "tls: expired certificate",
	"tls: unknown certificate", "tls: certificate required",
}

// teleportRefusal is a proxy refusal in its own sentence, still an API error underneath.
type teleportRefusal struct {
	sentence string
	err      error
}

func (e *teleportRefusal) Error() string { return e.sentence }
func (e *teleportRefusal) Unwrap() error { return e.err }

func (m *teleportMethod) refusal(err error, serverName string) error {
	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) {
		if sentence := teleportAPISentence(apiErr); sentence != "" {
			return &teleportRefusal{sentence: sentence, err: err}
		}

		return err
	}

	if teleportCertRefused(err) {
		return signInRequired(authTeleport, reasonTeleportSession)
	}

	var unknownCA x509.UnknownAuthorityError
	if errors.As(err, &unknownCA) && strings.HasPrefix(serverName, teleportALPNPrefix) {
		return &teleportRefusal{sentence: sentenceTeleportLB, err: err}
	}

	return err
}

// teleportAPISentence is the sentence of a refusal the proxy itself made, "" for one of the
// API server behind it (Kubernetes RBAC's "forbidden" stays as it is).
func teleportAPISentence(apiErr *kubeAPIError) string {
	msg := apiErr.Message

	switch {
	case apiErr.Code == http.StatusForbidden && strings.Contains(msg, teleportMFAMessage):
		return sentenceTeleportMFA
	case apiErr.Code == http.StatusForbidden &&
		(strings.Contains(msg, "[00] access denied") || strings.HasPrefix(msg, "access denied") || strings.Contains(msg, "no roles grant access")):
		return sentenceTeleportRoles
	}

	if found := teleportClusterNotFound.FindStringSubmatch(msg); found != nil {
		return "Teleport knows no Kubernetes cluster named " + found[1] + " for your roles."
	}

	return ""
}

// teleportCertRefused reports a client certificate the server refused at the handshake.
func teleportCertRefused(err error) bool {
	var alert tls.AlertError
	if errors.As(err, &alert) {
		for _, refused := range teleportCertRefusals {
			if alert == refused {
				return true
			}
		}
	}

	msg := err.Error()
	for _, words := range teleportCertRefusalWords {
		if strings.Contains(msg, words) {
			return true
		}
	}

	return false
}
