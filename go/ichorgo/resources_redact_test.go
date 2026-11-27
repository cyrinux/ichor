package ichorgo

import (
	"context"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// redactedWireguardLink is a WireGuard link spec as a Talos 1.15 node sends it to os:reader.
func redactedWireguardLink() *network.LinkSpec {
	link := network.NewLinkSpec(network.ConfigNamespaceName, "configuration/wg0")
	link.TypedSpec().Name = "wg0"
	link.TypedSpec().Kind = "wireguard"
	link.TypedSpec().Wireguard.PrivateKey = "REDACTED"
	link.TypedSpec().Wireguard.ListenPort = 51820
	link.Metadata().Annotations().Set(annotationRedacted, annotationRedactedValue)

	return link
}

// readerNode is a node that answers like it does to an os:reader: the link spec type is
// redactable, the machine config is not.
func readerNode(t *testing.T, node, version string) string {
	t.Helper()

	f := newFakeTalos()
	f.sensitive = map[string]bool{network.LinkSpecType: true, config.MachineConfigType: false}
	f.addNode(t, node, version, machine.TypeWorker)
	f.put(node,
		resourceDefinition(t, network.NewLinkSpec(network.ConfigNamespaceName, "x").ResourceDefinition()),
		resourceDefinition(t, (&config.MachineConfig{}).ResourceDefinition()),
		redactedWireguardLink(),
	)
	f.putMachineConfig(t, node)

	return f.start(t, node)
}

// A plain read of a sensitive type is refused; one that accepts redaction is served the
// redacted form on Talos 1.15, and still refused on 1.14 or for a type with no such form.
func TestRedactedReadFake(t *testing.T) {
	const node = "192.0.2.71"

	for _, tc := range []struct {
		name, version, typ, id string
		accept                 bool
		want                   codes.Code
	}{
		{"plain read", "v1.15.0", network.LinkSpecType, "configuration/wg0", false, codes.PermissionDenied},
		{"redacted read", "v1.15.0", network.LinkSpecType, "configuration/wg0", true, codes.OK},
		{"redacted read on 1.14", "v1.14.7", network.LinkSpecType, "configuration/wg0", true, codes.PermissionDenied},
		{"not redactable", "v1.15.0", config.MachineConfigType, config.ActiveID, true, codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := readerNode(t, node, tc.version)

			r, err := withNodeSession(cfg, "fake", node, callTimeout, func(ctx context.Context, s *session) (resource.Resource, error) {
				if tc.accept {
					ctx = acceptRedacted(ctx)
				}

				ns := network.ConfigNamespaceName
				if tc.typ == config.MachineConfigType {
					ns = config.NamespaceName
				}

				return s.client.COSI.Get(ctx, resource.NewMetadata(ns, tc.typ, tc.id, resource.VersionUndefined))
			})

			if got := status.Code(err); got != tc.want {
				t.Fatalf("code = %v, want %v (%v)", got, tc.want, err)
			}

			if tc.want == codes.OK && !isRedacted(r) {
				t.Errorf("annotation lost: %v", r.Metadata().Annotations().Raw())
			}
		})
	}
}

func TestResourceRedactedFake(t *testing.T) {
	const node = "192.0.2.72"

	cfg := readerNode(t, node, "v1.15.0")

	out, err := ResourceList(cfg, "fake", node, "", "LinkSpecs.net.talos.dev")
	items := decodeJSON[resourceItems](t, out, err)

	if len(items.Items) != 1 || !items.Items[0].Redacted || !strings.Contains(out, `"redacted":true`) {
		t.Errorf("list = %s", out)
	}

	out, err = ResourceGet(cfg, "fake", node, "", network.LinkSpecType, "configuration/wg0")
	y := decodeJSON[resourceYAML](t, out, err)

	if !y.Redacted || !strings.Contains(y.YAML, "privateKey: REDACTED") || !strings.Contains(y.YAML, "talos.dev/redacted") {
		t.Errorf("get = %s", out)
	}

	// No redacted form: still the permission error.
	if _, err := ResourceGet(cfg, "fake", node, "", config.MachineConfigType, config.ActiveID); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("machine config: %v", err)
	}

	// The same read on 1.14 is refused as before.
	old := readerNode(t, "192.0.2.73", "v1.14.7")
	if _, err := ResourceGet(old, "fake", "192.0.2.73", "", network.LinkSpecType, "configuration/wg0"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("1.14: %v", err)
	}
}

func TestResourceNotRedactedShape(t *testing.T) {
	// Without the annotation nothing changes in the JSON.
	link := redactedWireguardLink()
	link.Metadata().Annotations().Delete(annotationRedacted)

	if isRedacted(link) {
		t.Error("not annotated")
	}

	if out, _ := toJSON(resourceYAML{YAML: "a: 1\n"}); out != `{"yaml":"a: 1\n"}` { //nolint:errcheck
		t.Errorf("shape: %s", out)
	}
}

func TestDemoRedactedResource(t *testing.T) {
	cfg := demoConfigForTest(t)
	node := demoNodes()[0].Node

	out, err := ResourceList(cfg, "", node, "", "LinkSpecs.net.talos.dev")
	if items := decodeJSON[resourceItems](t, out, err); len(items.Items) != 1 || !items.Items[0].Redacted {
		t.Errorf("list = %s", out)
	}

	out, err = ResourceGet(cfg, "", node, "", "linkspec", "configuration/wg0")
	y := decodeJSON[resourceYAML](t, out, err)

	if !y.Redacted || !strings.Contains(y.YAML, "privateKey: REDACTED") || !strings.Contains(y.YAML, "id: configuration/wg0") {
		t.Errorf("get = %s", y.YAML)
	}

	t.Log(y.YAML)

	// The hostname sample is not redacted.
	out, err = ResourceGet(cfg, "", node, "", "hostname", "hostname")
	if y := decodeJSON[resourceYAML](t, out, err); y.Redacted || strings.Contains(out, "redacted") {
		t.Errorf("hostname = %s", out)
	}
}
