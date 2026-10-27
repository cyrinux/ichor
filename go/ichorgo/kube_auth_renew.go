package ichorgo

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Renewing every sign-in ahead of need: the apps call KubeRenewSignIns when they come to the
// front and before each background check. A refresh keeps a session alive at identity
// providers with an idle timeout (Keycloak's "SSO Session Idle" is bumped by every refresh
// request), and a session that really ended is found before the user needs the cluster.
// Nothing here extends an absolute session (IAM Identity Center, Google Workspace session
// control): those are reported when they end.

// renewParallel is how many clusters renew at once: a store of 30 clusters must not stall the
// background run, nor open 30 token requests at once.
const renewParallel = 4

// Renewal statuses.
const (
	renewValid          = "valid"
	renewRenewed        = "renewed"
	renewSignInRequired = "signInRequired"
	renewError          = "error"
)

// signInRenewal is the outcome of one cluster's renewal.
type signInRenewal struct {
	Context string `json:"context"`
	// Kind is "kube" or "talos" (an Omni context): a name can be both.
	Kind   string `json:"kind"`
	Method string `json:"method"`
	Status string `json:"status"`
	// Expires is the current token's end, SessionExpires when a new sign-in will be needed
	// (0 unknown). Unix seconds.
	Expires        int64  `json:"expires,omitempty"`
	SessionExpires int64  `json:"sessionExpires,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// renewTarget is one signed-in context to renew.
type renewTarget struct {
	name string
	sc   kubeSignInContext
}

// KubeRenewSignIns renews the token of every signed-in context of storedYAML (the stored
// kubeconfig) whose method has one to renew, renewParallel at a time, and reports the Omni
// contexts of talosYAML (their user keys cannot be renewed). Returns a JSON array of
// signInRenewal; contexts that were never signed in are left out, so an empty or unreadable
// auth store gives [].
func KubeRenewSignIns(storedYAML, talosYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	privacy.learnConfig(storedYAML)
	privacy.learnConfig(talosYAML)

	targets := renewTargets(storedYAML)
	results := make([]signInRenewal, len(targets))

	forEachLimit(targets, renewParallel, func(i int, t renewTarget) {
		results[i] = renewOne(t)
	})

	report := make([]signInRenewal, 0, len(results))

	for _, r := range results {
		if r.Context != "" {
			report = append(report, r)
		}
	}

	return toJSON(append(report, omniRenewals(talosYAML)...))
}

// renewTargets is every context of storedYAML that signs in through a method and has a usable
// sign-in stored (what KubeSignInInfo calls signedIn).
func renewTargets(storedYAML string) []renewTarget {
	if strings.TrimSpace(storedYAML) == "" {
		return nil
	}

	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return nil
	}

	var out []renewTarget

	for _, c := range doc.Contexts {
		sc, err := signInContext(storedYAML, c.Name)
		if err != nil || sc.method == nil {
			continue
		}

		state := kubeAuth.load(sc.key)
		if state.Method != sc.method.name() || (len(state.Session) == 0 && len(state.Secrets) == 0) {
			continue
		}

		out = append(out, renewTarget{name: c.Name, sc: sc})
	}

	return out
}

// renewOne renews one context through its token source, the path every call takes, so a
// rotating refresh token is never used twice by a foreground call and this one.
func renewOne(t renewTarget) signInRenewal {
	before := kubeAuth.load(t.sc.key)
	out := signInRenewal{Context: t.name, Kind: kindKube, Method: t.sc.method.name(), Expires: before.Expires, SessionExpires: before.SessionExpires}

	// Keys the user entered, with no session: nothing that can end, nothing to renew.
	if _, ok := t.sc.method.(credentialMethod); ok && len(before.Session) == 0 {
		out.Status = renewValid

		return out
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	_, err := kubeAuth.source(t.sc.key, t.sc.method).get(ctx)
	after := kubeAuth.load(t.sc.key)
	out.Expires, out.SessionExpires = after.Expires, after.SessionExpires

	var required *errSignInRequired

	switch {
	case errors.As(err, &required):
		out.Status, out.Reason = renewSignInRequired, required.reason
	case err != nil:
		// The phone is offline or the provider is down: the session may well be fine.
		out.Status, out.Reason = renewError, err.Error()
	case after.Expires != before.Expires:
		out.Status = renewRenewed
	default:
		out.Status = renewValid
	}

	return out
}

// omniRenewals reports the signed-in Omni contexts of talosYAML: a user key lives the hours
// Omni gives it and cannot be renewed, a service account key does not end.
func omniRenewals(talosYAML string) []signInRenewal {
	if strings.TrimSpace(talosYAML) == "" {
		return nil
	}

	cfg, err := configs.get(talosYAML).parsed()
	if err != nil {
		return nil
	}

	var out []signInRenewal

	for _, name := range sortedContextNames(cfg) {
		sc, err := loadOmniSignInContext(talosYAML, name)
		if err != nil || sc.key == "" {
			continue
		}

		method := omniMethodName(sc.ctx)

		state := kubeAuth.load(sc.key)
		if state.Method != method {
			continue
		}

		r := signInRenewal{Context: name, Kind: kindTalos, Method: method, Status: renewValid, SessionExpires: state.SessionExpires}

		if _, err := loadOmniSigner(sc.key, sc.ctx); err != nil {
			r.Status, r.Reason = renewSignInRequired, err.Error()
		} else if state.SessionExpires > 0 && time.Now().Unix() >= state.SessionExpires {
			r.Status, r.Reason = renewSignInRequired, "the Omni key expired"
		}

		out = append(out, r)
	}

	return out
}
