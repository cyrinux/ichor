package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

const (
	sopsTestAge  = "age1testrecipient00000000000000000000000000000000000000000000"
	sopsTestPGP  = "FA4E0000111122223333444455556666777788AB"
	sopsTestEnc  = "ENC[AES256_GCM,data:ZXhhbXBsZQ==,iv:aXY=,tag:dGFn,type:str]"
	sopsTestMeta = "sops:\n    age:\n        - recipient: " + sopsTestAge + "\n          enc: x\n" +
		"    pgp:\n        - fp: " + sopsTestPGP + "\n          enc: x\n" +
		"    lastmodified: \"2026-10-01T08:00:00Z\"\n    mac: " + sopsTestEnc + "\n    version: 3.10.2\n"
)

func TestSopsInspect(t *testing.T) {
	// A Kubernetes Secret encrypted on data/stringData only: its metadata is clear by design.
	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n    name: db\ndata:\n    password: " + sopsTestEnc + "\n" +
		sopsTestMeta + "    encrypted_regex: ^(data|stringData)$\n"

	info := inspectSops(secret)
	if !info.Encrypted || info.Format != sopsFormatYAML || info.EncryptedKeys != 1 || len(info.ClearKeys) != 0 || info.EncryptedRegex == "" {
		t.Fatalf("secret %+v", info)
	}

	if !slices.Equal(info.Recipients, []sopsRecipient{{"age", sopsTestAge}, {"pgp", sopsTestPGP}}) || info.LastModified == "" {
		t.Errorf("recipients %+v", info.Recipients)
	}

	// Half-encrypted: a value added by hand after the encryption.
	half := "cluster:\n    id: " + sopsTestEnc + "\n    secret: plain-value\n" + sopsTestMeta
	if info = inspectSops(half); !info.Encrypted || !slices.Equal(info.ClearKeys, []string{"cluster.secret"}) {
		t.Fatalf("half %+v", info)
	}

	// An _unencrypted key is clear by SOPS's default rule.
	if info = inspectSops("name_unencrypted: web\ntoken: " + sopsTestEnc + "\n" + sopsTestMeta); len(info.ClearKeys) != 0 {
		t.Errorf("suffix %+v", info)
	}

	dotenv := "DB_PASSWORD=" + sopsTestEnc + "\nsops_age__list_0__map_recipient=" + sopsTestAge + "\nsops_lastmodified=2026-10-01T08:00:00Z\nsops_version=3.10.2\n"
	if info = inspectSops(dotenv); !info.Encrypted || info.Format != sopsFormatDotenv || info.EncryptedKeys != 1 || len(info.Recipients) != 1 {
		t.Fatalf("dotenv %+v", info)
	}

	binary := `{"data": "` + sopsTestEnc + `", "sops": {"mac": "x", "age": [{"recipient": "` + sopsTestAge + `"}]}}`
	if info = inspectSops(binary); !info.Encrypted || info.Format != sopsFormatBinary {
		t.Fatalf("binary %+v", info)
	}

	for _, plain := range []string{"apiVersion: v1\nkind: ConfigMap\n", "KEY=value\n", "not: [yaml"} {
		if info = inspectSops(plain); info.Encrypted || info.EncryptedKeys != 0 || len(info.ClearKeys) != 0 {
			t.Errorf("%q: %+v", plain, info)
		}
	}
}

const sopsTestRules = `creation_rules:
  - path_regex: talos/.*\.sops\.ya?ml
    age: ` + sopsTestAge + `, age1testsecond0000000000000000000000000000000000000000000000
  - path_regex: kubernetes/.*secret.*\.ya?ml
    encrypted_regex: ^(data|stringData)$
    key_groups:
      - age:
          - ` + sopsTestAge + `
        pgp:
          - ` + sopsTestPGP + `
`

func TestSopsRuleFor(t *testing.T) {
	r, err := sopsRuleFor(sopsTestRules, "talos/talsecret.sops.yaml")
	if err != nil || !r.Matched || len(r.Recipients) != 2 || r.Recipients[1].ID != "age1testsecond0000000000000000000000000000000000000000000000" {
		t.Fatalf("talos %+v %v", r, err)
	}

	r, _ = sopsRuleFor(sopsTestRules, "kubernetes/apps/web/secret.yaml")
	if !r.Matched || r.EncryptedRegex != "^(data|stringData)$" || !slices.Equal(r.Recipients, []sopsRecipient{{"age", sopsTestAge}, {"pgp", sopsTestPGP}}) {
		t.Fatalf("key groups %+v", r)
	}

	for _, p := range []string{"kubernetes/apps/web/deployment.yaml", "README.md"} {
		if r, _ = sopsRuleFor(sopsTestRules, p); r.Matched {
			t.Errorf("%s matched %+v", p, r)
		}
	}

	if r, _ = sopsRuleFor("", "a.yaml"); r.Matched {
		t.Error("no .sops.yaml matched")
	}

	if r, _ = sopsRuleFor("creation_rules:\n  - age: "+sopsTestAge+"\n", "any/path.yaml"); !r.Matched {
		t.Error("a rule without path_regex matches every path")
	}

	out, err := SopsRuleFor(sopsTestRules, "talos/x.sops.yaml")
	if err != nil || !strings.Contains(out, `"matched":true`) {
		t.Errorf("exported %s %v", out, err)
	}
}

func TestSopsEditWarning(t *testing.T) {
	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: web\nstringData:\n  password: hunter2\n"

	w := sopsEditWarning(sopsTestRules, "kubernetes/apps/web/secret.yaml", secret)
	if !w.Warn || w.What != sopsWhatSecret || w.Rule.PathRegex == "" {
		t.Fatalf("secret %+v", w)
	}

	// The same file where no rule encrypts, or once encrypted, or not a secret: no warning.
	for _, c := range []struct{ path, content string }{
		{"kubernetes/apps/web/web.yaml", secret},
		{"kubernetes/apps/web/secret.yaml", secret + sopsTestMeta},
		{"kubernetes/apps/web/secret.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\n"},
	} {
		if w = sopsEditWarning(sopsTestRules, c.path, c.content); w.Warn {
			t.Errorf("%s: %+v", c.path, w)
		}
	}

	if w = sopsEditWarning(sopsTestRules, "talos/patch.sops.yaml", "cluster:\n  secret: abc\n"); !w.Warn || w.What != "cluster.secret" {
		t.Errorf("talos %+v", w)
	}
}

func TestSopsTreeAndDemo(t *testing.T) {
	out, err := GitForgeTree(`{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, "talos")
	if err != nil {
		t.Fatal(err)
	}

	var tree gitforge.Tree
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatal(err)
	}

	for _, e := range tree.Entries {
		if want := e.Path == "talos/talsecret.sops.yaml"; e.SOPS != want {
			t.Errorf("%s sops=%v", e.Path, e.SOPS)
		}
	}

	info := inspectSops(demoGitForgeFiles["talos/talsecret.sops.yaml"])
	if !info.Encrypted || len(info.Recipients) != 2 || info.EncryptedKeys != 3 || len(info.ClearKeys) != 0 {
		t.Errorf("demo %+v", info)
	}
}

func TestSopsReviewCases(t *testing.T) {
	// Comment regexes are not read: no clear key is named rather than false ones.
	if info := inspectSops("a: clear\n# sops:enc\nb: " + sopsTestEnc + "\n" + sopsTestMeta + "    encrypted_comment_regex: sops:enc\n"); !info.Encrypted || len(info.ClearKeys) != 0 {
		t.Errorf("comment regex %+v", info)
	}

	// Number keys are walked like any other.
	if info := inspectSops("ports:\n    80: " + sopsTestEnc + "\n" + sopsTestMeta); info.EncryptedKeys != 1 || len(info.ClearKeys) != 0 {
		t.Errorf("number keys %+v", info)
	}

	// List items keep their parent's key, as SOPS walks them: a regex on digits matches none.
	if info := inspectSops("hosts:\n    - a\n    - b\n" + sopsTestMeta + "    encrypted_regex: ^[0-9]\n"); len(info.ClearKeys) != 0 {
		t.Errorf("list indices %+v", info)
	}

	// A dotenv file's own rules.
	dotenv := "PUBLIC=hello\nTOKEN=" + sopsTestEnc + "\nsops_encrypted_regex=^TOKEN$\nsops_mac=x\nsops_version=3.10.2\n"
	if info := inspectSops(dotenv); !info.Encrypted || info.EncryptedKeys != 1 || len(info.ClearKeys) != 0 {
		t.Errorf("dotenv rules %+v", info)
	}

	// A values file's own sops: section, a dotenv's sops_enabled: not encrypted.
	for _, plain := range []string{"sops:\n  enabled: true\nimage: web\n", "sops_enabled=true\nKEY=v\n"} {
		if info := inspectSops(plain); info.Encrypted {
			t.Errorf("%q: %+v", plain, info)
		}
	}

	// The creation rules are clear text: no lock.
	if isSOPSPath(".sops.yaml") || isSOPSPath("clusters/lab/.sops.yml") || !isSOPSPath("a/secret.sops.yaml") {
		t.Error("isSOPSPath and the config file")
	}
}

func TestSopsWarningReviewCases(t *testing.T) {
	path := "kubernetes/apps/web/secret.yaml"
	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: web\nstringData:\n  password: hunter2\n"
	list := "apiVersion: v1\nkind: List\nitems:\n  - apiVersion: v1\n    kind: Secret\n    metadata:\n      name: a\n    data:\n      k: dg==\n"

	for _, c := range []struct{ content, want string }{
		{list, sopsWhatSecret},
		{secret + "---\n" + secret + sopsTestMeta, sopsWhatSecret},
		{"apiVersion: v1\nkind: Secret\nmetadata:\n  name: web\ndata: {}\n", ""},
		{"secrets:\n  bootstraptoken: abc.def\ncerts:\n  etcd:\n    crt: x\n", sopsWhatTalos},
	} {
		if w := sopsEditWarning(sopsTestRules, path, c.content); w.What != c.want || w.Warn != (c.want != "") {
			t.Errorf("%q: %+v, want %q", c.content, w, c.want)
		}
	}

	out, err := SopsEditWarning(sopsTestRules, path, secret)
	if err != nil || !strings.Contains(out, `"warn":true`) {
		t.Errorf("exported %s %v", out, err)
	}
}
