package ichorgo

import (
	"cmp"
	"context"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// groupCertManager is cert-manager's API group (certificates, issuers, clusterissuers).
const groupCertManager = "cert-manager.io"

// How close to its expiry a certificate is critical (when not ready) or a warning, how late a
// renewal may be before it is overdue, and how many DNS names an item carries.
const (
	certCriticalWithin = 7 * 24 * time.Hour
	certWarningWithin  = 14 * 24 * time.Hour
	certRenewalGrace   = time.Hour
	certMaxDNSNames    = 5
)

type certManagerStatus struct {
	Version      string            `json:"version"`
	Error        string            `json:"error"`
	Certificates []certManagerCert `json:"certificates"`
	Issuers      []certIssuer      `json:"issuers"`
}

type certManagerCert struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	SecretName string `json:"secretName"`
	// DNSNames is the common name then the DNS names, the first few; DNSNameCount counts them all.
	DNSNames     []string `json:"dnsNames"`
	DNSNameCount int      `json:"dnsNameCount"`
	Issuer       string   `json:"issuer"`  // "ClusterIssuer/letsencrypt", "Issuer/internal-ca"
	Health       string   `json:"health"`  // critical|warning|ok
	Reasons      []string `json:"reasons"` // see certReason*
	Ready        bool     `json:"ready"`
	// Issuing: cert-manager is issuing it now (a renewal, or one forced from the app).
	Issuing bool `json:"issuing"`
	// Message is the Ready condition's message when the certificate is not ready.
	Message        string `json:"message"`
	NotAfter       int64  `json:"notAfter"`    // unix ms, 0 before the first issuance
	RenewalTime    int64  `json:"renewalTime"` // unix ms, 0 when none is planned
	FailedAttempts int    `json:"failedAttempts"`
}

type certIssuer struct {
	Kind      string `json:"kind"` // Issuer|ClusterIssuer
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Type      string `json:"type"`   // acme|ca|selfSigned|vault|venafi, "" when another
	Server    string `json:"server"` // the ACME server's host, "" otherwise
	Ready     bool   `json:"ready"`
	Message   string `json:"message"` // the Ready condition's message
	Health    string `json:"health"`  // warning|ok
}

// Reasons a certificate is not ok, for the app to word.
const (
	certReasonExpired        = "expired"        // notAfter is past
	certReasonExpiring       = "expiring"       // expires within 14 days with no renewal planned
	certReasonRenewalOverdue = "renewalOverdue" // its renewal time passed over an hour ago
	certReasonNotReady       = "notReady"       // the Ready condition is not True
	certReasonIssuer         = "issuer"         // its issuer is not ready
)

type certificateObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		SecretName string   `json:"secretName"`
		CommonName string   `json:"commonName"`
		DNSNames   []string `json:"dnsNames"`
		IssuerRef  struct {
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Group string `json:"group"`
		} `json:"issuerRef"`
	} `json:"spec"`
	Status struct {
		Conditions             []kubeCondition `json:"conditions"`
		NotAfter               string          `json:"notAfter"`
		RenewalTime            string          `json:"renewalTime"`
		FailedIssuanceAttempts int             `json:"failedIssuanceAttempts"`
	} `json:"status"`
}

type issuerObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		ACME *struct {
			Server string `json:"server"`
		} `json:"acme"`
		CA         *struct{} `json:"ca"`
		SelfSigned *struct{} `json:"selfSigned"`
		Vault      *struct{} `json:"vault"`
		Venafi     *struct{} `json:"venafi"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// readCertManager lists the certificates, the issuers and the cluster issuers.
func readCertManager(ctx context.Context, k *kubeClient, version string, now time.Time) *certManagerStatus {
	var (
		certs                   kubeList[certificateObject]
		issuers, clusterIssuers kubeList[issuerObject]
		errs                    = make([]error, 3)
		wg                      sync.WaitGroup
	)

	base := "/apis/" + groupCertManager + "/" + version + "/"

	wg.Go(func() { errs[0] = getList(ctx, k, base+"certificates", &certs) })
	wg.Go(func() { errs[1] = getList(ctx, k, base+"issuers", &issuers) })
	wg.Go(func() { errs[2] = getList(ctx, k, base+"clusterissuers", &clusterIssuers) })
	wg.Wait()

	out := mapCertManager(certs.Items, issuers.Items, clusterIssuers.Items, now)
	out.Version = version
	out.Error = sectionError(errs...)

	return out
}

func mapCertManager(certs []certificateObject, issuers, clusterIssuers []issuerObject, now time.Time) *certManagerStatus {
	out := &certManagerStatus{Certificates: []certManagerCert{}, Issuers: []certIssuer{}}

	for _, obj := range issuers {
		out.Issuers = append(out.Issuers, mapCertIssuer("Issuer", obj))
	}

	for _, obj := range clusterIssuers {
		out.Issuers = append(out.Issuers, mapCertIssuer("ClusterIssuer", obj))
	}

	// Issuers by "Kind/namespace/name" (no namespace for a ClusterIssuer).
	byRef := map[string]certIssuer{}
	for _, iss := range out.Issuers {
		byRef[iss.Kind+"/"+iss.Namespace+"/"+iss.Name] = iss
	}

	for _, obj := range certs {
		out.Certificates = append(out.Certificates, mapCertificate(obj, byRef, now))
	}

	slices.SortFunc(out.Certificates, func(a, b certManagerCert) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		// The soonest expiry first; a certificate never issued last.
		expiry := func(c certManagerCert) int64 {
			if c.NotAfter == 0 {
				return math.MaxInt64
			}

			return c.NotAfter
		}

		if d := cmp.Compare(expiry(a), expiry(b)); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	slices.SortFunc(out.Issuers, func(a, b certIssuer) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Kind+"/"+a.Namespace+"/"+a.Name, b.Kind+"/"+b.Namespace+"/"+b.Name)
	})

	return out
}

func mapCertIssuer(kind string, obj issuerObject) certIssuer {
	iss := certIssuer{Kind: kind, Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Health: healthOK}

	spec := obj.Spec

	switch {
	case spec.ACME != nil:
		iss.Type = "acme"
		if u, err := url.Parse(spec.ACME.Server); err == nil {
			iss.Server = u.Hostname()
		}
	case spec.CA != nil:
		iss.Type = "ca"
	case spec.SelfSigned != nil:
		iss.Type = "selfSigned"
	case spec.Vault != nil:
		iss.Type = "vault"
	case spec.Venafi != nil:
		iss.Type = "venafi"
	}

	ready := readyCondition(obj.Status.Conditions)
	iss.Ready = ready.Status == "True"
	iss.Message = ready.Message

	if !iss.Ready {
		iss.Health = healthWarning
	}

	return iss
}

// readyCondition is the Ready condition, empty when absent.
func readyCondition(conds []kubeCondition) kubeCondition {
	for _, c := range conds {
		if c.Type == "Ready" {
			return c
		}
	}

	return kubeCondition{}
}

func mapCertificate(obj certificateObject, issuers map[string]certIssuer, now time.Time) certManagerCert {
	st := obj.Status
	cert := certManagerCert{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, SecretName: obj.Spec.SecretName,
		NotAfter: unixMilli(st.NotAfter), RenewalTime: unixMilli(st.RenewalTime), FailedAttempts: st.FailedIssuanceAttempts,
	}

	names := []string{}
	if cn := obj.Spec.CommonName; cn != "" && !slices.Contains(obj.Spec.DNSNames, cn) {
		names = append(names, cn)
	}

	names = append(names, obj.Spec.DNSNames...)
	cert.DNSNameCount = len(names)
	cert.DNSNames = names[:min(len(names), certMaxDNSNames)]

	ref := obj.Spec.IssuerRef

	kind := ref.Kind
	if kind == "" {
		kind = "Issuer"
	}

	cert.Issuer = kind + "/" + ref.Name

	ready := readyCondition(st.Conditions)
	cert.Ready = ready.Status == "True"

	if !cert.Ready {
		cert.Message = ready.Message
	}

	for _, c := range st.Conditions {
		if c.Type == certConditionIssuing && c.Status == "True" {
			cert.Issuing = true
		}
	}

	// An external issuer (another API group) has no Ready condition of cert-manager's to read.
	issuerReady := true

	if ref.Group == "" || ref.Group == groupCertManager {
		ns := obj.Metadata.Namespace
		if kind == "ClusterIssuer" {
			ns = ""
		}

		if iss, ok := issuers[kind+"/"+ns+"/"+ref.Name]; ok {
			issuerReady = iss.Ready
		}
	}

	cert.Health, cert.Reasons = certHealth(cert, issuerReady, now)

	return cert
}

// certHealth: expired, or not ready and expiring within a week, is critical. Not ready, a renewal
// overdue, an expiry within two weeks or an issuer not ready is a warning. A certificate whose
// renewal is still planned (short-lived ones, renewed hours ahead) is not expiring.
func certHealth(cert certManagerCert, issuerReady bool, now time.Time) (string, []string) {
	reasons := []string{}
	critical := false

	nowMs := now.UnixMilli()
	left := time.Duration(cert.NotAfter-nowMs) * time.Millisecond
	renewalPlanned := cert.RenewalTime > nowMs
	overdue := cert.RenewalTime > 0 && time.Duration(nowMs-cert.RenewalTime)*time.Millisecond > certRenewalGrace

	if cert.NotAfter > 0 && left <= 0 {
		reasons, critical = append(reasons, certReasonExpired), true
	} else {
		if !cert.Ready {
			reasons = append(reasons, certReasonNotReady)
		}

		if cert.NotAfter > 0 && left < certWarningWithin && !renewalPlanned {
			reasons = append(reasons, certReasonExpiring)
			critical = !cert.Ready && left < certCriticalWithin
		}

		if overdue {
			reasons = append(reasons, certReasonRenewalOverdue)
		}
	}

	if !issuerReady {
		reasons = append(reasons, certReasonIssuer)
	}

	switch {
	case critical:
		return healthCritical, reasons
	case len(reasons) > 0:
		return healthWarning, reasons
	default:
		return healthOK, reasons
	}
}
