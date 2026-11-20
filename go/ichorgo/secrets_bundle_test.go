package ichorgo

import (
	"errors"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"go.yaml.in/yaml/v4"
)

// secretsTestConfig generates a config of role from a fresh bundle, and returns both.
func secretsTestConfig(t *testing.T, role machine.Type) (*secrets.Bundle, string) {
	t.Helper()

	bundle, err := secrets.NewBundle(secrets.NewClock(), config.TalosVersionCurrent, secrets.WithECDSAServiceAccountKey())
	if err != nil {
		t.Fatal(err)
	}

	in, err := generate.NewInput("secrets-test", "https://192.0.2.10:6443", "1.34.0", generate.WithSecretsBundle(bundle))
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := in.Config(role)
	if err != nil {
		t.Fatal(err)
	}

	text, err := cfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		t.Fatal(err)
	}

	return bundle, text
}

func TestSecretsBundleRoundTrip(t *testing.T) {
	want, text := secretsTestConfig(t, machine.TypeControlPlane)

	out, err := secretsBundleYAML(text)
	if err != nil {
		t.Fatal(err)
	}

	got := &secrets.Bundle{Clock: secrets.NewClock()}
	if err := yaml.Unmarshal([]byte(out), got); err != nil {
		t.Fatalf("%v: the export is not a secrets.yaml", err)
	}

	if err := got.Validate(config.TalosVersionCurrent); err != nil {
		t.Errorf("exported bundle does not validate: %v", err)
	}

	for name, pair := range map[string][2]string{
		"os CA":           {string(want.Certs.OS.Crt), string(got.Certs.OS.Crt)},
		"os key":          {string(want.Certs.OS.Key), string(got.Certs.OS.Key)},
		"k8s CA":          {string(want.Certs.K8s.Crt), string(got.Certs.K8s.Crt)},
		"etcd CA":         {string(want.Certs.Etcd.Crt), string(got.Certs.Etcd.Crt)},
		"aggregator CA":   {string(want.Certs.K8sAggregator.Crt), string(got.Certs.K8sAggregator.Crt)},
		"service account": {string(want.Certs.K8sServiceAccount.Key), string(got.Certs.K8sServiceAccount.Key)},
		"cluster id":      {want.Cluster.ID, got.Cluster.ID},
		"cluster secret":  {want.Cluster.Secret, got.Cluster.Secret},
		"bootstrap token": {want.Secrets.BootstrapToken, got.Secrets.BootstrapToken},
		"secretbox":       {want.Secrets.SecretboxEncryptionSecret, got.Secrets.SecretboxEncryptionSecret},
		"trustd token":    {want.TrustdInfo.Token, got.TrustdInfo.Token},
	} {
		if pair[0] == "" || pair[0] != pair[1] {
			t.Errorf("%s differs after the round trip", name)
		}
	}

	// talosctl gen config --with-secrets: the bundle makes a config again.
	if _, err := generate.NewInput("secrets-test", "https://192.0.2.10:6443", "1.34.0", generate.WithSecretsBundle(got)); err != nil {
		t.Errorf("the exported bundle cannot generate a config: %v", err)
	}
}

func TestSecretsBundleRefusesWorker(t *testing.T) {
	_, text := secretsTestConfig(t, machine.TypeWorker)

	if _, err := secretsBundleYAML(text); !errors.Is(err, errSecretsWorker) {
		t.Errorf("worker: err = %v", err)
	}
}

func TestSecretsBundleRefusesUnreadable(t *testing.T) {
	for _, text := range []string{"", "version: v1alpha1\n", "{"} {
		if _, err := secretsBundleYAML(text); err == nil {
			t.Errorf("%q: accepted", text)
		}
	}
}

func TestClusterSecretsBundleRefusals(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ClusterSecretsBundle(demo, "", demoNodes()[0].Node); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: err = %v", err)
	}

	if _, err := ClusterSecretsBundle(demo, "", " "); !errors.Is(err, errSecretsNoNode) {
		t.Errorf("no node: err = %v", err)
	}

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	if _, err := ClusterSecretsBundle(demo, "", demoNodes()[0].Node); err == nil || err.Error() != errSecretsPrivacy.Error() {
		t.Errorf("privacy: err = %v", err)
	}
}

func TestClusterSecretsBundleFake(t *testing.T) {
	withDataDir(t)

	f, cfg := threeControlPlanes(t, true)
	f.putMachineConfig(t, "192.0.2.51")

	out, err := ClusterSecretsBundle(cfg, "fake", "192.0.2.51")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "certs:") || !strings.Contains(out, "trustdinfo:") || strings.Contains(out, redacted) {
		t.Errorf("export = %.200s", out)
	}

	entries := readAudit(t, "", "secrets-bundle-exported")
	if len(entries) != 1 || entries[0].Node != "192.0.2.51" || entries[0].Params != "" {
		t.Errorf("audit = %+v", entries)
	}
}
