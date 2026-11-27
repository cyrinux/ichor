package ichorgo

import (
	"context"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	talosmeta "github.com/siderolabs/talos/pkg/machinery/meta"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The META list and the VM specs are sensitive on Talos 1.15 and redactable
// (pkg/machinery/resources/runtime/meta_key.go and hypervisor/virtual_machine_spec.go at
// v1.15.0-beta.1); MetaKeys are not sensitive on 1.14. Values are invented.

// redactedPlatformConfig is a metal network config as a 1.15 node redacts it: the WireGuard
// key of the embedded link spec is REDACTED, the addresses stay.
const redactedPlatformConfig = `addresses:
    - address: 203.0.113.9/24
      linkName: eth0
links:
    - name: wg0
      kind: wireguard
      wireguard:
        privateKey: REDACTED
`

// metaReaderNode is a node answering an os:reader. sensitive is the fake's rule for the META
// type (nil: not sensitive, like 1.14); redacted stores the keys as a 1.15 node redacts them.
func metaReaderNode(t *testing.T, node, version string, sensitive map[string]bool, redacted bool) string {
	t.Helper()

	key := func(tag uint8, value string, secret bool) *runtime.MetaKey {
		k := runtime.NewMetaKey(runtime.NamespaceName, runtime.MetaKeyTagToID(tag))
		k.TypedSpec().Value = value

		if secret && redacted {
			k.TypedSpec().Value = redactedPlaceholder
			if tag == talosmeta.MetalNetworkPlatformConfig {
				k.TypedSpec().Value = redactedPlatformConfig
			}

			k.Metadata().Annotations().Set(annotationRedacted, annotationRedactedValue)
		}

		return k
	}

	loaded := runtime.NewMetaLoaded()
	loaded.TypedSpec().Done = true

	f := newFakeTalos()
	f.sensitive = sensitive
	f.addNode(t, node, version, machine.TypeWorker)
	f.put(node,
		key(talosmeta.MetalNetworkPlatformConfig, strings.ReplaceAll(redactedPlatformConfig, "REDACTED", "aW52ZW50ZWQta2V5"), true),
		key(talosmeta.UserReserved1, "rack-3", false),
		key(talosmeta.UniqueMachineToken, "invented-token", true),
		loaded)

	return f.start(t, node)
}

func TestNodeMetaKeysRedacted(t *testing.T) {
	const node = "192.0.2.91"

	redactable := map[string]bool{runtime.MetaKeyType: true}

	for _, tc := range []struct {
		name, version string
		sensitive     map[string]bool
		redacted      bool
		wantErr       string
		wantRedacted  string // the hex of the keys flagged, in order
		wantNetwork   string // a part of the 0x0a value
	}{
		{"1.15 reader: redacted form", "v1.15.0", redactable, true, "", "0x0a 0x10", "privateKey: REDACTED"},
		{"1.14 reader: not sensitive, full values", "v1.14.7", nil, false, "", "", "privateKey: aW52ZW50ZWQta2V5"},
		{"no redacted form: still refused", "v1.15.0", map[string]bool{runtime.MetaKeyType: false}, true, "permission denied", "", ""},
		{"sensitive on a node without redaction: still refused", "v1.14.7", redactable, true, "permission denied", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := metaReaderNode(t, node, tc.version, tc.sensitive, tc.redacted)

			out, err := NodeMetaKeys(cfg, "fake", node)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}

				return
			}

			list := decodeJSON[metaKeyList](t, out, err)

			var flagged []string

			for _, k := range list.Keys {
				if k.Redacted {
					flagged = append(flagged, k.Hex)
				}
			}

			if got := strings.Join(flagged, " "); got != tc.wantRedacted {
				t.Errorf("redacted = %q, want %q: %s", got, tc.wantRedacted, out)
			}

			network, user, token := list.Keys[0], list.Keys[1], list.Keys[2]
			if !strings.Contains(network.Value, tc.wantNetwork) || !network.Writable {
				t.Errorf("0x0a = %+v", network)
			}

			if user.Value != "rack-3" || user.Redacted {
				t.Errorf("0x0c = %+v", user)
			}

			if !token.Secret || token.Value != "" {
				t.Errorf("token = %+v", token)
			}

			if tc.wantRedacted == "" && strings.Contains(out, `"redacted"`) {
				t.Errorf("redacted in the JSON: %s", out)
			}
		})
	}
}

// Without the header the META list is refused on 1.15, as it was before the reader sent it.
func TestMetaKeysPlainListRefused(t *testing.T) {
	const node = "192.0.2.92"

	cfg := metaReaderNode(t, node, "v1.15.0", map[string]bool{runtime.MetaKeyType: true}, true)

	for _, accept := range []bool{false, true} {
		_, err := withNodeSession(cfg, "fake", node, callTimeout, func(ctx context.Context, s *session) (resource.List, error) {
			if accept {
				ctx = acceptRedacted(ctx)
			}

			return s.client.COSI.List(ctx, resource.NewMetadata(runtime.NamespaceName, runtime.MetaKeyType, "", resource.VersionUndefined))
		})

		if want := map[bool]codes.Code{false: codes.PermissionDenied, true: codes.OK}[accept]; status.Code(err) != want {
			t.Errorf("accept=%v: code = %v, want %v (%v)", accept, status.Code(err), want, err)
		}
	}
}

// Screenshot mode still masks a redacted META value.
func TestNodeMetaKeysRedactedMasked(t *testing.T) {
	const node = "192.0.2.93"

	cfg := metaReaderNode(t, node, "v1.15.0", map[string]bool{runtime.MetaKeyType: true}, true)

	enableMask(t, "")

	out, err := NodeMetaKeys(cfg, "fake", node)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "203.0.113.9") || !strings.Contains(out, "privateKey: REDACTED") || !strings.Contains(out, `"redacted":true`) {
		t.Errorf("masked = %s", out)
	}
}

// redactedVMSpecYAML is a VM spec as a 1.15 node redacts it: the cloud-init payloads only.
const redactedVMSpecYAML = vmSpecWebYAML + `cloudInit:
    pool: vm-pool
    userData: REDACTED
`

// vmReaderNode is a hypervisor node answering an os:reader; specSensitive is the fake's rule
// for the VM spec type (nil: none).
func vmReaderNode(t *testing.T, node, version string, specSensitive map[string]bool) string {
	t.Helper()

	spec := untypedResource(t, vmSpecType, "web", redactedVMSpecYAML)
	spec.Metadata().Annotations().Set(annotationRedacted, annotationRedactedValue)

	f := newFakeTalos()
	f.sensitive = specSensitive
	f.addNode(t, node, version, machine.TypeWorker)
	f.put(node,
		resourceDefinition(t, meta.ResourceDefinitionSpec{Type: vmStatusType, DefaultNamespace: hypervisorNamespace}),
		untypedResource(t, vmStatusType, "web", vmStatusRunningYAML),
		spec,
	)

	return f.start(t, node)
}

func TestNodeVirtualMachinesRedactedSpecs(t *testing.T) {
	const node = "192.0.2.94"

	for _, tc := range []struct {
		name, version string
		sensitive     map[string]bool
		wantSpecs     bool
	}{
		{"1.15 reader: redacted specs read", "v1.15.0", map[string]bool{vmSpecType: true}, true},
		{"node refusing the header: specs section error", "v1.14.7", map[string]bool{vmSpecType: true}, false},
		{"no redacted form: specs section error", "v1.15.0", map[string]bool{vmSpecType: false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := vmReaderNode(t, node, tc.version, tc.sensitive)

			out, err := NodeVirtualMachines(cfg, "fake", node)
			report := decodeJSON[nodeVMs](t, out, err)

			if len(report.VMs) != 1 || !report.VMs[0].Running {
				t.Fatalf("vms = %s", out) // the statuses show either way
			}

			vm := report.VMs[0]
			specErr := report.Errors[vmSectionSpecs]

			if tc.wantSpecs {
				if specErr != "" || vm.CPUs != 4 || vm.MemoryBytes != 8589934592 || vm.WantedPowerState != "running" {
					t.Errorf("redacted spec lost: %s", out)
				}

				if strings.Contains(out, redactedPlaceholder) {
					t.Errorf("a redacted cloud-init field reaches the report: %s", out)
				}

				return
			}

			if !strings.Contains(specErr, "permission denied") || vm.CPUs != 0 || vm.WantedPowerState != "" {
				t.Errorf("specs = %q: %s", specErr, out)
			}
		})
	}
}

// Screenshot mode still shows a VM read from a redacted spec as vm-N.
func TestNodeVirtualMachinesRedactedMasked(t *testing.T) {
	const node = "192.0.2.95"

	cfg := vmReaderNode(t, node, "v1.15.0", map[string]bool{vmSpecType: true})

	enableMask(t, "")

	out, err := NodeVirtualMachines(cfg, "fake", node)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, `"web"`) || strings.Contains(out, "198.51.100.7") || !strings.Contains(out, `"cpus":4`) {
		t.Errorf("masked = %s", out)
	}
}
