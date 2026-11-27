package ichorgo

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The fixtures follow LLDPNeighborStatusSpec's yaml tags at Talos v1.15.0-beta.1:
// pkg/machinery/resources/network/lldp_neighbor_status.go.
const lldpEno1YAML = `neighbors:
    - chassisID: 02:00:5e:00:53:aa
      systemName: sw-b.example.test
      systemDescription: "Example switch OS\n7.1"
      portID: Ethernet12
      portDescription: rack 3 uplink
      managementAddresses:
        - 192.0.2.10
        - 2001:db8::10
      vlans:
        - id: 40
          name: storage
        - id: 30
          name: servers
    - chassisID: 02:00:5e:00:53:bb
      systemName: sw-a.example.test
      portID: "7"
`

const lldpEno2YAML = `neighbors:
    - chassisID: 02:00:5e:00:53:cc
      portID: ge-0/0/1
`

func lldpSpec(t *testing.T, id, yamlSpec string) map[string]any {
	t.Helper()

	spec, err := resourceSpecMap(untypedResource(t, lldpNeighborStatusType, id, yamlSpec))
	if err != nil {
		t.Fatal(err)
	}

	return spec
}

func TestMapLLDPNeighbors(t *testing.T) {
	got := mapLLDPNeighbors(map[string]map[string]any{
		"eno2": lldpSpec(t, "eno2", lldpEno2YAML),
		"eno1": lldpSpec(t, "eno1", lldpEno1YAML),
		"eno3": lldpSpec(t, "eno3", "{}\n"),
	})

	want := []lldpNeighbor{
		{
			Link: "eno1", ChassisID: "02:00:5e:00:53:bb", SystemName: "sw-a.example.test", PortID: "7",
			ManagementAddresses: []string{}, VLANs: []lldpVLAN{},
		},
		{
			Link: "eno1", ChassisID: "02:00:5e:00:53:aa", SystemName: "sw-b.example.test",
			SystemDescription: "Example switch OS 7.1", PortID: "Ethernet12", PortDescription: "rack 3 uplink",
			ManagementAddresses: []string{"192.0.2.10", "2001:db8::10"},
			VLANs:               []lldpVLAN{{ID: 30, Name: "servers"}, {ID: 40, Name: "storage"}},
		},
		{Link: "eno2", ChassisID: "02:00:5e:00:53:cc", PortID: "ge-0/0/1", ManagementAddresses: []string{}, VLANs: []lldpVLAN{}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestMapLLDPNeighborsCapsUntrustedInput(t *testing.T) {
	var b strings.Builder

	b.WriteString("neighbors:\n")

	for range lldpNeighborsMax + 3 {
		b.WriteString("    - chassisID: x\n      portID: \"" + strings.Repeat("p", 300) + "\\u0007\"\n      vlans:\n")

		for i := range lldpVLANsMax + 5 {
			b.WriteString("        - id: " + strconv.Itoa(i+1) + "\n")
		}
	}

	got := mapLLDPNeighbors(map[string]map[string]any{"eth0": lldpSpec(t, "eth0", b.String())})
	if len(got) != lldpNeighborsMax {
		t.Fatalf("%d neighbours, want %d", len(got), lldpNeighborsMax)
	}

	n := got[0]
	if r := []rune(n.PortID); len(r) != lldpTextMax || !strings.HasSuffix(n.PortID, "…") {
		t.Errorf("port id not clipped: %d runes", len(r))
	}

	if strings.ContainsRune(n.PortID, '\a') {
		t.Error("control character kept")
	}

	if len(n.VLANs) != lldpVLANsMax {
		t.Errorf("%d VLANs, want %d", len(n.VLANs), lldpVLANsMax)
	}
}

func TestNetworkJSONShape(t *testing.T) {
	b, err := json.Marshal(nodeNetwork{Neighbors: demoNeighbors(), NeighborsSupported: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		`"neighborsSupported":true`, `"link":"eth0"`, `"chassisId"`, `"systemName":"sw-rack-a"`, `"systemDescription"`,
		`"portId":"Ethernet12"`, `"portDescription"`, `"managementAddresses":["192.0.2.250"]`, `"vlans":[{"id":30,"name":"servers"}]`,
	} {
		if !strings.Contains(string(b), key) {
			t.Errorf("%s missing from %s", key, b)
		}
	}
}

func TestSwitchNamesAreMasked(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	learnSwitchNames([]lldpNeighbor{{SystemName: "core-sw7.example.test"}})

	got := privacy.mask(`{"systemName":"core-sw7.example.test","portDescription":"to core-sw7"}`)
	if strings.Contains(got, "core-sw7") || !strings.Contains(got, "switch-1") {
		t.Errorf("switch name not masked: %s", got)
	}
}
