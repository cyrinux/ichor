package ichorgo

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	gcpTestNetSpec  = `{"project":"example-project","zone":"europe-west1-b","role":"worker"}`
	gcpTestNetOmni  = `{"project":"example-project","zone":"europe-west1-b","role":"worker","omni":true}`
	gcpTestPhone    = "203.0.113.7"
	gcpTestDefault  = gcpTestLink + "/global/networks/default"
	gcpTestDefSubnt = gcpTestLink + "/regions/" + gcpTestRegion + "/subnetworks/default"
)

// fakeGCENetwork is the guided network's side of fakeGCE: the default network and the project's
// firewall rules, their writes and global operations. fakeGCE's lock covers it.
type fakeGCENetwork struct {
	noDefault  bool     // the organisation policy skipped the default network
	listDenied bool     // firewalls.list is refused
	missing    []string // permissions testIamPermissions leaves out

	firewalls map[string]map[string]any
	inserts   []map[string]any
	patches   []map[string]any
}

func newFakeGCENetwork(rules ...map[string]any) *fakeGCENetwork {
	n := &fakeGCENetwork{firewalls: map[string]map[string]any{}}
	for _, r := range rules {
		n.firewalls[r["name"].(string)] = r
	}

	return n
}

// handle answers the calls of the guided network; false leaves the call to fakeGCE.
func (n *fakeGCENetwork) handle(f *fakeGCE, w http.ResponseWriter, r *http.Request, rest string) bool {
	switch {
	case r.Method == http.MethodGet && rest == "/global/networks/default":
		if n.noDefault {
			gceFail(w, http.StatusNotFound, gcpReasonNotFound, "The resource 'projects/"+gcpTestProject+"/global/networks/default' was not found")

			return true
		}

		gceJSON(w, http.StatusOK, map[string]any{
			"name": "default", "selfLink": gcpTestDefault, "autoCreateSubnetworks": true,
			"subnetworks": []string{gcpTestLink + "/regions/us-central1/subnetworks/default", gcpTestDefSubnt},
		})
	case r.Method == http.MethodGet && rest == "/global/firewalls":
		if n.listDenied {
			gceFail(w, http.StatusForbidden, gcpReasonForbidden, "Required 'compute.firewalls.list' permission for 'projects/"+gcpTestProject+"'")

			return true
		}

		items := []any{}
		for _, name := range slices.Sorted(maps.Keys(n.firewalls)) {
			items = append(items, n.firewalls[name])
		}

		gceJSON(w, http.StatusOK, map[string]any{"items": items})
	case r.Method == http.MethodPost && rest == "/global/firewalls":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		n.inserts = append(n.inserts, body)
		name, _ := body["name"].(string)

		if _, ok := n.firewalls[name]; ok {
			gceFail(w, http.StatusConflict, gcpReasonAlreadyExists, "The resource 'projects/"+gcpTestProject+"/global/firewalls/"+name+"' already exists")

			return true
		}

		n.firewalls[name] = body
		gceJSON(w, http.StatusOK, f.operation("firewall", true))
	case r.Method == http.MethodPatch && strings.HasPrefix(rest, "/global/firewalls/"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		name := strings.TrimPrefix(rest, "/global/firewalls/")
		n.patches = append(n.patches, map[string]any{"name": name, "body": body})
		maps.Copy(n.firewalls[name], body)
		gceJSON(w, http.StatusOK, f.operation("firewall", true))
	case r.Method == http.MethodPost && strings.HasPrefix(rest, "/global/operations/") && strings.HasSuffix(rest, "/wait"):
		gceJSON(w, http.StatusOK, f.ops[strings.TrimSuffix(strings.TrimPrefix(rest, "/global/operations/"), "/wait")])
	default:
		return false
	}

	return true
}

// gcpDecoded is decodeJSON on a call's two results.
func gcpDecoded[T any](t *testing.T) func(string, error) T {
	t.Helper()

	return func(out string, err error) T { return decodeJSON[T](t, out, err) }
}

func gceFirewall(name, network string, sources []string, protocol string, ports ...string) map[string]any {
	allowed := map[string]any{"IPProtocol": protocol}
	if len(ports) > 0 {
		allowed["ports"] = ports
	}

	return map[string]any{
		"name": name, "network": gcpTestLink + "/global/networks/" + network, "direction": "INGRESS",
		"sourceRanges": sources, "targetTags": []string{gcpNetworkTag}, "allowed": []any{allowed},
	}
}

func gceInternalRule() map[string]any {
	r := gceFirewall(gcpRuleInternal, "default", nil, "all")
	delete(r, "sourceRanges")
	r["sourceTags"] = []string{gcpNetworkTag}

	return r
}

func TestGCPNetworkPrepare(t *testing.T) {
	cases := []struct {
		name        string
		network     *fakeGCENetwork
		spec        string
		checkStatus int // 0: checkip is down
		wantCreated []string
		wantUpdated []string
		wantPhone   string
		wantSources []string // the phone rule's sources afterwards, nil when there is none
		wantWarning string
		wantError   string
	}{
		{
			name: "plain Talos: the nodes rule and the phone's /32", network: newFakeGCENetwork(), spec: gcpTestNetSpec, checkStatus: http.StatusOK,
			wantCreated: []string{gcpRuleInternal, gcpRulePhone}, wantPhone: gcpTestPhone, wantSources: []string{gcpTestPhone + "/32"},
		},
		{
			name: "Omni: the nodes rule only, no address asked", network: newFakeGCENetwork(), spec: gcpTestNetOmni, checkStatus: http.StatusOK,
			wantCreated: []string{gcpRuleInternal},
		},
		{
			name: "checkip down: a warning, no 50000 rule", network: newFakeGCENetwork(), spec: gcpTestNetSpec,
			wantCreated: []string{gcpRuleInternal}, wantWarning: "could not learn this phone's address: port 50000 is not opened",
		},
		{
			name:    "an older phone rule gets today's /32",
			network: newFakeGCENetwork(gceInternalRule(), gceFirewall(gcpRulePhone, "default", []string{"198.51.100.4/32"}, "tcp", "50000")),
			spec:    gcpTestNetSpec, checkStatus: http.StatusOK,
			wantCreated: []string{}, wantUpdated: []string{gcpRulePhone}, wantPhone: gcpTestPhone,
			wantSources: []string{"198.51.100.4/32", gcpTestPhone + "/32"},
		},
		{
			name: "no default network: the console steps", network: &fakeGCENetwork{noDefault: true}, spec: gcpTestNetSpec, checkStatus: http.StatusOK,
			wantError: "VPC network → Create VPC network → automatic subnets",
		},
		{
			name: "a missing permission is refused before any write, named", spec: gcpTestNetSpec, checkStatus: http.StatusOK,
			network:   &fakeGCENetwork{firewalls: map[string]map[string]any{}, missing: []string{"compute.firewalls.create", "compute.networks.updatePolicy"}},
			wantError: "may not use compute.firewalls.create, compute.networks.updatePolicy in project example-project",
		},
		{name: "a role that is not one", network: newFakeGCENetwork(), spec: `{"zone":"europe-west1-b","role":"boss"}`, wantError: "not a node role"},
		{name: "a zone that is not one", network: newFakeGCENetwork(), spec: `{"zone":"Europe","role":"worker"}`, wantError: "not a Compute Engine zone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDataDir(t)

			f := &fakeGCE{network: tc.network}
			startFakeGCE(t, f)
			startFakeCheckIP(t, tc.checkStatus, gcpTestPhone)

			out, err := GCPNetworkPrepare("", "", gcpTestCred(t), tc.spec)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) || len(tc.network.inserts)+len(tc.network.patches) != 0 {
					t.Fatalf("err %v, inserts %v", err, tc.network.inserts)
				}

				return
			}

			r := decodeJSON[gcpNetworkResult](t, out, err)
			if r.Network != "default" || r.NetworkLink != gcpTestDefault || r.Subnetwork != "regions/"+gcpTestRegion+"/subnetworks/default" ||
				r.SubnetworkLink != gcpTestDefSubnt || r.PhoneIP != tc.wantPhone || !slices.Equal(r.Created, tc.wantCreated) ||
				!slices.Equal(r.Updated, tc.wantUpdated) {
				t.Fatalf("result %+v", r)
			}

			if (tc.wantWarning == "" && len(r.Warnings) != 0) ||
				(tc.wantWarning != "" && (len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], tc.wantWarning))) {
				t.Fatalf("warnings %v", r.Warnings)
			}

			gcpCheckNetworkRules(t, tc.network, r, tc.wantSources)

			if entries := auditEntries("", gcpNetworkAuditAction); len(entries) != 1 || entries[0].Outcome != auditOK {
				t.Errorf("audit %+v", entries)
			}
		})
	}
}

// gcpCheckNetworkRules: nothing written opens the world or 6443; the nodes rule goes from the tag
// to the tag; the phone rule (when wanted) is TCP 50000 from wantSources, dated.
func gcpCheckNetworkRules(t *testing.T, n *fakeGCENetwork, r gcpNetworkResult, wantSources []string) {
	t.Helper()

	written, _ := json.Marshal([]any{n.inserts, n.patches})
	for _, banned := range []string{"0.0.0.0/0", "6443", "::/0"} {
		if strings.Contains(string(written), banned) {
			t.Fatalf("a write carries %s: %s", banned, written)
		}
	}

	internal := n.firewalls[gcpRuleInternal]
	if s, _ := json.Marshal(internal); !strings.Contains(string(s), `"sourceTags":["ichor-node"]`) || !strings.Contains(string(s), `"targetTags":["ichor-node"]`) ||
		!strings.Contains(string(s), `"IPProtocol":"all"`) || strings.Contains(string(s), "sourceRanges") {
		t.Fatalf("nodes rule %s", s)
	}

	phone, ok := n.firewalls[gcpRulePhone]
	if wantSources == nil {
		if ok {
			t.Fatalf("a phone rule %v", phone)
		}

		return
	}

	s, _ := json.Marshal(phone)
	sources, _ := json.Marshal(wantSources)

	for _, want := range []string{`"sourceRanges":` + string(sources), `"ports":["50000"]`, `"IPProtocol":"tcp"`, `"description":"Ichor phone 2026-11-07"`} {
		if !strings.Contains(string(s), want) {
			t.Fatalf("phone rule %s lacks %s", s, want)
		}
	}

	if !slices.ContainsFunc(r.Rules, func(x gcpNetworkRule) bool { return x.Name == gcpRuleInternal && x.Self && x.Protocol == "all" }) ||
		!slices.ContainsFunc(r.Rules, func(x gcpNetworkRule) bool { return x.Source == gcpTestPhone+"/32" && x.Ports == "50000" }) {
		t.Fatalf("rules %+v", r.Rules)
	}
}

// A second call makes nothing: the same rules, no write.
func TestGCPNetworkPrepareTwice(t *testing.T) {
	withDataDir(t)

	n := newFakeGCENetwork()
	f := &fakeGCE{network: n}
	startFakeGCE(t, f)
	startFakeCheckIP(t, http.StatusOK, gcpTestPhone)

	first := gcpDecoded[gcpNetworkResult](t)(GCPNetworkPrepare("", "", gcpTestCred(t), gcpTestNetSpec))
	writes := len(n.inserts) + len(n.patches)

	second := gcpDecoded[gcpNetworkResult](t)(GCPNetworkPrepare("", "", gcpTestCred(t), gcpTestNetSpec))
	if len(second.Created) != 0 || len(second.Updated) != 0 || len(n.inserts)+len(n.patches) != writes || writes != 2 {
		t.Fatalf("second %+v after %d writes, now %d", second, writes, len(n.inserts)+len(n.patches))
	}

	if len(second.Rules) != len(first.Rules) || len(second.Rules) != 2 {
		t.Fatalf("rules %+v then %+v", first.Rules, second.Rules)
	}
}

// The demo answers samples and calls nothing.
func TestGCPNetworkDemo(t *testing.T) {
	withDataDir(t)

	f := &fakeGCE{network: newFakeGCENetwork()}
	startFakeGCE(t, f)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	r := gcpDecoded[gcpNetworkResult](t)(GCPNetworkPrepare(cfg, "", "", gcpTestNetSpec))
	if r.Network != "default" || len(r.Rules) != 2 || len(r.Created) != 0 || len(f.calls) != 0 || f.tokens != 0 {
		t.Fatalf("demo %+v, calls %v", r, f.calls)
	}
}

// The options flag a network a rule opens to the world, and the one the guided network prepared;
// rules that cannot be read are a warning and no flag.
func TestGCPOptionsFlagTheFirewalls(t *testing.T) {
	icmp := gceFirewall("default-allow-icmp", "default", []string{"0.0.0.0/0"}, "icmp")
	delete(icmp, "targetTags")
	ssh := gceFirewall("default-allow-ssh", "default", []string{"0.0.0.0/0"}, "tcp", "22")
	elsewhere := gceFirewall("web-only", "default", []string{"0.0.0.0/0"}, "all")
	elsewhere["targetTags"] = []string{"web"}
	open := gceFirewall("talos-controlplane-talosctl", "empty", []string{"0.0.0.0/0"}, "tcp", "40000-60000")

	f := &fakeGCE{network: newFakeGCENetwork(gceInternalRule(), icmp, ssh, elsewhere, open)}
	startFakeGCE(t, f)

	o := gcpDecoded[gcpOptions](t)(GCPOptions("", "", gcpTestCred(t)))
	flags := map[string][2]bool{}

	for _, n := range o.Networks {
		flags[n.Name] = [2]bool{n.WideOpen, n.Managed}
	}

	if flags["default"] != [2]bool{false, true} || flags["empty"] != [2]bool{true, false} {
		t.Fatalf("flags %v", flags)
	}

	f.network.listDenied = true

	o = gcpDecoded[gcpOptions](t)(GCPOptions("", "", gcpTestCred(t)))
	if slices.ContainsFunc(o.Networks, func(n gcpNetwork) bool { return n.WideOpen || n.Managed }) ||
		!slices.ContainsFunc(o.Warnings, func(w string) bool { return strings.Contains(w, "firewall rules cannot be read") }) {
		t.Fatalf("options %+v", o)
	}
}

func TestGCPPortsCover(t *testing.T) {
	cases := map[string]bool{"": true, "50000": true, "22": false, "40000-60000": true, "1-49999": false, "22,50000": true}

	for ports, want := range cases {
		list := []string{}
		if ports != "" {
			list = strings.Split(ports, ",")
		}

		if got := gcpPortsCover(list, 50000); got != want {
			t.Errorf("%q = %v", ports, got)
		}
	}

	for _, bad := range []string{"0.0.0.0", "10.0.0.1", "not an address", "2001:db8::1"} {
		if _, err := gcpPhoneCIDR(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestGCPNetworkPermissionsOfItsCalls(t *testing.T) {
	cases := map[string]string{
		"GET " + gcpTestBase + "/global/networks/default":            "compute.networks.get",
		"GET " + gcpTestBase + "/global/firewalls":                   "compute.firewalls.list",
		"POST " + gcpTestBase + "/global/firewalls":                  "compute.firewalls.create",
		"PATCH " + gcpTestBase + "/global/firewalls/" + gcpRulePhone: "compute.firewalls.update",
		"POST " + gcpTestBase + "/global/operations/firewall-1/wait": "compute.globalOperations.get",
	}

	for call, want := range cases {
		method, path, _ := strings.Cut(call, " ")
		if got := gcpPermissionOf(method, gcpComputeEndpoint+path); got != want || !slices.Contains(gcpRolePermissions(), got) {
			t.Errorf("%s = %q, want %q in the role", call, got, want)
		}
	}

	if !slices.Contains(gcpRolePermissions(), "compute.networks.updatePolicy") ||
		!strings.Contains(GCPRoleDefinition(""), "- compute.firewalls.update\n") {
		t.Error("the role lacks the firewall permissions")
	}
}
