package ichorgo

import (
	"context"
	"strings"
	"testing"
	"time"
)

// sampleOmniJoinConfig is what an invented Omni instance answers: no real instance's token.
const sampleOmniJoinConfig = "apiVersion: v1alpha1\nkind: SideroLinkConfig\napiUrl: https://omni.example.com:8090?jointoken=sample-secret-token\n---\napiVersion: v1alpha1\nkind: EventSinkConfig\nendpoint: '[fdae:41e4:649b:9303::1]:8090'\n---\napiVersion: v1alpha1\nkind: KmsgLogConfig\nname: omni-kmsg\nurl: tcp://[fdae:41e4:649b:9303::1]:8092\n"

func TestCloudImages(t *testing.T) {
	cases := []struct {
		arch string
		want []string
	}{
		{"amd64", []string{"hcloud-amd64.raw.xz", "openstack-amd64.raw.xz", "scaleway-amd64.raw.zst"}},
		{"arm64", []string{"hcloud-arm64.raw.xz", "openstack-arm64.raw.xz", "scaleway-arm64.raw.zst"}},
	}

	for _, c := range cases {
		t.Run(c.arch, func(t *testing.T) {
			images := cloudImages(bootTarget{version: "v1.14.4", schematic: sampleSchematic, arch: c.arch, secureBoot: true})
			if len(images) != len(c.want) {
				t.Fatalf("images = %+v", images)
			}

			for i, img := range images {
				want := "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/" + c.want[i]
				if img.URL != want || img.Arch != c.arch || !strings.HasPrefix(c.want[i], img.Platform+"-") {
					t.Errorf("image %d = %+v, want %s", i, img, want)
				}
			}
		})
	}
}

func TestCloudUserDataFinish(t *testing.T) {
	cases := []struct {
		name        string
		size        int
		wantWarning bool
	}{
		{"small", 12 << 10, false},
		{"at the limit", cloudUserDataLimit, false},
		{"too large", cloudUserDataLimit + 1, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := cloudUserData{UserData: strings.Repeat("a", c.size), Warnings: []string{}}
			d.finish()

			warned := len(d.Warnings) == 1 && strings.Contains(d.Warnings[0], "32 KiB")
			if d.Size != c.size || d.Limit != 32768 || warned != c.wantWarning || d.Images == nil {
				t.Errorf("finish = size %d limit %d warnings %q images %v", d.Size, d.Limit, d.Warnings, d.Images)
			}
		})
	}
}

func TestNodeCloudUserDataDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	worker := demoNodes()[3].Node

	out, err := NodeCloudUserData(cfg, "", worker, "worker", "worker-9", "/dev/sda", "")
	d := decodeJSON[cloudUserData](t, out, err)

	if d.Kind != cloudUserDataTalos || !strings.HasPrefix(d.UserData, "version: v1alpha1") || strings.Contains(d.UserData, redacted) ||
		!strings.Contains(d.UserData, "worker-9") || d.Size != len(d.UserData) || d.Limit != cloudUserDataLimit {
		t.Errorf("user data = kind %s size %d limit %d", d.Kind, d.Size, d.Limit)
	}

	if !strings.Contains(d.RedactedPreview, redacted) || d.Role != "worker" || d.Hostname != "worker-9" || d.TemplateNode != worker {
		t.Errorf("preview/role = %s %s %s", d.Role, d.Hostname, d.TemplateNode)
	}

	if d.TalosVersion != demoTalosVersion || d.Schematic != demoBootSchematic || len(d.Images) != 3 ||
		d.Images[0].URL != "https://factory.talos.dev/image/"+demoBootSchematic+"/"+demoTalosVersion+"/hcloud-amd64.raw.xz" {
		t.Errorf("images = %+v", d.Images)
	}

	if !strings.Contains(strings.Join(d.Warnings, "\n"), "Scaleway") {
		t.Errorf("warnings = %q", d.Warnings)
	}

	entries := readAudit(t, "", "cloud-user-data-generated")
	if len(entries) != 1 || !strings.Contains(entries[0].Params, "worker-9") || strings.Contains(entries[0].Params, "v1alpha1") {
		t.Errorf("audit = %+v", entries)
	}

	for _, key := range []string{`"kind":"talos"`, `"userData"`, `"redactedPreview"`, `"images":[{"platform":"hcloud","arch":"amd64","url"`, `"warnings"`} {
		if !strings.Contains(out, key) {
			t.Errorf("JSON lacks %s", key)
		}
	}
}

func TestNodeCloudUserDataRefusals(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	worker := demoNodes()[3].Node

	cases := []struct {
		name, template, role, hostname, disk, want string
	}{
		{"no template", "", "worker", "worker-9", "/dev/sda", "template"},
		{"no hostname", worker, "worker", "", "/dev/sda", "host name"},
		{"no disk", worker, "worker", "worker-9", "", "not a disk"},
		{"role unlike the template", worker, "controlplane", "cp-9", "/dev/sda", "pick a controlplane"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NodeCloudUserData(cfg, "", c.template, c.role, c.hostname, c.disk, ""); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	if _, err := NodeCloudUserData(cfg, "", worker, "worker", "worker-9", "/dev/sda", ""); err == nil || !strings.Contains(err.Error(), "privacy mode") {
		t.Errorf("privacy mode: %v", err)
	}

	omni := "context: acme\ncontexts:\n    acme:\n        endpoints:\n            - https://omni.example.com\n        auth:\n            siderov1:\n                identity: ops@example.com\n"
	if _, err := NodeCloudUserData(omni, "", "", "", "", "", ""); err == nil || !strings.Contains(err.Error(), "join token") {
		t.Errorf("omni in privacy mode: %v", err)
	}
}

func TestOmniJoinConfig(t *testing.T) {
	useMemAuthStore(t)

	args := []string{"siderolink.api=https://omni.example.com:8090?jointoken=sample-secret-token"}
	f := &fakeOmniAPI{joinToken: "sample-token-id", joinArgs: args, joinConfig: sampleOmniJoinConfig}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	cfgCtx, err := omniInstanceContext(endpoint, testUserIdentity)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	join, err := omniJoinConfig(ctx, cfgCtx, omniSigner{identity: testUserIdentity, key: f.key}, map[string]string{"ichor/role": "worker", "rack": "b2"})
	if err != nil || join.config != sampleOmniJoinConfig || join.kernelArgs != args[0] {
		t.Fatalf("join = %+v, %v", join, err)
	}

	if f.joinLabels["ichor/role"] != "worker" || f.joinLabels["rack"] != "b2" || len(f.joinLabels) != 2 {
		t.Errorf("labels in the join request = %v", f.joinLabels)
	}

	preview := omniJoinTokenPattern.ReplaceAllString(join.config, "${1}"+redacted)
	if strings.Contains(preview, "sample-secret-token") || !strings.Contains(preview, "jointoken="+redacted) || !strings.Contains(preview, "KmsgLogConfig") {
		t.Errorf("preview = %s", preview)
	}
}
