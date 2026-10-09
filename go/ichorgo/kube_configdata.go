package ichorgo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The data of a Secret or a ConfigMap key by key: size, what it looks like (JSON, PEM,
// text, binary), the certificates it holds, and the pods that use it.
//
// Security model:
//   - A Secret's values never leave this function unless the app names ONE key: the app
//     reveals per key, behind its app lock, and only that value sits in memory and on screen.
//     A ConfigMap's values are not secret (the YAML shows them): they always come back.
//   - Screenshot mode refuses to reveal a Secret's value (errSecretRevealScreenshot): masking
//     would alter it (a copied password with an address swapped is wrong) and a value in clear
//     has no place in a screenshot. What does come back (key names, certificates, usernames
//     blanked) is masked like every result.
//   - Nothing here is logged, audited (read-only) or collected by the support bundle or the
//     diagnosis.

const (
	configKindSecret    = "Secret"
	configKindConfigMap = "ConfigMap"

	configHintJSON   = "json"
	configHintPEM    = "pem"
	configHintText   = "text"
	configHintBinary = "binary"

	secretTypeDockerConfigJSON = "kubernetes.io/dockerconfigjson"
	secretTypeDockerCfg        = "kubernetes.io/dockercfg"
)

var errSecretRevealScreenshot = errors.New("secret values are not shown in screenshot mode")

// configKeyPattern is what Kubernetes accepts as a Secret or ConfigMap key.
var configKeyPattern = regexp.MustCompile(`^[-._a-zA-Z0-9]{1,253}$`)

// kubeConfigData is a Secret or ConfigMap read key by key.
type kubeConfigData struct {
	Kind string `json:"kind"`
	// Type is the Secret's type ("Opaque", "kubernetes.io/tls"...), "" for a ConfigMap.
	Type string          `json:"type"`
	Keys []kubeConfigKey `json:"keys"`
	// Registries are the credentials of a docker config Secret, without the passwords.
	Registries []kubeRegistryAuth `json:"registries"`
	UsedBy     []kubeConfigUse    `json:"usedBy"`
	// UsedByUnknown when the pods of the namespace could not be listed (RBAC).
	UsedByUnknown bool `json:"usedByUnknown,omitempty"`
}

type kubeConfigKey struct {
	Key string `json:"key"`
	// Size is the value's length in bytes, decoded.
	Size int    `json:"size"`
	Hint string `json:"hint"`
	// Revealed tells Value holds the value: every ConfigMap key, the one Secret key asked for.
	Revealed bool   `json:"revealed,omitempty"`
	Value    string `json:"value,omitempty"`
	// Base64 tells Value is base64: the value is binary.
	Base64 bool          `json:"base64,omitempty"`
	Cert   *kubeCertInfo `json:"cert,omitempty"`
}

// kubeCertInfo is the first certificate of a PEM value; Count how many the value holds (a chain).
type kubeCertInfo struct {
	Subject   string   `json:"subject"`
	Issuer    string   `json:"issuer"`
	NotBefore int64    `json:"notBefore"` // Unix seconds
	NotAfter  int64    `json:"notAfter"`  // Unix seconds
	DNSNames  []string `json:"dnsNames"`
	Count     int      `json:"count"`
}

type kubeRegistryAuth struct {
	Registry string `json:"registry"`
	// Username is "" in screenshot mode.
	Username string `json:"username"`
}

// kubeConfigUse is a pod of the namespace that uses the object, and how (env, envFrom,
// volume, projected, imagePullSecret).
type kubeConfigUse struct {
	Pod string   `json:"pod"`
	Via []string `json:"via"`
}

// configSource is the raw object: its values decoded, by key.
type configSource struct {
	kind   string
	typ    string
	values map[string][]byte
}

// KubeConfigData reads a Secret or ConfigMap (kind) of namespace key by key, as a JSON
// kubeConfigData. A Secret's values stay hidden except the one of key ("" for none), refused
// in screenshot mode; a ConfigMap's all come back, key is ignored. usedBy lists the pods of
// the namespace referencing it.
func KubeConfigData(configYAML, contextName, kubeServer, kind, namespace, name, key string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := checkConfigDataArgs(kind, namespace, name, key); err != nil {
		return "", err
	}

	if kind == configKindSecret && key != "" && privacy.isEnabled() {
		return "", errSecretRevealScreenshot
	}

	if isDemoContext(configYAML, contextName) {
		data, err := demoConfigData(kind, namespace, name, key)
		if err != nil {
			return "", err
		}

		return toJSON(data)
	}

	data, err := withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (kubeConfigData, error) {
		return readConfigData(ctx, k, kind, namespace, name, key)
	})
	if err != nil {
		return "", err
	}

	return toJSON(data)
}

func readConfigData(ctx context.Context, k *kubeClient, kind, namespace, name, key string) (kubeConfigData, error) {
	src, err := readConfigSource(ctx, k, kind, namespace, name)
	if err != nil {
		return kubeConfigData{}, err
	}

	data, err := buildConfigData(src, key)
	if err != nil {
		return kubeConfigData{}, err
	}

	pods, err := listConfigRefPods(ctx, k, namespace)
	if err != nil {
		// Best effort: credentials that may read the Secret may not list the pods.
		data.UsedByUnknown = true
	} else {
		data.UsedBy = configUsers(pods, kind, name)
	}

	return data, nil
}

func checkConfigDataArgs(kind, namespace, name, key string) error {
	if kind != configKindSecret && kind != configKindConfigMap {
		return fmt.Errorf("invalid kind %q: Secret or ConfigMap", kind)
	}

	if namespace == "" {
		return errors.New("a namespace is required")
	}

	if err := validateNamespace(namespace); err != nil {
		return err
	}

	if !kubeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid name %q", name)
	}

	if key != "" && !configKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid key %q", key)
	}

	return nil
}

// readConfigSource reads the object and decodes its values. Nothing of it is logged.
func readConfigSource(ctx context.Context, k *kubeClient, kind, namespace, name string) (configSource, error) {
	resource := "configmaps"
	if kind == configKindSecret {
		resource = "secrets"
	}

	var obj struct {
		Type       string            `json:"type"`
		Data       map[string]string `json:"data"`
		BinaryData map[string]string `json:"binaryData"`
	}

	if err := k.get(ctx, scopedPath("/api/v1", namespace, resource)+"/"+url.PathEscape(name), &obj); err != nil {
		return configSource{}, err
	}

	src := configSource{kind: kind, typ: obj.Type, values: map[string][]byte{}}

	for key, v := range obj.Data {
		if kind == configKindConfigMap {
			src.values[key] = []byte(v)

			continue
		}

		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			// Never the value in the error: it is a Secret's.
			return configSource{}, fmt.Errorf("the value of key %q is not base64", key)
		}

		src.values[key] = raw
	}

	for key, v := range obj.BinaryData {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return configSource{}, fmt.Errorf("the value of key %q is not base64", key)
		}

		src.values[key] = raw
	}

	if kind == configKindConfigMap {
		src.typ = ""
	}

	return src, nil
}

// buildConfigData describes src key by key; reveal names the one Secret key whose value is
// given ("" for none), a ConfigMap gives all of them.
func buildConfigData(src configSource, reveal string) (kubeConfigData, error) {
	if src.kind == configKindSecret && reveal != "" {
		if _, ok := src.values[reveal]; !ok {
			return kubeConfigData{}, fmt.Errorf("no key %q in this Secret", reveal)
		}
	}

	data := kubeConfigData{Kind: src.kind, Type: src.typ, Keys: []kubeConfigKey{}, Registries: []kubeRegistryAuth{}, UsedBy: []kubeConfigUse{}}

	keys := make([]string, 0, len(src.values))
	for key := range src.values {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, key := range keys {
		raw := src.values[key]
		entry := kubeConfigKey{Key: key, Size: len(raw), Hint: valueHint(raw)}

		if entry.Hint == configHintPEM {
			entry.Cert = parseCertInfo(raw)
		}

		if src.kind == configKindConfigMap || key == reveal {
			entry.Revealed = true
			entry.Value, entry.Base64 = displayValue(raw, entry.Hint)
		}

		data.Keys = append(data.Keys, entry)
	}

	data.Registries = dockerRegistries(src)

	return data, nil
}

// displayValue is the value as shown: text as is, binary as base64.
func displayValue(raw []byte, hint string) (string, bool) {
	if hint == configHintBinary {
		return base64.StdEncoding.EncodeToString(raw), true
	}

	return string(raw), false
}

// valueHint tells what a value looks like: PEM, JSON, text or binary.
func valueHint(raw []byte) string {
	if !utf8.Valid(raw) || bytes.ContainsFunc(raw, isBinaryRune) {
		return configHintBinary
	}

	if bytes.Contains(raw, []byte("-----BEGIN ")) {
		return configHintPEM
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed) {
		return configHintJSON
	}

	return configHintText
}

// isBinaryRune tells a control character text does not hold (tabs and line ends do).
func isBinaryRune(r rune) bool {
	return r < 0x20 && r != '\t' && r != '\n' && r != '\r' || r == 0x7f
}

// dockerRegistries lists the registries and usernames of a docker config Secret, never the
// passwords; the usernames are blanked in screenshot mode (they often name the user).
func dockerRegistries(src configSource) []kubeRegistryAuth {
	var auths map[string]dockerAuth

	switch src.typ {
	case secretTypeDockerConfigJSON:
		var cfg struct {
			Auths map[string]dockerAuth `json:"auths"`
		}

		if json.Unmarshal(src.values[".dockerconfigjson"], &cfg) != nil {
			return []kubeRegistryAuth{}
		}

		auths = cfg.Auths
	case secretTypeDockerCfg:
		if json.Unmarshal(src.values[".dockercfg"], &auths) != nil {
			return []kubeRegistryAuth{}
		}
	default:
		return []kubeRegistryAuth{}
	}

	hideUsers := privacy.isEnabled()
	out := make([]kubeRegistryAuth, 0, len(auths))

	for registry, a := range auths {
		user := a.user()
		if hideUsers {
			user = ""
		}

		out = append(out, kubeRegistryAuth{Registry: registry, Username: user})
	}

	slices.SortFunc(out, func(a, b kubeRegistryAuth) int { return strings.Compare(a.Registry, b.Registry) })

	return out
}

type dockerAuth struct {
	Username string `json:"username"`
	Auth     string `json:"auth"`
}

// user is the username, from its field or from auth ("user:password", base64).
func (a dockerAuth) user() string {
	if a.Username != "" {
		return a.Username
	}

	raw, err := base64.StdEncoding.DecodeString(a.Auth)
	if err != nil {
		return ""
	}

	user, _, _ := strings.Cut(string(raw), ":")

	return user
}
