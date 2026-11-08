package ichorgo

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	azureTestNetBase   = "/subscriptions/" + azureTestSub + "/resourceGroups/talos/providers/Microsoft.Network/"
	azureTestNetNSG    = azureTestNetBase + "networkSecurityGroups/" + azureNetworkName
	azureTestNetVNet   = azureTestNetBase + "virtualNetworks/" + azureNetworkName
	azureTestNetVNets  = azureTestNetBase + "virtualNetworks"
	azureTestNetSpec   = `{"resourceGroup":"talos","location":"westeurope"}`
	azureTestNetOmni   = `{"resourceGroup":"talos","location":"westeurope","omni":true}`
	azureTestPhoneAddr = "203.0.113.7"
)

func azureNetworkPrepared(t *testing.T, spec string) azureNetworkResult {
	t.Helper()

	out, err := AzureNetworkPrepare("", "", azureTestCred, spec)
	if err != nil {
		t.Fatal(err)
	}

	var r azureNetworkResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	return r
}

// The rules a security group PUT asked for, by name.
func azureTestRules(t *testing.T, f *fakeARM) map[string]map[string]any {
	t.Helper()

	body, ok := f.puts[azureTestNetNSG]
	if !ok {
		t.Fatal("no security group written")
	}

	data, _ := json.Marshal(body)

	var g azureNetworkResource
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}

	out := map[string]map[string]any{}
	for _, r := range g.Properties.SecurityRules {
		out[r.Name] = r.Properties
	}

	return out
}

// Plain Talos: the group with the phone's rule only, then the network behind it on 10.240/16.
func TestAzureNetworkPrepareCreates(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{}
	startFakeARM(t, f)
	startFakeCheckIP(t, http.StatusOK, azureTestPhoneAddr)

	r := azureNetworkPrepared(t, azureTestNetSpec)
	if r.NetworkID != azureTestNetVNet || r.SecurityGroupID != azureTestNetNSG || r.SubnetID != azureTestNetVNet+"/subnets/nodes" ||
		r.PhoneIP != azureTestPhoneAddr+"/32" || !slices.Equal(r.Created, []string{"nsg", "vnet"}) || len(r.Reused) != 0 {
		t.Fatalf("result %+v", r)
	}

	if !slices.Equal(f.putOrder, []string{azureTestNetNSG, azureTestNetVNet}) {
		t.Fatalf("writes %v", f.putOrder)
	}

	rules := azureTestRules(t, f)
	phone := rules[azureNetworkPhoneRule]
	if len(rules) != 1 || phone["sourceAddressPrefix"] != azureTestPhoneAddr+"/32" || phone["destinationPortRange"] != "50000" ||
		phone["direction"] != "Inbound" || phone["access"] != "Allow" {
		t.Fatalf("rules %v", rules)
	}

	vnet, _ := json.Marshal(f.puts[azureTestNetVNet])
	for _, want := range []string{`"addressPrefixes":["10.240.0.0/16"]`, `"addressPrefix":"10.240.0.0/24"`, `"id":"` + azureTestNetNSG + `"`, `"ichor-managed":"true"`} {
		if !strings.Contains(string(vnet), want) {
			t.Errorf("network %s lacks %s", vnet, want)
		}
	}

	if entries := auditEntries("", azureNetworkAuditAction); len(entries) != 1 || entries[0].Outcome != auditOK {
		t.Errorf("audit %+v", entries)
	}
}

// Omni: no inbound rule at all, and no address asked.
func TestAzureNetworkPrepareOmni(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{}
	startFakeARM(t, f)
	startFakeCheckIP(t, http.StatusOK, azureTestPhoneAddr)

	if r := azureNetworkPrepared(t, azureTestNetOmni); r.PhoneIP != "" || len(r.Created) != 2 {
		t.Fatalf("result %+v", r)
	}

	if rules := azureTestRules(t, f); len(rules) != 0 {
		t.Fatalf("rules %v", rules)
	}
}

// A second run reuses both, keeps the group's other rules and refreshes only the phone's address.
func TestAzureNetworkPrepareReuses(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{}
	startFakeARM(t, f)
	startFakeCheckIP(t, http.StatusOK, azureTestPhoneAddr)
	azureNetworkPrepared(t, azureTestNetSpec)

	// Someone added a rule meanwhile; the phone moved to another address.
	nsg := f.resources[azureTestNetNSG]
	props, _ := nsg["properties"].(map[string]any)
	props["securityRules"] = append(props["securityRules"].([]any), map[string]any{"name": "ssh-from-office", "properties": map[string]any{"sourceAddressPrefix": "198.51.100.0/24"}})
	startFakeCheckIP(t, http.StatusOK, "203.0.113.8")
	f.putOrder = nil

	r := azureNetworkPrepared(t, azureTestNetSpec)
	if len(r.Created) != 0 || !slices.Equal(r.Reused, []string{"nsg", "vnet"}) || r.PhoneIP != "203.0.113.8/32" {
		t.Fatalf("result %+v", r)
	}

	if !slices.Equal(f.putOrder, []string{azureTestNetNSG}) {
		t.Fatalf("writes %v", f.putOrder)
	}

	rules := azureTestRules(t, f)
	if len(rules) != 2 || rules[azureNetworkPhoneRule]["sourceAddressPrefix"] != "203.0.113.8/32" || rules["ssh-from-office"] == nil {
		t.Fatalf("rules %v", rules)
	}
}

// Without the network writes the prepare is refused before any of them, naming the action.
func TestAzureNetworkPrepareRefused(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{noCreateRight: true}
	startFakeARM(t, f)
	startFakeCheckIP(t, http.StatusOK, azureTestPhoneAddr)

	_, err := AzureNetworkPrepare("", "", azureTestCred, azureTestNetSpec)
	if err == nil || !strings.Contains(err.Error(), "Microsoft.Network/networkSecurityGroups/write") || len(f.putOrder) != 0 {
		t.Fatalf("err %v writes %v", err, f.putOrder)
	}

	for _, bad := range []string{`{"location":"westeurope"}`, `{"resourceGroup":"talos","location":"West Europe"}`, `not json`} {
		if _, err := AzureNetworkPrepare("", "", azureTestCred, bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// The phone's address cannot be learned: no rule, a warning, still a network.
func TestAzureNetworkPrepareNoPhoneAddress(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{}
	startFakeARM(t, f)
	startFakeCheckIP(t, 0, "")

	r := azureNetworkPrepared(t, azureTestNetSpec)
	if r.PhoneIP != "" || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "port 50000") || len(r.Created) != 2 {
		t.Fatalf("result %+v", r)
	}

	if rules := azureTestRules(t, f); len(rules) != 0 {
		t.Fatalf("rules %v", rules)
	}
}

// A network of the group already on 10.240/16 (and one on 10.241.8/24): the next free /16.
func TestAzureNetworkPrepareAddressSpace(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{}
	startFakeARM(t, f)
	startFakeCheckIP(t, http.StatusOK, azureTestPhoneAddr)

	space := func(prefixes ...string) map[string]any {
		return map[string]any{"properties": map[string]any{"addressSpace": map[string]any{"addressPrefixes": prefixes}}}
	}
	f.resources[azureTestNetVNets] = map[string]any{"value": []any{space("10.240.0.0/16"), space("10.241.8.0/24")}}

	azureNetworkPrepared(t, azureTestNetSpec)

	if vnet, _ := json.Marshal(f.puts[azureTestNetVNet]); !strings.Contains(string(vnet), `"addressPrefixes":["10.242.0.0/16"]`) {
		t.Fatalf("network %s", vnet)
	}
}

// The demo answers samples and writes nothing.
func TestAzureNetworkDemo(t *testing.T) {
	withDataDir(t)

	f := &fakeARM{}
	startFakeARM(t, f)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := AzureNetworkPrepare(cfg, "", "", azureTestNetSpec)
	if err != nil || !strings.Contains(out, azureNetworkName) || len(f.calls) != 0 || len(f.putOrder) != 0 {
		t.Fatalf("demo %s %v calls %v", out, err, f.calls)
	}
}

// A picked group open to the world names the rule that opens it.
func TestAzureOptionsNameTheWideOpenRule(t *testing.T) {
	g := azureAPISecurityGroup{}
	if err := json.Unmarshal([]byte(`{"properties":{"securityRules":[
	  {"name":"from-office","properties":{"direction":"Inbound","access":"Allow","sourceAddressPrefix":"198.51.100.0/24","destinationPortRange":"*"}},
	  {"name":"talos-api","properties":{"direction":"Inbound","access":"Allow","sourceAddressPrefixes":["10.0.0.0/8","Internet"],"destinationPortRange":"50000"}}]}}`), &g); err != nil {
		t.Fatal(err)
	}

	if got := g.wideOpenRule(); got != "talos-api ← Internet" || !g.wideOpen() {
		t.Fatalf("rule %q", got)
	}

	if (azureAPISecurityGroup{}).wideOpen() {
		t.Fatal("an empty group is open")
	}
}
