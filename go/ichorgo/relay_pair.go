package ichorgo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/cyrinux/ichor/go/relay"
)

// ichor-relay pairing (CYR-203): the phone finds the relay among the cluster's Services, reads
// its pairing token from the relay's Secret with its own credentials (RBAC decides who may
// pair) and registers its push endpoint, all through the API server's service proxy. The
// token never leaves these calls.

const (
	relayKind          = "ichor-relay"
	relaySecretName    = "ichor-relay"
	relayPairingKey    = "pairingToken"
	relayPairingHeader = "X-Ichor-Pairing"
	relayMaxEndpoint   = 2048
	relayAuthLen       = 16
	relayP256DHLen     = 65
)

var (
	relayDeviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	errRelayToken        = errors.New("the relay refused the pairing token: redeploy the relay or check RBAC")
)

// relayDiscovery is RelayDiscover's answer: the relays found, the likeliest first, and
// whether the credentials may pair with the first one.
type relayDiscovery struct {
	Sources []promSource `json:"sources"`
	CanPair bool         `json:"canPair"`
	Reason  string       `json:"reason"`
}

// RelayDiscover looks for ichor-relay among the cluster's Services (named ichor-relay or
// labelled app.kubernetes.io/name=ichor-relay):
// {"sources":[{mode:"proxy",kind:"ichor-relay",namespace,service,port}],canPair,reason}, in
// the metrics source shape the other relay calls take. canPair says whether the credentials
// may read the first relay's Secret, which pairing needs; reason says why not.
func RelayDiscover(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoRelayDiscovery, discoverRelay)
}

func discoverRelay(ctx context.Context, k *kubeClient) (relayDiscovery, error) {
	var list kubeList[promService]

	if err := getList(ctx, k, "/api/v1/services", &list); err != nil {
		return relayDiscovery{}, err
	}

	d := relayDiscovery{Sources: rankSources(list.Items, relayMatch)}
	if len(d.Sources) == 0 {
		d.Reason = "no ichor-relay Service in the cluster: apply deploy/relay first"

		return d, nil
	}

	ns := d.Sources[0].Namespace
	access, err := canDo(ctx, k, ssarAttributes{Namespace: ns, Verb: "get", Resource: "secrets", Name: relaySecretName})

	switch {
	case err != nil:
		// The API server cannot say: let the pairing itself tell.
		d.CanPair = true
	case access.Allowed:
		d.CanPair = true
	default:
		d.Reason = fmt.Sprintf("pairing needs get on the Secret %s/%s", ns, relaySecretName)
		if access.Reason != "" {
			d.Reason += " (" + access.Reason + ")"
		}
	}

	return d, nil
}

// relayMatch recognises the relay's Service and its HTTP port (8080, 80 or named http, else
// its only port).
func relayMatch(s promService) (promCandidate, bool) {
	var score int

	switch {
	case s.Metadata.Name == relayKind:
		score = 100
	case s.Metadata.Labels["app.kubernetes.io/name"] == relayKind:
		score = 90
	default:
		return promCandidate{}, false
	}

	port, ok := matchingServicePort(s, []int{8080, 80}, []string{"http"})
	if !ok && len(s.Spec.Ports) == 1 {
		port, ok = s.Spec.Ports[0].Port, true
	}

	if !ok {
		return promCandidate{}, false
	}

	src := promSource{Mode: promModeProxy, Kind: relayKind, Namespace: s.Metadata.Namespace, Service: s.Metadata.Name, Port: port}

	return promCandidate{src: src, score: score}, true
}

func demoRelayDiscovery() relayDiscovery {
	return relayDiscovery{
		Sources: []promSource{{Mode: promModeProxy, Kind: relayKind, Namespace: "ichor-relay", Service: "ichor-relay", Port: 8080}},
		Reason:  "demo",
	}
}

// RelayPairing reads what the phone needs before it subscribes to its push distributor from
// the relay at sourceJSON (see RelayDiscover): {"vapidPublicKey","relayVersion"}. Refused in
// the demo.
func RelayPairing(configYAML, contextName, kubeServer, sourceJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	src, err := parseRelaySource(sourceJSON)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	body, err := relayCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodGet, "/v1/pairing", nil)
	if err != nil {
		return "", err
	}

	var info struct {
		VAPIDPublicKey string `json:"vapidPublicKey"`
		RelayVersion   string `json:"relayVersion"`
	}

	if json.Unmarshal(body, &info) != nil || info.VAPIDPublicKey == "" {
		return "", fmt.Errorf("%s: no VAPID public key in the answer: is this ichor-relay?", src.label())
	}

	return toJSON(info)
}

// RelayRegister pairs this phone with the relay at sourceJSON: deviceJSON is a relay.Device
// without id, cluster or click ({platform:"unifiedpush"|"ntfy",endpoint,p256dh,auth,label,
// verbose}). The id is derived from the cluster and the endpoint, so pairing again replaces
// the same device; a ntfy device opens the cluster's Alerts screen when tapped. Returns
// {"id"}. Refused in the demo.
func RelayRegister(configYAML, contextName, kubeServer, sourceJSON, deviceJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	src, srcErr := parseRelaySource(sourceJSON)

	var device relay.Device
	deviceErr := json.Unmarshal([]byte(deviceJSON), &device)

	defer recordAction(&err, configYAML, contextName, relayAuditAction(kubeServer, "relay-pair", src, "platform="+device.Platform))

	switch {
	case srcErr != nil:
		return "", srcErr
	case deviceErr != nil:
		return "", errors.New("invalid device")
	case isDemoContext(configYAML, contextName):
		return "", errDemoUnavailable
	}

	device, err = relayDevice(configYAML, contextName, device)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(device)
	if err != nil {
		return "", fmt.Errorf("encode device: %w", err)
	}

	if _, err := relayCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodPut, "/v1/devices", body); err != nil {
		return "", err
	}

	return toJSON(map[string]string{"id": device.ID})
}

// RelayUnregister removes the device id (see RelayRegister) from the relay at sourceJSON.
// Refused in the demo.
func RelayUnregister(configYAML, contextName, kubeServer, sourceJSON, id string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	src, srcErr := parseRelaySource(sourceJSON)
	id = strings.TrimSpace(id)

	defer recordAction(&err, configYAML, contextName, relayAuditAction(kubeServer, "relay-unpair", src, "device="+id))

	switch {
	case srcErr != nil:
		return srcErr
	case !relayDeviceIDPattern.MatchString(id):
		return errors.New("not a device id")
	case isDemoContext(configYAML, contextName):
		return errDemoUnavailable
	}

	_, err = relayCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodDelete, "/v1/devices/"+url.PathEscape(id), nil)

	return err
}

// relayDevice completes and checks a device the app sent: the id, the cluster's fingerprint
// (sent back in every payload) and, for ntfy, the link to the cluster's Alerts screen.
func relayDevice(configYAML, contextName string, d relay.Device) (relay.Device, error) {
	fingerprint, clusterID := contextIdentity(configYAML, contextName)
	if fingerprint == "" || clusterID == "" {
		return relay.Device{}, errors.New("this cluster cannot be identified: open it again from the cluster list")
	}

	if err := checkRelayEndpoint(d); err != nil {
		return relay.Device{}, err
	}

	d.ID, d.Cluster, d.Click, d.CreatedAt = relayDeviceID(fingerprint, d.Endpoint), fingerprint, "", 0

	if d.Platform == relay.PlatformNtfy {
		click, err := BuildShareLink(`{"cluster":"` + clusterID + `","target":"alerts"}`)
		if err != nil {
			return relay.Device{}, err
		}

		d.Click = click
	}

	if err := relay.ValidateDevice(d); err != nil {
		return relay.Device{}, err
	}

	return d, nil
}

// checkRelayEndpoint refuses what the relay would, with the phone's words: an endpoint that
// is not https, and keys that do not fit the platform.
func checkRelayEndpoint(d relay.Device) error {
	u, err := url.Parse(d.Endpoint)

	switch {
	case len(d.Endpoint) > relayMaxEndpoint:
		return fmt.Errorf("endpoint: at most %d characters", relayMaxEndpoint)
	case err != nil || u.Scheme != "https":
		return errors.New("endpoint: an https URL is required")
	case u.Host == "":
		return errors.New("endpoint: no host")
	}

	switch d.Platform {
	case relay.PlatformNtfy:
		if d.P256DH != "" || d.Auth != "" {
			return errors.New("ntfy takes no p256dh or auth key")
		}
	case relay.PlatformUnifiedPush:
		if p, err := decodeBase64URL(d.P256DH); err != nil || len(p) != relayP256DHLen {
			return fmt.Errorf("p256dh: a %d-byte P-256 public key, base64url", relayP256DHLen)
		}

		if a, err := decodeBase64URL(d.Auth); err != nil || len(a) != relayAuthLen {
			return fmt.Errorf("auth: %d bytes, base64url", relayAuthLen)
		}
	default:
		return fmt.Errorf("platform: %q or %q", relay.PlatformUnifiedPush, relay.PlatformNtfy)
	}

	return nil
}

// decodeBase64URL reads a key in base64url, padded or not (the standard alphabet too, as the
// relay accepts it).
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}

	return base64.RawStdEncoding.DecodeString(s)
}

// relayDeviceID is the same for the same cluster and endpoint, so pairing again replaces the
// device rather than adding one.
func relayDeviceID(fingerprint, endpoint string) string {
	sum := sha256.Sum256([]byte(fingerprint + "\x00" + endpoint))

	return hex.EncodeToString(sum[:])[:16]
}

// parseRelaySource reads a source from RelayDiscover: the proxy mode only.
func parseRelaySource(sourceJSON string) (promSource, error) {
	src, err := parsePromSource(sourceJSON)

	switch {
	case err != nil:
		return promSource{}, errors.New("bad relay source")
	case src.Mode != promModeProxy || src.Namespace == "" || src.Service == "" || src.Port == 0:
		return promSource{}, errors.New("bad relay source: a Service of the cluster is required")
	}

	src.Kind, src.PathPrefix = relayKind, ""

	return src, nil
}

// relayAuditAction describes a pairing change, on the relay's Service.
func relayAuditAction(kubeServer, action string, src promSource, params string) auditAction {
	return auditAction{Server: kubeServer, Action: action, Namespace: src.Namespace, Object: "Service/" + src.Service, Params: params}
}

// relayCall reads the relay's pairing token from its Secret and makes one request to the
// relay API with it; the body of a successful answer.
func relayCall(target kubeTarget, src promSource, method, path string, body []byte) ([]byte, error) {
	call := func(ctx context.Context, k *kubeClient) (promProxyAnswer, error) {
		secret, err := readConfigSource(ctx, k, configKindSecret, src.Namespace, relaySecretName)
		if err != nil {
			return promProxyAnswer{}, fmt.Errorf("read the relay's Secret %s/%s: %w", src.Namespace, relaySecretName, err)
		}

		token := strings.TrimSpace(string(secret.values[relayPairingKey]))
		if token == "" {
			return promProxyAnswer{}, fmt.Errorf("the Secret %s/%s has no %s: is the relay running?", src.Namespace, relaySecretName, relayPairingKey)
		}

		return relayProxyCall(ctx, k, src, method, path, body, token)
	}

	var (
		a   promProxyAnswer
		err error
	)

	if method == http.MethodGet {
		a, err = withKube(target, call)
	} else {
		// Not under the cluster's call scope: leaving the cluster does not cut a pairing. Both
		// changes are idempotent, so a lost answer needs no "may have been applied" warning.
		a, err = withKubeFrom(context.Background(), target, call)
	}

	if err != nil {
		return nil, err
	}

	status, answer, err := a.result(src)

	switch {
	case errors.Is(err, errPromNoAnswer):
		return nil, fmt.Errorf("%s: no answer within %s", src.label(), callTimeout)
	case err != nil:
		return nil, err
	case status == http.StatusUnauthorized:
		return nil, errRelayToken
	case status/100 != 2:
		return nil, fmt.Errorf("%s: the relay refused: %s", src.label(), relayErrorMessage(status, answer))
	}

	return answer, nil
}

// relayProxyCall is promProxyCall with the pairing header: the proxy consumes Authorization.
func relayProxyCall(ctx context.Context, k *kubeClient, src promSource, method, path string, body []byte, token string) (promProxyAnswer, error) {
	proxyPath := serviceProxyPath(src.Namespace, fmt.Sprintf("%s:%d", url.PathEscape(src.Service), src.Port), path)

	contentType := ""
	if body != nil {
		contentType = "application/json"
	}

	resp, data, err := k.send(ctx, method, proxyPath, "application/json", contentType, body, map[string]string{relayPairingHeader: token})

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return promProxyAnswer{timeout: true}, nil
	case err != nil:
		return promProxyAnswer{}, err
	case resp.StatusCode/100 == 2 || !isKubeStatus(data):
		return promProxyAnswer{status: resp.StatusCode, body: data}, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return promProxyAnswer{}, kubeStatusError(resp.StatusCode, data)
	default:
		return promProxyAnswer{status: resp.StatusCode, body: data, kube: true}, nil
	}
}

// relayErrorMessage is the relay's {error}, else the HTTP status.
func relayErrorMessage(status int, body []byte) string {
	var answer struct {
		Error string `json:"error"`
	}

	if json.Unmarshal(body, &answer) == nil && answer.Error != "" {
		return clipUTF8(answer.Error, 200)
	}

	return fmt.Sprintf("HTTP %d", status)
}
