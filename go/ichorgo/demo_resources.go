package ichorgo

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// The demo resource browser: the node's hostname, and a WireGuard link spec as Talos 1.15
// serves it to an os:reader (private key REDACTED, annotated talos.dev/redacted).
const (
	demoHostnameType = "HostnameStatuses.net.talos.dev"
	demoLinkSpecType = "LinkSpecs.net.talos.dev"
	demoLinkSpecID   = "configuration/wg0"
)

// demoResource answers ResourceTypes, ResourceList (args: type) and ResourceGet (args:
// type, id) for the demo node hostname.
func demoResource(operation, hostname string, now int64, args []string) (string, error) {
	link := len(args) > 0 && demoIsLinkSpec(args[0])

	switch operation {
	case "ResourceTypes":
		return toJSON([]resourceTypeInfo{
			{Type: demoHostnameType, Aliases: []string{"hostname"}, Namespace: "network"},
			{Type: demoLinkSpecType, Aliases: []string{"linkspec"}, Namespace: network.ConfigNamespaceName, Sensitivity: "sensitive"},
		})
	case "ResourceList":
		if link {
			return toJSON(resourceItems{Type: demoLinkSpecType, Namespace: network.ConfigNamespaceName, Items: []resourceItem{
				{ID: demoLinkSpecID, Namespace: network.ConfigNamespaceName, Version: "2", Phase: "running", Updated: now, Redacted: true},
			}})
		}

		return toJSON(resourceItems{Type: demoHostnameType, Namespace: "network", Items: []resourceItem{{ID: "hostname", Namespace: "network", Version: "1", Phase: "running", Updated: now}}})
	case "ResourceGet":
		if link {
			text, err := resourceToYAML(demoLinkSpec())
			if err != nil {
				return "", err
			}

			return toJSON(resourceYAML{YAML: text, Redacted: true})
		}

		return toJSON(resourceYAML{YAML: fmt.Sprintf("metadata:\n  namespace: network\n  type: %s\n  id: hostname\n  version: 1\nspec:\n  hostname: %s\n  domainname: demo.invalid\n", demoHostnameType, hostname)})
	}

	return "", errDemoUnavailable
}

func demoIsLinkSpec(resourceType string) bool {
	return strings.EqualFold(resourceType, demoLinkSpecType) || strings.EqualFold(resourceType, "linkspec") ||
		strings.EqualFold(resourceType, "linkspecs")
}

// demoLinkSpec is the demo WireGuard link spec, its keys redacted like Talos 1.15 does.
func demoLinkSpec() resource.Resource {
	link := network.NewLinkSpec(network.ConfigNamespaceName, demoLinkSpecID)
	link.Metadata().SetVersion(resource.VersionUndefined.Next().Next())
	link.Metadata().Annotations().Set(annotationRedacted, annotationRedactedValue)

	spec := link.TypedSpec()
	spec.Name, spec.Logical, spec.Up, spec.MTU, spec.Kind = "wg0", true, true, 1420, "wireguard"
	spec.Type, spec.ConfigLayer = nethelpers.LinkNone, network.ConfigMachineConfiguration
	spec.Wireguard = network.WireguardSpec{
		PrivateKey: redactedPlaceholder,
		ListenPort: 51820,
		Peers: []network.WireguardPeer{{
			PublicKey:                   "Zm9yLWRlbW8tb25seS1ub3QtYS1yZWFsLWtleS0wMDA=",
			PresharedKey:                redactedPlaceholder,
			Endpoint:                    "198.51.100.20:51820",
			PersistentKeepaliveInterval: 25 * time.Second,
			AllowedIPs:                  []netip.Prefix{netip.MustParsePrefix("10.99.0.0/24")},
		}},
	}

	return link
}
