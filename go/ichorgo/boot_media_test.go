package ichorgo

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// sampleSchematic is invented: no real cluster's.
const sampleSchematic = "9d2c5e4b1a7f0836c4d2e1b9a0f7c6d5e4b3a2918f7e6d5c4b3a29180f7e6d5c"

func TestBootMediaLinks(t *testing.T) {
	cases := []struct {
		name   string
		target bootTarget
		want   bootLinks
	}{
		{"amd64", bootTarget{version: "v1.14.4", schematic: sampleSchematic, arch: "amd64"}, bootLinks{
			ISO:       "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/metal-amd64.iso",
			IPXE:      "https://pxe.factory.talos.dev/pxe/" + sampleSchematic + "/v1.14.4/metal-amd64",
			Kernel:    "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/kernel-amd64",
			Initramfs: "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/initramfs-amd64.xz",
			Cmdline:   "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/cmdline-metal-amd64",
		}},
		{"arm64 secure boot", bootTarget{version: "v1.14.4", schematic: sampleSchematic, arch: "arm64", secureBoot: true}, bootLinks{
			ISO:       "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/metal-arm64-secureboot.iso",
			IPXE:      "https://pxe.factory.talos.dev/pxe/" + sampleSchematic + "/v1.14.4/metal-arm64-secureboot",
			Kernel:    "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/kernel-arm64",
			Initramfs: "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/initramfs-arm64.xz",
			Cmdline:   "https://factory.talos.dev/image/" + sampleSchematic + "/v1.14.4/cmdline-metal-arm64",
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bootMediaLinks(c.target); got != c.want {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestBooterCommand(t *testing.T) {
	plain := bootTarget{version: "v1.14.4", schematic: sampleSchematic, arch: "amd64"}
	secure := plain
	secure.secureBoot = true

	const omniArgs = "siderolink.api=https://omni.example.com:8090?jointoken=abc&grpc_tunnel=false talos.events.sink=[fdae:41e4:649b:9303::1]:8090 talos.logging.kernel=tcp://[fdae:41e4:649b:9303::1]:8092"

	cases := []struct {
		name, runtime, args string
		target              bootTarget
		want                string
	}{
		{"docker", "docker", "", plain,
			"sudo docker run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.4 --schematic-id=" + sampleSchematic},
		{"podman secure boot", "podman", "", secure,
			"sudo podman run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.4 --schematic-id=" + sampleSchematic + " --secure-boot-enabled"},
		{"omni", "docker", omniArgs, plain,
			"sudo docker run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.4 -k '" + omniArgs + "'"},
		{"quote in args", "podman", "a='b'", plain,
			`sudo podman run --rm --network host ghcr.io/siderolabs/booter:v0.4.0 --talos-version=v1.14.4 -k 'a='\''b'\'''`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := booterCommand(c.runtime, c.target, c.args); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestNormalizeBootArch(t *testing.T) {
	for in, want := range map[string]string{"": "", "amd64": "amd64", "x86_64": "amd64", "ARM64": "arm64", "aarch64": "arm64"} {
		if got, err := normalizeBootArch(in); err != nil || got != want {
			t.Errorf("normalizeBootArch(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	if _, err := normalizeBootArch("riscv64"); err == nil {
		t.Error("riscv64 accepted")
	}
}

func TestBuildBootMedia(t *testing.T) {
	factoryImage := factoryHost + "/metal-installer/" + sampleSchematic + ":v1.14.4"

	cases := []struct {
		name           string
		src            bootSource
		arch           string
		wantVersion    string
		wantSchematic  string
		wantSource     string
		wantArch       string
		wantWarning    string // a substring of one warning, "" for none at all
		wantErrContain string
	}{
		{name: "factory image", src: bootSource{node: "worker-1", version: "v1.14.4", arch: "amd64", image: factoryImage},
			wantVersion: "v1.14.4", wantSchematic: sampleSchematic, wantSource: schematicFromMember, wantArch: "amd64"},
		{name: "custom registry", src: bootSource{node: "worker-1", version: "v1.14.4", arch: "arm64", image: "registry.example.com/talos/installer:v1.14.4"},
			wantVersion: "v1.14.4", wantSchematic: defaultSchematic, wantSource: schematicFromDefault, wantArch: "arm64", wantWarning: "not an Image Factory image"},
		{name: "config unreadable (os:reader)", src: bootSource{node: "worker-1", version: "v1.14.4", arch: "amd64", imageErr: "permission denied"},
			wantVersion: "v1.14.4", wantSchematic: defaultSchematic, wantSource: schematicFromDefault, wantArch: "amd64", wantWarning: "cannot be read (permission denied)"},
		{name: "upgraded past its install image", src: bootSource{node: "worker-1", version: "v1.14.5", arch: "amd64", image: factoryImage},
			wantVersion: "v1.14.5", wantSchematic: sampleSchematic, wantSource: schematicFromMember, wantArch: "amd64", wantWarning: "runs v1.14.5 but its install image is v1.14.4"},
		{name: "version from the tag", src: bootSource{node: "worker-1", image: factoryImage},
			wantVersion: "v1.14.4", wantSchematic: sampleSchematic, wantSource: schematicFromMember, wantArch: "amd64"},
		{name: "arch asked", src: bootSource{node: "worker-1", version: "v1.14.4", arch: "amd64", image: factoryImage}, arch: "arm64",
			wantVersion: "v1.14.4", wantSchematic: sampleSchematic, wantSource: schematicFromMember, wantArch: "arm64"},
		{name: "no version", src: bootSource{node: "worker-1", image: "registry.example.com/installer"}, wantErrContain: "which Talos version"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := buildBootMedia(c.src, c.arch, false)
			if c.wantErrContain != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrContain) {
					t.Fatalf("err = %v, want %q", err, c.wantErrContain)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if m.TalosVersion != c.wantVersion || m.Schematic != c.wantSchematic || m.SchematicSource != c.wantSource || m.Arch != c.wantArch {
				t.Errorf("got %s %s %s %s", m.TalosVersion, m.Schematic, m.SchematicSource, m.Arch)
			}

			hasWarning := slices.ContainsFunc(m.Warnings, func(w string) bool { return c.wantWarning != "" && strings.Contains(w, c.wantWarning) })
			if (c.wantWarning == "" && len(m.Warnings) > 0) || (c.wantWarning != "" && !hasWarning) {
				t.Errorf("warnings = %q, want %q", m.Warnings, c.wantWarning)
			}

			if !strings.Contains(m.ISO, "/"+c.wantSchematic+"/"+c.wantVersion+"/metal-"+c.wantArch+".iso") ||
				!strings.Contains(m.BooterDocker, "--schematic-id="+c.wantSchematic) || !strings.HasPrefix(m.BooterPodman, "sudo podman ") {
				t.Errorf("media = %+v", m)
			}

			if m.Omni != nil || m.TemplateNode != "worker-1" {
				t.Errorf("omni %v, template %q", m.Omni, m.TemplateNode)
			}
		})
	}
}

func TestNodeBootMediaDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := NodeBootMedia(cfg, "", "", "arm64", true)
	m := decodeJSON[bootMedia](t, out, err)

	if m.Schematic != demoBootSchematic || m.TalosVersion != demoTalosVersion || m.Arch != "arm64" || !m.SecureBoot ||
		!strings.HasSuffix(m.ISO, "metal-arm64-secureboot.iso") || m.Omni != nil || m.TemplateNode == "" {
		t.Errorf("demo = %+v", m)
	}

	for _, key := range []string{`"iso"`, `"ipxe"`, `"kernel"`, `"initramfs"`, `"cmdline"`, `"booterDocker"`, `"omni":null`, `"warnings":[]`} {
		if !strings.Contains(out, key) {
			t.Errorf("JSON lacks %s: %s", key, out)
		}
	}

	if _, err := NodeBootMedia(cfg, "", "", "sparc", false); err == nil {
		t.Error("an unknown architecture was accepted")
	}
}

func TestOmniJoinKernelArgs(t *testing.T) {
	useMemAuthStore(t)

	args := []string{"siderolink.api=https://omni.example.com:8090?jointoken=sample-token", "talos.events.sink=[fdae:41e4:649b:9303::1]:8090", "talos.logging.kernel=tcp://[fdae:41e4:649b:9303::1]:8092"}
	f := &fakeOmniAPI{joinToken: "sample-token-id", joinArgs: args}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	cfgCtx, err := omniInstanceContext(endpoint, testUserIdentity)
	if err != nil {
		t.Fatal(err)
	}

	signer := omniSigner{identity: testUserIdentity, key: f.key}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got, err := omniJoinKernelArgs(ctx, cfgCtx, signer, nil)
	if err != nil || got != strings.Join(args, " ") {
		t.Fatalf("kernel args = %q, %v", got, err)
	}

	f.denyJoin = true

	if _, err := omniJoinKernelArgs(ctx, cfgCtx, signer, nil); err == nil || !strings.Contains(err.Error(), "Operator role") {
		t.Errorf("denied: %v", err)
	}

	f.joinToken = ""

	if _, err := omniJoinKernelArgs(ctx, cfgCtx, signer, nil); err == nil {
		t.Error("no default join token: no error")
	}
}

func TestAddOmniJoinInPrivacyMode(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	s := &session{context: &clientconfig.Context{Auth: clientconfig.Auth{SideroV1: &clientconfig.SideroV1{Identity: testUserIdentity}}}}
	m := bootMedia{Warnings: []string{}}

	addOmniJoin(context.Background(), s, &m, nil)

	if m.Omni != nil || len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "privacy mode") {
		t.Errorf("media = %+v", m)
	}
}
