package ichorgo

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"sync"
	"time"
)

// The demo's Secrets and ConfigMaps: invented values run through the same description as a
// real object, so the demo shows hints, a certificate and a registry like a cluster would.

// demoCertPEM is a self-signed certificate for demo.example.com, made once per run.
var demoCertPEM = sync.OnceValue(func() []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "demo.example.com", Organization: []string{"Ichor demo"}},
		DNSNames:     []string{"demo.example.com", "www.demo.example.com"},
		NotBefore:    now.Add(-30 * 24 * time.Hour),
		NotAfter:     now.Add(60 * 24 * time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
})

func demoConfigSource(kind, name string) configSource {
	if kind == configKindConfigMap {
		return configSource{kind: kind, values: map[string][]byte{
			"app.properties": []byte("greeting=Hello from Ichor\nlog.level=info\n"),
			"settings.json":  []byte(`{"theme":"dark","replicas":3,"shop":{"currency":"EUR"}}`),
			"favicon.ico":    {0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x10, 0x10},
		}}
	}

	switch {
	case strings.Contains(name, "tls"):
		return configSource{kind: kind, typ: "kubernetes.io/tls", values: map[string][]byte{
			"tls.crt": demoCertPEM(),
			"tls.key": []byte("# Demo cluster: no private key here.\n"),
		}}
	case strings.Contains(name, "registry") || strings.Contains(name, "pull"):
		return configSource{kind: kind, typ: secretTypeDockerConfigJSON, values: map[string][]byte{
			".dockerconfigjson": []byte(`{"auths":{"registry.example.com":{"username":"demo","password":"not-a-real-password"}}}`),
		}}
	default:
		return configSource{kind: kind, typ: "Opaque", values: map[string][]byte{
			"username":    []byte("demo"),
			"password":    []byte("not-a-real-password"),
			"config.json": []byte(`{"host":"db.demo.example.com","port":5432}`),
		}}
	}
}

// demoConfigData describes the demo object; the demo pods named after it use it.
func demoConfigData(kind, namespace, name, key string) (kubeConfigData, error) {
	data, err := buildConfigData(demoConfigSource(kind, name), key)
	if err != nil {
		return kubeConfigData{}, err
	}

	via := []string{configViaEnvFrom, configViaVolume}
	if kind == configKindSecret {
		via = []string{configViaEnv, configViaVolume}
	}

	for _, p := range demoPods() {
		if p.Namespace == namespace && strings.HasPrefix(p.Name, name+"-") {
			data.UsedBy = append(data.UsedBy, kubeConfigUse{Pod: p.Name, Via: via})
		}
	}

	return data, nil
}
