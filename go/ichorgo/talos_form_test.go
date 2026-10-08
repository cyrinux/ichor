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
		strings.Join(got.Nodes, ",") != "10.0.0.2" || got.Omni ||
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
	if !got.Omni || got.Cluster != "prod-eu" || got.Identity != "ops@example.com" || got.SignIn != omniUserMethod ||
		strings.Join(got.Endpoints, ",") != "https://acme.omni.example.com" || len(got.Roles) != 0 {
		t.Fatalf("context = %+v", got)
	}
}

func TestBuildTalosconfigErrors(t *testing.T) {
	ca, crt, key := testIdentity(t, time.Now().Add(time.Hour))
	_, otherCrt, _ := testIdentity(t, time.Now().Add(time.Hour))

	direct := talosForm{Mode: formModeDirect, Name: "lab", Endpoints: []string{"10.0.0.1"}, CA: ca, Crt: crt, Key: key}
	omni := talosForm{Mode: formModeOmni, Name: "prod", OmniURL: "https://omni.example.com", Cluster: "prod", Identity: "ops@example.com"}

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
		"omni no identity":  with(omni, func(f *talosForm) { f.Identity = "" }),
		"omni bad identity": with(omni, func(f *talosForm) { f.Identity = "two words@example.com" }),
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

func TestBuildTalosconfigOmniServiceAccount(t *testing.T) {
	yaml, err := BuildTalosconfig(formJSON(t, talosForm{
		Mode:     formModeOmni,
		Name:     "ci",
		OmniURL:  "https://acme.omni.example.com",
		Cluster:  "prod-eu",
		Identity: "ichor" + serviceAccountDomain,
	}))
	if err != nil {
		t.Fatal(err)
	}

	if got := parsedSummary(t, yaml).Contexts[0]; got.SignIn != omniServiceAccountMethod {
		t.Fatalf("a service account identity signs in with its key: %+v", got)
	}
}
