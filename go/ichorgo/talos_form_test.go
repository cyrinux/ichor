package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func formJSON(t *testing.T, f talosForm) string {
	t.Helper()

	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

func parsedSummary(t *testing.T, yaml string) configSummary {
	t.Helper()

	out, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("ParseConfig: %v\n%s", err, yaml)
	}

	var summary configSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	return summary
}

func TestBuildTalosconfigDirect(t *testing.T) {
	ca, crt, key := testIdentity(t, time.Now().Add(time.Hour))

	yaml, err := BuildTalosconfig(formJSON(t, talosForm{
		Mode:      formModeDirect,
		Name:      " lab ",
		Endpoints: []string{"10.0.0.1", " ", "talos.example.org:50000"},
		Nodes:     []string{"10.0.0.2"},
		CA:        ca,
		Crt:       crt,
		Key:       key,
	}))
	if err != nil {
		t.Fatal(err)
	}

	summary := parsedSummary(t, yaml)
	if summary.Current != "lab" || len(summary.Contexts) != 1 {
		t.Fatalf("summary = %+v", summary)
	}

	got := summary.Contexts[0]
	if strings.Join(got.Endpoints, ",") != "10.0.0.1,talos.example.org:50000" ||
		strings.Join(got.Nodes, ",") != "10.0.0.2" || got.Auth != "" ||
		strings.Join(got.Roles, ",") != "os:admin" {
		t.Fatalf("context = %+v", got)
	}
}

func TestBuildTalosconfigAcceptsPlainPEM(t *testing.T) {
	ca, crt, key := testIdentity(t, time.Now().Add(time.Hour))
	plain := func(s string) string {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}

		return "\n" + string(raw) + "\n"
	}

	form := talosForm{Mode: formModeDirect, Name: "lab", Endpoints: []string{"10.0.0.1"}}

	b64 := form
	b64.CA, b64.Crt, b64.Key = ca, crt, key

	pemForm := form
	pemForm.CA, pemForm.Crt, pemForm.Key = plain(ca), plain(crt), plain(key)

	want, err := BuildTalosconfig(formJSON(t, b64))
	if err != nil {
		t.Fatal(err)
	}

	got, err := BuildTalosconfig(formJSON(t, pemForm))
	if err != nil {
		t.Fatal(err)
	}

	if got != want {
		t.Fatalf("PEM input differs:\n%s\nwant\n%s", got, want)
	}
}

func TestBuildTalosconfigOmni(t *testing.T) {
	yaml, err := BuildTalosconfig(formJSON(t, talosForm{
		Mode:     formModeOmni,
		Name:     "prod",
		OmniURL:  "https://acme.omni.example.com/",
		Cluster:  "prod-eu",
		Identity: "ops@example.com",
	}))
	if err != nil {
		t.Fatal(err)
	}

	got := parsedSummary(t, yaml).Contexts[0]
	if got.Auth != authOmni || got.OmniCluster != "prod-eu" || got.Identity != "ops@example.com" ||
		strings.Join(got.Endpoints, ",") != "https://acme.omni.example.com" || len(got.Roles) != 0 {
		t.Fatalf("context = %+v", got)
	}
}

func TestBuildTalosconfigErrors(t *testing.T) {
	ca, crt, key := testIdentity(t, time.Now().Add(time.Hour))
	_, otherCrt, _ := testIdentity(t, time.Now().Add(time.Hour))

	direct := talosForm{Mode: formModeDirect, Name: "lab", Endpoints: []string{"10.0.0.1"}, CA: ca, Crt: crt, Key: key}
	omni := talosForm{Mode: formModeOmni, Name: "prod", OmniURL: "https://omni.example.com", Cluster: "prod"}

	with := func(f talosForm, edit func(*talosForm)) talosForm {
		edit(&f)

		return f
	}

	cases := map[string]talosForm{
		"no mode":           with(direct, func(f *talosForm) { f.Mode = "" }),
		"no name":           with(direct, func(f *talosForm) { f.Name = "  " }),
		"no endpoint":       with(direct, func(f *talosForm) { f.Endpoints = []string{" "} }),
		"bad endpoint":      with(direct, func(f *talosForm) { f.Endpoints = []string{"10.0.0.1 extra"} }),
		"bad port":          with(direct, func(f *talosForm) { f.Endpoints = []string{"10.0.0.1:port"} }),
		"bad node":          with(direct, func(f *talosForm) { f.Nodes = []string{"a/b"} }),
		"no ca":             with(direct, func(f *talosForm) { f.CA = "" }),
		"garbage ca":        with(direct, func(f *talosForm) { f.CA = "not a cert" }),
		"no key":            with(direct, func(f *talosForm) { f.Key = "" }),
		"mismatched pair":   with(direct, func(f *talosForm) { f.Crt = otherCrt }),
		"omni http":         with(omni, func(f *talosForm) { f.OmniURL = "http://omni.example.com" }),
		"omni no host":      with(omni, func(f *talosForm) { f.OmniURL = "https://" }),
		"omni no cluster":   with(omni, func(f *talosForm) { f.Cluster = "" }),
		"omni with a path":  with(omni, func(f *talosForm) { f.OmniURL = "https://omni.example.com/x" }),
		"omni bad identity": with(omni, func(f *talosForm) { f.Identity = "two words" }),
	}

	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := BuildTalosconfig(formJSON(t, form))
			if err == nil {
				t.Fatal("expected an error")
			}

			for _, secret := range []string{"PRIVATE", "CERTIFICATE", "LS0t"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks key material: %v", err)
				}
			}
		})
	}

	if _, err := BuildTalosconfig("{"); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

const omniConfig = `context: prod
contexts:
  prod:
    endpoints:
      - https://acme.omni.example.com
    auth:
      siderov1:
        identity: ops@example.com
    cluster: prod-eu
`

func TestParseConfigOmniContext(t *testing.T) {
	got := parsedSummary(t, omniConfig).Contexts[0]

	if got.Auth != authOmni || got.OmniCluster != "prod-eu" || got.CertNotAfter != 0 || got.Fingerprint == "" || got.ClusterID == "" {
		t.Fatalf("context = %+v", got)
	}

	renamed := parsedSummary(t, strings.ReplaceAll(omniConfig, "prod:", "other:")).Contexts[0]
	if renamed.ClusterID != got.ClusterID || renamed.Fingerprint == got.Fingerprint {
		t.Fatalf("cluster id must follow the cluster, the fingerprint the name: %+v vs %+v", renamed, got)
	}

	otherCluster := parsedSummary(t, strings.ReplaceAll(omniConfig, "cluster: prod-eu", "cluster: dev")).Contexts[0]
	if otherCluster.ClusterID == got.ClusterID || otherCluster.Fingerprint == got.Fingerprint {
		t.Fatal("another Omni cluster must be another cluster")
	}
}

func TestParseConfigOmniNeedsCluster(t *testing.T) {
	if _, err := ParseConfig(strings.ReplaceAll(omniConfig, "cluster: prod-eu\n", "")); err == nil {
		t.Fatal("expected an error for an Omni context without its cluster")
	}
}

func TestMergeConfigKeepsOmniContext(t *testing.T) {
	stored := testConfig(t, time.Now().Add(time.Hour))

	merged, err := MergeConfig(stored, omniConfig, "")
	if err != nil {
		t.Fatal(err)
	}

	summary := parsedSummary(t, merged)
	if len(summary.Contexts) != 3 {
		t.Fatalf("contexts = %+v", summary.Contexts)
	}

	var omni contextSummary
	for _, c := range summary.Contexts {
		if c.Auth == authOmni {
			omni = c
		}
	}

	if omni.Name != "prod" || omni.OmniCluster != "prod-eu" || omni.Identity != "ops@example.com" {
		t.Fatalf("Omni context lost on merge: %+v", omni)
	}

	conflicts, err := ImportConflicts(merged, omniConfig)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(conflicts, `"sameAs":"prod"`) {
		t.Fatalf("re-importing the same Omni cluster must match it: %s", conflicts)
	}
}
