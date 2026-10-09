package ichorgo

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func testCertPEM(t *testing.T, notAfter time.Time, names ...string) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func decodeConfigData(t *testing.T, out string) kubeConfigData {
	t.Helper()

	var data kubeConfigData
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return data
}

func configKey(t *testing.T, data kubeConfigData, key string) kubeConfigKey {
	t.Helper()

	for _, k := range data.Keys {
		if k.Key == key {
			return k
		}
	}

	t.Fatalf("no key %q in %+v", key, data.Keys)

	return kubeConfigKey{}
}

func TestValueHint(t *testing.T) {
	cases := map[string]string{
		"":                                 configHintText,
		"plain words\n\twith tabs":         configHintText,
		`{"a":1}`:                          configHintJSON,
		" [1, 2]\n":                        configHintJSON,
		"{not json":                        configHintText,
		"-----BEGIN PRIVATE KEY-----\nx\n": configHintPEM,
		"\x00\x01\x02":                     configHintBinary,
		"\xff\xfe":                         configHintBinary,
	}

	for value, want := range cases {
		if got := valueHint([]byte(value)); got != want {
			t.Errorf("valueHint(%q) = %s, want %s", value, got, want)
		}
	}
}

func TestKubeConfigDataSecret(t *testing.T) {
	notAfter := time.Now().Add(20 * 24 * time.Hour).Truncate(time.Second)
	chain := string(testCertPEM(t, notAfter, "shop.example.com", "www.shop.example.com")) + string(testCertPEM(t, notAfter, "ca.example.com"))

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/secrets/web-tls": `{"kind":"Secret","type":"kubernetes.io/tls","data":{
		  "tls.crt":"` + b64(chain) + `","tls.key":"` + b64("-----BEGIN EC PRIVATE KEY-----\nnot-a-key\n-----END EC PRIVATE KEY-----\n") + `",
		  "token":"` + b64("not-a-real-token") + `","blob":"` + b64("\x00\x01\x02") + `","conf":"` + b64(`{"a":1}`) + `"}}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[
		  {"metadata":{"name":"web-1"},"spec":{"containers":[{"env":[{"name":"T","valueFrom":{"secretKeyRef":{"name":"web-tls","key":"token"}}}],
		    "envFrom":[{"secretRef":{"name":"web-tls"}}]}],"volumes":[{"secret":{"secretName":"web-tls"}}]}},
		  {"metadata":{"name":"api-1"},"spec":{"initContainers":[{"envFrom":[{"secretRef":{"name":"web-tls"}}]}],
		    "volumes":[{"projected":{"sources":[{"secret":{"name":"web-tls"}}]}}],"imagePullSecrets":[{"name":"web-tls"}]}},
		  {"metadata":{"name":"other"},"spec":{"containers":[{"envFrom":[{"configMapRef":{"name":"web-tls"}}]}],"volumes":[{"configMap":{"name":"web-tls"}}]}}]}`,
	})
	stored := kubeStoreFor(t, f)

	out, err := KubeConfigData(stored, "admin@test", "", "Secret", "shop", "web-tls", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, leak := range []string{"not-a-real-token", "not-a-key", b64("not-a-real-token")} {
		if strings.Contains(out, leak) {
			t.Fatalf("value %q without reveal: %s", leak, out)
		}
	}

	data := decodeConfigData(t, out)
	if data.Kind != "Secret" || data.Type != "kubernetes.io/tls" || len(data.Keys) != 5 || data.Keys[0].Key != "blob" {
		t.Fatalf("data %+v", data)
	}

	for _, k := range data.Keys {
		if k.Revealed || k.Value != "" {
			t.Errorf("key %s revealed without asking", k.Key)
		}
	}

	if k := configKey(t, data, "token"); k.Hint != configHintText || k.Size != len("not-a-real-token") {
		t.Errorf("token %+v", k)
	}

	if k := configKey(t, data, "blob"); k.Hint != configHintBinary || k.Size != 3 {
		t.Errorf("blob %+v", k)
	}

	if k := configKey(t, data, "conf"); k.Hint != configHintJSON {
		t.Errorf("conf %+v", k)
	}

	if k := configKey(t, data, "tls.key"); k.Hint != configHintPEM || k.Cert != nil {
		t.Errorf("tls.key %+v", k)
	}

	cert := configKey(t, data, "tls.crt").Cert
	if cert == nil || cert.NotAfter != notAfter.Unix() || cert.Count != 2 || cert.Subject != "CN=shop.example.com" ||
		!slices.Equal(cert.DNSNames, []string{"shop.example.com", "www.shop.example.com"}) {
		t.Errorf("cert %+v", cert)
	}

	wantUsers := []kubeConfigUse{
		{Pod: "api-1", Via: []string{"envFrom", "projected", "imagePullSecret"}},
		{Pod: "web-1", Via: []string{"env", "envFrom", "volume"}},
	}
	if !slices.EqualFunc(data.UsedBy, wantUsers, func(a, b kubeConfigUse) bool { return a.Pod == b.Pod && slices.Equal(a.Via, b.Via) }) {
		t.Errorf("usedBy %+v", data.UsedBy)
	}

	// One key revealed: only that one.
	out, err = KubeConfigData(stored, "admin@test", "", "Secret", "shop", "web-tls", "token")
	if err != nil {
		t.Fatal(err)
	}

	data = decodeConfigData(t, out)
	if k := configKey(t, data, "token"); !k.Revealed || k.Value != "not-a-real-token" || k.Base64 {
		t.Errorf("token %+v", k)
	}

	if strings.Contains(out, "not-a-key") {
		t.Errorf("another key revealed: %s", out)
	}

	// Binary values come as base64.
	out, err = KubeConfigData(stored, "admin@test", "", "Secret", "shop", "web-tls", "blob")
	if err != nil {
		t.Fatal(err)
	}

	if k := configKey(t, decodeConfigData(t, out), "blob"); !k.Base64 || k.Value != "AAEC" {
		t.Errorf("blob %+v", k)
	}

	if _, err := KubeConfigData(stored, "admin@test", "", "Secret", "shop", "web-tls", "missing"); err == nil {
		t.Error("revealed a key that does not exist")
	}
}

func TestKubeConfigDataDockerConfig(t *testing.T) {
	cfg := `{"auths":{"registry.example.com":{"username":"shopbot","password":"not-a-real-password"},
	  "ghcr.example.com":{"auth":"` + b64("builder:not-a-real-password") + `"}}}`
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/secrets/pull": `{"kind":"Secret","type":"kubernetes.io/dockerconfigjson","data":{".dockerconfigjson":"` + b64(cfg) + `"}}`,
		"GET /api/v1/namespaces/shop/pods":         `{"items":[{"metadata":{"name":"web-1"},"spec":{"imagePullSecrets":[{"name":"pull"}]}}]}`,
	})
	stored := kubeStoreFor(t, f)

	out, err := KubeConfigData(stored, "admin@test", "", "Secret", "shop", "pull", "")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "not-a-real-password") {
		t.Fatalf("password without reveal: %s", out)
	}

	data := decodeConfigData(t, out)
	want := []kubeRegistryAuth{{"ghcr.example.com", "builder"}, {"registry.example.com", "shopbot"}}

	if !slices.Equal(data.Registries, want) {
		t.Errorf("registries %+v", data.Registries)
	}

	if len(data.UsedBy) != 1 || !slices.Equal(data.UsedBy[0].Via, []string{"imagePullSecret"}) {
		t.Errorf("usedBy %+v", data.UsedBy)
	}

	if configKey(t, data, ".dockerconfigjson").Hint != configHintJSON {
		t.Errorf("keys %+v", data.Keys)
	}
}

func TestKubeConfigDataConfigMap(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/configmaps/web": `{"kind":"ConfigMap","data":{"app.properties":"a=1\n","settings.json":"{\"x\":true}"},
		  "binaryData":{"logo.png":"` + b64("\x89PNG\x00") + `"}}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[{"metadata":{"name":"web-1"},"spec":{"containers":[{"env":[{"valueFrom":{"configMapKeyRef":{"name":"web","key":"a"}}}]}],
		  "volumes":[{"configMap":{"name":"web"}},{"secret":{"secretName":"web"}}]}}]}`,
	})
	stored := kubeStoreFor(t, f)

	out, err := KubeConfigData(stored, "admin@test", "", "ConfigMap", "shop", "web", "")
	if err != nil {
		t.Fatal(err)
	}

	data := decodeConfigData(t, out)
	if data.Type != "" || len(data.Keys) != 3 {
		t.Fatalf("data %+v", data)
	}

	if k := configKey(t, data, "app.properties"); !k.Revealed || k.Value != "a=1\n" || k.Hint != configHintText {
		t.Errorf("app.properties %+v", k)
	}

	if k := configKey(t, data, "settings.json"); k.Hint != configHintJSON {
		t.Errorf("settings.json %+v", k)
	}

	if k := configKey(t, data, "logo.png"); k.Hint != configHintBinary || !k.Base64 || k.Value != b64("\x89PNG\x00") || k.Size != 5 {
		t.Errorf("logo.png %+v", k)
	}

	if len(data.UsedBy) != 1 || !slices.Equal(data.UsedBy[0].Via, []string{"env", "volume"}) {
		t.Errorf("usedBy %+v", data.UsedBy)
	}
}

func TestKubeConfigDataPodsForbidden(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/configmaps/web": `{"kind":"ConfigMap","data":{"a":"b"}}`,
	})
	stored := kubeStoreFor(t, f)

	out, err := KubeConfigData(stored, "admin@test", "", "ConfigMap", "shop", "web", "")
	if err != nil {
		t.Fatal(err)
	}

	if data := decodeConfigData(t, out); !data.UsedByUnknown || len(data.Keys) != 1 {
		t.Errorf("data %+v", data)
	}
}

func TestKubeConfigDataArgs(t *testing.T) {
	for _, args := range [][4]string{
		{"Pod", "shop", "web", ""},
		{"Secret", "", "web", ""},
		{"Secret", "shop", "Web!", ""},
		{"Secret", "shop", "web", "a/b"},
	} {
		if _, err := KubeConfigData("", "", "", args[0], args[1], args[2], args[3]); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestKubeConfigDataScreenshotMode(t *testing.T) {
	noKubeDial(t)
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	yaml := demoKubeconfigForTest(t)

	if _, err := KubeConfigData(yaml, "", "", "Secret", "demo", "postgres", "password"); err == nil ||
		!strings.Contains(err.Error(), errSecretRevealScreenshot.Error()) {
		t.Fatalf("reveal in screenshot mode: %v", err)
	}

	out, err := KubeConfigData(yaml, "", "", "Secret", "demo", "registry", "")
	if err != nil {
		t.Fatal(err)
	}

	data := decodeConfigData(t, out)
	if len(data.Registries) != 1 || data.Registries[0].Username != "" {
		t.Errorf("username shown in screenshot mode: %+v", data.Registries)
	}
}

func TestKubeConfigDataDemo(t *testing.T) {
	noKubeDial(t)

	yaml := demoKubeconfigForTest(t)

	out, err := KubeConfigData(yaml, "", "", "Secret", "demo", "postgres", "")
	if err != nil {
		t.Fatal(err)
	}

	data := decodeConfigData(t, out)
	if data.Type != "Opaque" || len(data.Keys) != 3 || strings.Contains(out, "not-a-real-password") {
		t.Fatalf("demo secret %s", out)
	}

	if len(data.UsedBy) != 1 || data.UsedBy[0].Pod != "postgres-0" {
		t.Errorf("demo usedBy %+v", data.UsedBy)
	}

	out, err = KubeConfigData(yaml, "", "", "Secret", "demo", "postgres", "password")
	if err != nil {
		t.Fatal(err)
	}

	if k := configKey(t, decodeConfigData(t, out), "password"); k.Value != "not-a-real-password" {
		t.Errorf("demo reveal %+v", k)
	}

	out, err = KubeConfigData(yaml, "", "", "Secret", "demo", "web-tls", "")
	if err != nil {
		t.Fatal(err)
	}

	if cert := configKey(t, decodeConfigData(t, out), "tls.crt").Cert; cert == nil || cert.NotAfter <= time.Now().Unix() {
		t.Errorf("demo certificate %+v", cert)
	}

	out, err = KubeConfigData(yaml, "", "", "ConfigMap", "demo", "hello-ichor", "")
	if err != nil {
		t.Fatal(err)
	}

	data = decodeConfigData(t, out)
	if len(data.Keys) != 3 || !data.Keys[0].Revealed || len(data.UsedBy) != 3 {
		t.Errorf("demo configmap %s", out)
	}

	if _, err := KubeConfigData(yaml, "", "", "Secret", "demo", "postgres", "nope"); err == nil {
		t.Error("demo revealed a missing key")
	}
}
