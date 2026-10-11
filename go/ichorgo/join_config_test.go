package ichorgo

import (
	"slices"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"go.yaml.in/yaml/v4"
)

// joinTemplate is a generated config of role with a hostname, an install disk and a static
// address, as a real member's would have.
func joinTemplate(t *testing.T, role machine.Type) string {
	t.Helper()

	in, err := generate.NewInput("test", "https://192.0.2.10:6443", "1.34.0")
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := in.Config(role)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := cfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		t.Fatal(err)
	}

	docs, err := parseConfigDocs(raw)
	if err != nil {
		t.Fatal(err)
	}

	main := docs[0]
	for _, doc := range docs {
		if configDocKind(doc) == "HostnameConfig" {
			setYAMLScalar(doc, []string{"hostname"}, "cp-1")
			deleteYAMLKey(doc, []string{"auto"})
		}
		if configDocKind(doc) == "" {
			main = doc
		}
	}

	setYAMLScalar(main, []string{"machine", "install", "disk"}, "/dev/vda")

	var ifaces yaml.Node
	if err := yaml.Unmarshal([]byte("- interface: eth0\n  addresses: [192.0.2.11/24]\n  routes:\n    - network: 0.0.0.0/0\n      gateway: 192.0.2.1\n"), &ifaces); err != nil {
		t.Fatal(err)
	}

	setYAMLNode(main, []string{"machine", "network", "interfaces"}, ifaces.Content[0])

	text, err := encodeConfigDocs(docs)
	if err != nil {
		t.Fatal(err)
	}

	return text
}

func joinReq(role, address string) joinConfigRequest {
	return joinConfigRequest{template: "192.0.2.11", role: role, hostname: "worker-3", installDisk: "/dev/sda", staticAddress: address}
}

func TestBuildJoinConfigWorker(t *testing.T) {
	full, warnings, err := buildJoinConfig(joinTemplate(t, machine.TypeWorker), joinReq(joinRoleWorker, ""))
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes([]byte(full))
	if err != nil {
		t.Fatal(err)
	}

	if got := provider.Machine().Type().String(); got != joinRoleWorker {
		t.Errorf("type = %s", got)
	}

	if !strings.Contains(full, "worker-3") || strings.Contains(full, "cp-1") {
		t.Errorf("hostname not replaced")
	}

	if got := provider.Machine().Install().Disk(); got != "/dev/sda" {
		t.Errorf("disk = %s", got)
	}

	if strings.Contains(full, "192.0.2.11/24") || strings.Contains(full, "192.0.2.1\n") {
		t.Error("the template's address and route must be gone")
	}

	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "DHCP") || strings.Contains(joined, "etcd") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestBuildJoinConfigKeepsTheTemplateRole(t *testing.T) {
	if _, _, err := buildJoinConfig(joinTemplate(t, machine.TypeControlPlane), joinReq(joinRoleWorker, "")); err == nil ||
		!strings.Contains(err.Error(), "pick a worker as the template") {
		t.Errorf("err = %v", err)
	}

	if _, _, err := buildJoinConfig(joinTemplate(t, machine.TypeWorker), joinReq(joinRoleControlPlane, "")); err == nil {
		t.Error("a worker template cannot make a control plane")
	}
}

func TestBuildJoinConfigStaticAddressAndControlPlane(t *testing.T) {
	full, warnings, err := buildJoinConfig(joinTemplate(t, machine.TypeControlPlane), joinReq(joinRoleControlPlane, "192.0.2.23/24"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(full, "192.0.2.23/24") || strings.Contains(full, "192.0.2.11/24") {
		t.Errorf("addresses not replaced:\n%s", full)
	}

	if !strings.Contains(strings.Join(warnings, "\n"), "joins etcd") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestBuildJoinConfigEditsAndValidation(t *testing.T) {
	req := joinReq(joinRoleWorker, "")
	req.edits = []configEdit{{Doc: 0, Path: []string{"machine", "install", "wipe"}, Op: configOpAdd, Type: "boolean", Value: "true"}}

	// "add" on a scalar fails: the error names the path.
	if _, _, err := buildJoinConfig(joinTemplate(t, machine.TypeWorker), req); err == nil || !strings.Contains(err.Error(), "machine.install.wipe") {
		t.Errorf("err = %v", err)
	}

	// A config the machinery refuses is said in words.
	broken := strings.Replace(joinTemplate(t, machine.TypeWorker), "clusterName: test", "clusterName: ''", 1)
	if _, _, err := buildJoinConfig(broken, joinReq(joinRoleWorker, "")); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Errorf("broken err = %v", err)
	}
}

func TestJoinPreviewHoldsNoSecret(t *testing.T) {
	template := joinTemplate(t, machine.TypeWorker)

	full, _, err := buildJoinConfig(template, joinReq(joinRoleWorker, ""))
	if err != nil {
		t.Fatal(err)
	}

	preview, err := redactConfig(full)
	if err != nil {
		t.Fatal(err)
	}

	docs, err := parseConfigDocs(template)
	if err != nil {
		t.Fatal(err)
	}

	secrets := []string{
		yamlScalarAt(docs[0], []string{"machine", "token"}),
		yamlScalarAt(docs[0], []string{"cluster", "token"}),
	}

	for _, doc := range docs {
		if configDocKind(doc) == "DiscoveryIdentityConfig" {
			secrets = append(secrets, yamlScalarAt(doc, []string{"clusterSecret"}))
		}
	}

	if len(secrets) != 3 || slices.Contains(secrets, "") {
		t.Fatalf("the template's secrets were not found: %d", len(secrets))
	}

	for _, secret := range secrets {
		if secret == "" {
			continue
		}

		if !strings.Contains(full, secret) && !strings.Contains(full, strings.TrimSpace(secret)) {
			t.Errorf("the sealed config lost a secret")
		}

		if strings.Contains(preview, secret) {
			t.Errorf("the preview shows a secret")
		}
	}

	if _, err := encoderCheck(preview); err != nil {
		t.Errorf("preview is not YAML: %v", err)
	}
}

func encoderCheck(text string) (string, error) {
	provider, err := configloader.NewFromBytes([]byte(text))
	if err != nil {
		return "", err
	}

	return provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
}

func TestParseJoinRequestRefusals(t *testing.T) {
	for _, tt := range []struct{ template, role, hostname, disk, address, edits string }{
		{"", "worker", "w-3", "/dev/sda", "", ""},
		{"10.0.0.1", "master", "w-3", "/dev/sda", "", ""},
		{"10.0.0.1", "worker", "w 3", "/dev/sda", "", ""},
		{"10.0.0.1", "worker", "w-3.example.test", "/dev/sda", "", ""},
		{"10.0.0.1", "worker", "w-3", "sda", "", ""},
		{"10.0.0.1", "worker", "w-3", "/dev/sda", "192.0.2.4", ""},
		{"10.0.0.1", "worker", "w-3", "/dev/sda", "", "{"},
	} {
		if _, err := parseJoinRequest(tt.template, tt.role, tt.hostname, tt.disk, tt.address, tt.edits); err == nil {
			t.Errorf("%+v: accepted", tt)
		}
	}
}

func TestJoinNodeConfigDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	worker := demoNodes()[3].Node

	out, err := JoinNodeConfigPreview(cfg, "", worker, "worker", "worker-9", "/dev/sda", "", "")
	p := decodeJSON[joinConfigPreview](t, out, err)

	if !strings.Contains(p.RedactedYAML, "worker-9") || !strings.Contains(p.RedactedYAML, redacted) || p.Size == 0 {
		t.Errorf("preview = %+v", p)
	}

	sealed, err := JoinNodeConfigSealed(cfg, "", worker, "worker", "worker-9", "/dev/sda", "", "")
	if err != nil || strings.Contains(sealed, redacted) {
		t.Fatalf("sealed: %v", err)
	}

	entries := readAudit(t, "", "join-config-generated")
	if len(entries) != 1 || strings.Contains(entries[0].Params, "BEGIN") || !strings.Contains(entries[0].Params, "worker-9") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestJoinNodeConfigSealedRefusedInPrivacyMode(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	if _, err := JoinNodeConfigSealed(cfg, "", demoNodes()[3].Node, "worker", "worker-9", "/dev/sda", "", ""); err == nil {
		t.Error("privacy mode must refuse the sealed config")
	}
}
