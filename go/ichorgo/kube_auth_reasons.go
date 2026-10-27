package ichorgo

import (
	"strconv"
	"strings"
	"time"
)

// Why a cluster's sign-in ended, in one sentence: what ended it and who sets its length, so
// a session an administrator limits is not taken for an app bug. The apps show the reason
// after the sign-in-required code (kube_auth.go), on one line.

const (
	// gkeOwnClient is the GKE sign-in with the organisation's own OAuth client
	// (kube_auth_gke_oauth.go): Google ends its sign-ins after 7 days while its consent screen
	// is in Testing.
	gkeOwnClient = "gke-oauth-client"
	// gkeWorkforce is a workforce identity session (login config): the workforce pool sets its
	// length.
	gkeWorkforce = "gke-workforce"

	// googleTestingDays is how long Google keeps a sign-in of an OAuth client in Testing (7
	// days), plus a day so a sign-in that just passed it still reads as that.
	googleTestingDays = 8
)

const (
	reasonEKSSession      = "Your IAM Identity Center session ended. Its length (8 hours by default, up to 90 days) is set by your AWS administrator."
	reasonEKSRegistration = "The app's registration with IAM Identity Center expired; sign in again."
	reasonGoogleSession   = "Your organisation's Google Cloud session control asks you to sign in again (1 to 24 hours, set by your Workspace administrator)."
	reasonGoogleTesting   = "Google ended this sign-in. An OAuth client whose consent screen is in Testing gets 7-day sign-ins: publish the app in Google Cloud Console."
	reasonGoogleRefused   = "Google no longer accepts this sign-in (revoked, unused for six months, or too many sign-ins with this account)."
	reasonWorkforce       = "Your workforce identity session ended. Its length (1 hour by default, 15 minutes to 12 hours) is set by your Google Cloud administrator."
	reasonOIDC            = "Your identity provider ended this session (its idle or maximum session time is set by its administrator)."
	reasonEntraUnused     = "Microsoft Entra ended this sign-in: it was not used for 90 days."
	reasonEntraPassword   = "Microsoft Entra ended this sign-in: the account's password changed or its sessions were revoked."
	reasonEntra           = "Microsoft Entra ended this sign-in (its session policy is set by your administrator)."
)

// sessionEndedReason is why the provider refused to renew a sign-in: source is the method
// (authEKS, authGKE, authOIDC, authAzure) or a GKE variant (gkeOwnClient, gkeWorkforce),
// code and subtype the OAuth error and error_subtype, description its error_description.
// "Signed in … ago." follows when signedInAt (unix seconds) is known.
func sessionEndedReason(source, code, subtype, description string, signedInAt int64) string {
	return joinSentences(endedWhy(source, code, subtype, oneLine(description), signedInAt), signedInAgo(signedInAt, time.Now()))
}

func endedWhy(source, code, subtype, description string, signedInAt int64) string {
	switch source {
	case authEKS:
		switch code {
		case "expired_token", "invalid_grant":
			return reasonEKSSession
		case "invalid_client":
			return reasonEKSRegistration
		}
	case authGKE, gkeOwnClient:
		// Only a refusal reaches here (invalid_grant, or a 401/403 without a code).
		switch {
		case subtype == "invalid_rapt" || code == "invalid_rapt":
			return reasonGoogleSession
		case source == gkeOwnClient && signedInAt > 0 && time.Since(time.Unix(signedInAt, 0)) < googleTestingDays*24*time.Hour:
			return reasonGoogleTesting
		default:
			return reasonGoogleRefused
		}
	case gkeWorkforce:
		if subtype == "invalid_rapt" || code == "invalid_rapt" {
			return reasonGoogleSession
		}

		return reasonWorkforce
	case authAzure:
		if code == "invalid_grant" {
			return joinSentences(entraWhy(description), sentence(description))
		}
	case authOIDC:
		// The provider's own words stay: Keycloak's tell an idle timeout ("Token is not
		// active") from the session's end ("Session not active").
		if code == "invalid_grant" {
			return joinSentences(reasonOIDC, sentence(description))
		}
	}

	return cmpOr(description, code)
}

// entraWhy reads the AADSTS code Entra puts at the start of its error_description.
func entraWhy(description string) string {
	switch {
	case strings.HasPrefix(description, "AADSTS700082"):
		return reasonEntraUnused
	case strings.HasPrefix(description, "AADSTS50173"):
		return reasonEntraPassword
	default:
		return reasonEntra
	}
}

// signedInAgo is "Signed in 3 hours ago.", rounded to hours under two days, else to days;
// "" when the sign-in time is unknown.
func signedInAgo(signedInAt int64, now time.Time) string {
	if signedInAt <= 0 {
		return ""
	}

	since := now.Sub(time.Unix(signedInAt, 0))

	switch {
	case since < time.Hour:
		return "Signed in less than an hour ago."
	case since < 48*time.Hour:
		return "Signed in " + plural(int(since.Round(time.Hour)/time.Hour), "hour") + " ago."
	default:
		return "Signed in " + plural(int(since.Round(24*time.Hour)/(24*time.Hour)), "day") + " ago."
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}

	return strconv.Itoa(n) + " " + unit + "s"
}

// oneLine keeps a provider's description on one line: Entra's carries trace and
// correlation IDs on the next ones, and the apps read the reason up to the line's end.
func oneLine(s string) string {
	line, _, _ := strings.Cut(strings.ReplaceAll(s, "\r", "\n"), "\n")

	return strings.TrimSpace(line)
}

// sentence ends s with a full stop unless it ends with punctuation already.
func sentence(s string) string {
	if s == "" || strings.ContainsAny(s[len(s)-1:], ".!?") {
		return s
	}

	return s + "."
}

// joinSentences joins the non-empty parts with a space.
func joinSentences(parts ...string) string {
	kept := make([]string, 0, len(parts))

	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}

	return strings.Join(kept, " ")
}
