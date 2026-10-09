package ichorgo

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Where a Prometheus-compatible query API (Prometheus, Mimir, Thanos, VictoriaMetrics) is
// reached: through the API server's service proxy with the admin kubeconfig, or at a URL
// the phone reaches itself.
const (
	promModeProxy = "proxy"
	promModeURL   = "url"

	promAuthNone   = ""
	promAuthBearer = "bearer"
	promAuthBasic  = "basic"
)

// promSource is what the app stores per cluster; Secret is kept apart by the app (Keystore,
// Keychain) and only joined in for a query.
type promSource struct {
	Mode string `json:"mode"`
	// Kind is a display hint: prometheus, mimir, thanos, victoriametrics, alertmanager.
	Kind string `json:"kind,omitempty"`

	// Proxy mode: the Service and the HTTP port of the query API.
	Namespace string `json:"namespace,omitempty"`
	Service   string `json:"service,omitempty"`
	Port      int    `json:"port,omitempty"`

	// PathPrefix comes before /api/v1 (Mimir: /prometheus), in proxy mode only: a URL
	// carries its own.
	PathPrefix string `json:"pathPrefix,omitempty"`

	// URL mode.
	URL      string `json:"url,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
	// CA is an extra PEM certificate authority to trust, for a self-hosted server.
	CA                 string `json:"ca,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`

	// Tenant is sent as X-Scope-OrgID (Mimir, Cortex, Loki-style multi-tenancy), both modes.
	Tenant string `json:"tenant,omitempty"`
}

const maxPromCA = 64 << 10

var (
	// A Kubernetes namespace or Service name (RFC 1123 label).
	promDNSLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	// Path segments only: no query, no "..", nothing to escape.
	promPathPrefix = regexp.MustCompile(`^(/[A-Za-z0-9_~-][A-Za-z0-9._~-]*)*$`)
	// Mimir's tenant ID characters, "|" joining tenants for federated queries.
	promTenant = regexp.MustCompile(`^[A-Za-z0-9!_.*'()|-]{1,150}$`)
)

// parsePromSource decodes and checks a source as the app sends it, returning it cleaned up.
func parsePromSource(sourceJSON string) (promSource, error) {
	var src promSource

	if err := json.Unmarshal([]byte(sourceJSON), &src); err != nil {
		return promSource{}, errors.New("bad metrics source")
	}

	return src.normalize()
}

var promKinds = []string{"", "prometheus", "mimir", "thanos", "victoriametrics", "alertmanager"}

func (src promSource) normalize() (promSource, error) {
	if !slices.Contains(promKinds, src.Kind) {
		src.Kind = ""
	}

	src.Tenant = strings.TrimSpace(src.Tenant)
	if src.Tenant != "" && (!promTenant.MatchString(src.Tenant) || src.Tenant == "." || src.Tenant == "..") {
		return promSource{}, errors.New("the tenant (X-Scope-OrgID) may only use letters, digits and !-_.*'()|")
	}

	switch src.Mode {
	case promModeProxy, "":
		// An encoder leaving out default values sends no mode: the proxy is the default.
		src.Mode = promModeProxy

		return src.normalizeProxy()
	case promModeURL:
		return src.normalizeURL()
	default:
		return promSource{}, fmt.Errorf("unknown metrics source mode %q", src.Mode)
	}
}

func (src promSource) normalizeProxy() (promSource, error) {
	src.Namespace, src.Service = strings.TrimSpace(src.Namespace), strings.TrimSpace(src.Service)
	src.PathPrefix = strings.TrimRight(strings.TrimSpace(src.PathPrefix), "/")

	switch {
	case !promDNSLabel.MatchString(src.Namespace):
		return promSource{}, errors.New("the namespace is not a valid Kubernetes name")
	case !promDNSLabel.MatchString(src.Service):
		return promSource{}, errors.New("the Service is not a valid Kubernetes name")
	case src.Port < 1 || src.Port > 65535:
		return promSource{}, errors.New("the port must be between 1 and 65535")
	case !promPathPrefix.MatchString(src.PathPrefix):
		return promSource{}, errors.New("the path prefix must look like /prometheus")
	case src.Auth != promAuthNone || src.Secret != "" || src.Username != "":
		// The API server authenticates the proxy request with the kubeconfig's credentials.
		return promSource{}, errors.New("credentials are only for a URL: the Kubernetes API authenticates the service proxy")
	}

	src.URL, src.CA, src.InsecureSkipVerify = "", "", false

	return src, nil
}

func (src promSource) normalizeURL() (promSource, error) {
	u, err := url.Parse(strings.TrimSpace(src.URL))

	switch {
	case err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https":
		return promSource{}, errors.New("the URL must look like https://host/path")
	case u.User != nil:
		return promSource{}, errors.New("put the user name and password in the authentication fields, not the URL")
	case u.RawQuery != "" || u.Fragment != "":
		return promSource{}, errors.New("the URL takes no query or fragment")
	}

	src.Username, src.Secret = strings.TrimSpace(src.Username), strings.TrimSpace(src.Secret)

	switch src.Auth {
	case promAuthNone:
		src.Username, src.Secret = "", ""
	case promAuthBearer:
		src.Username = ""
		if src.Secret == "" {
			return promSource{}, errors.New("no token set")
		}
	case promAuthBasic:
		if src.Username == "" || strings.Contains(src.Username, ":") {
			return promSource{}, errors.New("the user name is empty or has a colon")
		}
	default:
		return promSource{}, fmt.Errorf("unknown authentication %q", src.Auth)
	}

	if strings.ContainsAny(src.Secret, "\r\n") {
		return promSource{}, errors.New("the token or password spans several lines")
	}

	if u.Scheme == "http" {
		// Credentials never travel in clear.
		if src.Auth != promAuthNone {
			return promSource{}, errors.New("credentials are only sent over https: use an https URL")
		}

		src.CA, src.InsecureSkipVerify = "", false
	}

	src.CA = strings.TrimSpace(src.CA)
	if src.CA != "" {
		if len(src.CA) > maxPromCA || !x509.NewCertPool().AppendCertsFromPEM([]byte(src.CA)) {
			return promSource{}, errors.New("the certificate authority is not a PEM certificate")
		}
	}

	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	src.URL = u.String()
	src.Namespace, src.Service, src.Port, src.PathPrefix = "", "", 0, ""

	return src, nil
}

// label names the source in messages.
func (src promSource) label() string {
	if src.Mode == promModeProxy {
		return src.Namespace + "/" + src.Service
	}

	return src.URL
}
