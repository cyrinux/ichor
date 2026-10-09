package ichorgo

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Forced renewal of a cert-manager certificate, the way `cmctl renew` does it: an Issuing
// condition set to True on the certificate's status makes cert-manager issue it again now,
// whatever its renewal time. The status is replaced with a merge patch carrying the
// resourceVersion read just before, so a change cert-manager made in between makes it fail
// instead of being overwritten.

const (
	certConditionIssuing = "Issuing"
	certRenewReason      = "ManuallyTriggered"
	certRenewMessage     = "Certificate re-issuance manually triggered"
)

var (
	errCertManagerMissing = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "cert-manager is not installed"}
	errCertIssuing        = &kubeAPIError{Code: 409, Reason: "Issuing", Message: "the certificate is already being issued"}
)

// certificateRef is what a renewal reads; the conditions stay raw so the fields this file does
// not know about survive the patch.
type certificateRef struct {
	Metadata struct {
		ResourceVersion string `json:"resourceVersion"`
		Generation      int64  `json:"generation"`
	} `json:"metadata"`
	Status struct {
		Conditions []map[string]any `json:"conditions"`
	} `json:"status"`
}

// KubeCertManagerRenew issues the cert-manager certificate namespace/name again now (os:admin),
// like `cmctl renew`. Refused while it is already being issued. kubeServer: see KubePods.
func KubeCertManagerRenew(configYAML, contextName, kubeServer, namespace, name string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "cert-renew", Namespace: namespace, Object: "Certificate/" + name})

	if err := validateKubeName("certificate", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return renewCertificate(ctx, k, namespace, name, time.Now())
	})
}

func renewCertificate(ctx context.Context, k *kubeClient, namespace, name string, now time.Time) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	version, ok := groups[groupCertManager]
	if !ok {
		return errCertManagerMissing
	}

	path := "/apis/" + groupCertManager + "/" + version + "/namespaces/" + url.PathEscape(namespace) +
		"/certificates/" + url.PathEscape(name)

	var cert certificateRef
	if err := k.get(ctx, path, &cert); err != nil {
		return err
	}

	issuing := map[string]any{
		"type":               certConditionIssuing,
		"status":             "True",
		"reason":             certRenewReason,
		"message":            certRenewMessage,
		"lastTransitionTime": now.UTC().Format(time.RFC3339),
		"observedGeneration": cert.Metadata.Generation,
	}

	conds := slices.Clone(cert.Status.Conditions)

	i := slices.IndexFunc(conds, func(c map[string]any) bool { return c["type"] == certConditionIssuing })
	switch {
	case i < 0:
		conds = append(conds, issuing)
	case conds[i]["status"] == "True":
		return errCertIssuing
	default:
		conds[i] = issuing
	}

	// A merge patch replaces a list whole: every condition goes, the new one among them.
	return k.patch(ctx, path+"/status", "application/merge-patch+json", map[string]any{
		"metadata": map[string]any{"resourceVersion": cert.Metadata.ResourceVersion},
		"status":   map[string]any{"conditions": conds},
	}, nil)
}
