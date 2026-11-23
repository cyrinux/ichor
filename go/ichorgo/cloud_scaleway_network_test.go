package ichorgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

const (
	scwTestGroup   = "sg-nodes"
	scwTestPhoneIP = "203.0.113.7"
	scwTestCP      = "198.51.100.10"
	scwTestWorker  = "198.51.100.11"
)

// fakeScwNetwork is the security group part of Scaleway's API, answered through fakeScaleway's
// extra hook. rules are "PROTOCOL:SOURCE[:PORT]"; every write is recorded.
type fakeScwNetwork struct {
	exists    bool
	foreign   bool   // the group carries no ichor-managed tag
	policy    string // its inbound default policy, "drop" when ""
	rawRanges bool   // the API answers ranges without a mask
	forbidden bool   // every write answers 403
	rules     []string

	creates []map[string]any
	adds    []map[string]any
}

func (n *fakeScwNetwork) handle(w http.ResponseWriter, r *http.Request) bool {
	groups, rules := scwZonePath+"/security_groups", scwZonePath+"/security_groups/"+scwTestGroup+"/rules"

	var body map[string]any
	if r.Method == http.MethodPost {
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)

		if n.forbidden {
			scwFail(w, http.StatusForbidden, "insufficient permissions")

			return true
		}
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == groups && r.URL.Query().Get("name") == scwNetworkGroup:
		if r.URL.Query().Get("project") != scwTestProject {
			scwFail(w, http.StatusBadRequest, "no project")

			return true
		}

		list := `{"id":"sg-other","name":"ichor-nodes-old","tags":["ichor-managed"]}`
		if n.exists {
			list += "," + n.groupJSON()
		}

		fmt.Fprintf(w, `{"security_groups":[%s]}`, list)
	case r.Method == http.MethodPost && r.URL.Path == groups:
		n.creates = append(n.creates, body)
		n.exists = true
		fmt.Fprintf(w, `{"security_group":%s}`, n.groupJSON())
	case r.Method == http.MethodGet && r.URL.Path == rules:
		n.rulesJSON(w)
	case r.Method == http.MethodPost && r.URL.Path == rules:
		n.adds = append(n.adds, body)

		rule := fmt.Sprintf("%s:%s", body["protocol"], body["ip_range"])
		if port, ok := body["dest_port_from"].(float64); ok {
			rule += fmt.Sprintf(":%d", int(port))
		}

		n.rules = append(n.rules, rule)
		fmt.Fprint(w, `{"rule":{}}`)
	default:
		return false
	}

	return true
}

func (n *fakeScwNetwork) groupJSON() string {
	tags, policy := `["ichor-managed"]`, n.policy
	if n.foreign {
		tags = `[]`
	}

	if policy == "" {
		policy = "drop"
	}

	return fmt.Sprintf(`{"id":%q,"name":%q,"project":%q,"tags":%s,"inbound_default_policy":%q}`, scwTestGroup, scwNetworkGroup, scwTestProject, tags, policy)
}

func (n *fakeScwNetwork) rulesJSON(w io.Writer) {
	// Scaleway's own outbound rules of a group are listed too: never shown.
	out := []string{`{"protocol":"TCP","direction":"outbound","action":"drop","ip_range":"0.0.0.0/0","dest_port_from":25}`}

	for _, r := range n.rules {
		parts := strings.Split(r, ":")
		source := parts[1]

		if n.rawRanges {
			source = strings.TrimSuffix(source, "/32")
		}

		rule := fmt.Sprintf(`{"protocol":%q,"direction":"inbound","action":"accept","ip_range":%q`, parts[0], source)
		if len(parts) == 3 {
			rule += `,"dest_port_from":` + parts[2] + `,"dest_port_to":` + parts[2]
		}

		out = append(out, rule+"}")
	}

	fmt.Fprintf(w, `{"rules":[%s]}`, strings.Join(out, ","))
}

// scwTestMembers are a control plane with a private and a public address, and a worker with a
// public one.
func scwTestMembers() []clusterMember {
	return []clusterMember{
		{hostname: "cp-1", role: "controlplane", addresses: []netip.Addr{netip.MustParseAddr("10.0.0.10"), netip.MustParseAddr(scwTestCP)}},
		{hostname: "worker-1", role: "worker", addresses: []netip.Addr{netip.MustParseAddr(scwTestWorker), netip.MustParseAddr("2001:db8::11")}},
	}
}

func startFakeScwNetwork(t *testing.T, n *fakeScwNetwork, members []clusterMember, membersErr error) {
	t.Helper()

	startFakeScaleway(t, &fakeScaleway{extra: n.handle})

	saved := scwNetworkMembers
	scwNetworkMembers = func(string, string) ([]clusterMember, error) { return members, membersErr }

	t.Cleanup(func() { scwNetworkMembers = saved })
}

func TestScalewayNetworkPrepare(t *testing.T) {
	full := []string{"ANY:" + scwTestCP + "/32", "ANY:" + scwTestWorker + "/32", "TCP:" + scwTestPhoneIP + "/32:50000"}
	wantFull := []string{"all:0:" + scwTestCP + "/32:cp-1", "all:0:" + scwTestWorker + "/32:worker-1", "tcp:50000:" + scwTestPhoneIP + "/32:this phone"}
	membersOnly := wantFull[:2]
	private := []clusterMember{{hostname: "worker-2", addresses: []netip.Addr{netip.MustParseAddr("10.0.0.12")}}}

	cases := []struct {
		name        string
		network     fakeScwNetwork
		members     []clusterMember
		membersErr  error
		spec        string
		checkStatus int
		wantErr     string
		wantCreated bool
		wantRules   []string
		wantWarn    string
		wantAdds    int
	}{
		{
			name: "plain Talos: the group, the members and the phone on 50000", spec: `{"role":"controlplane"}`,
			checkStatus: http.StatusOK, wantCreated: true, wantRules: wantFull, wantAdds: 3,
		},
		{
			name: "second call: reused, nothing written", spec: `{"role":"worker"}`,
			network:     fakeScwNetwork{exists: true, rules: full},
			checkStatus: http.StatusOK, wantRules: wantFull,
		},
		{
			name: "ranges answered without a mask still match", spec: `{"role":"worker"}`,
			network:     fakeScwNetwork{exists: true, rules: full, rawRanges: true},
			checkStatus: http.StatusOK, wantRules: wantFull,
		},
		{
			name: "a new member: only its rule is added", spec: `{"role":"worker","omni":true}`,
			network:   fakeScwNetwork{exists: true, rules: []string{"ANY:" + scwTestCP + "/32"}},
			wantRules: membersOnly, wantAdds: 1,
		},
		{
			name: "Omni: no port 50000", spec: `{"role":"worker","omni":true}`,
			checkStatus: http.StatusOK, wantCreated: true, wantRules: membersOnly, wantAdds: 2,
		},
		{
			name: "the phone's address unknown: a warning, never a wider rule", spec: `{"role":"worker"}`,
			wantCreated: true, wantRules: membersOnly, wantAdds: 2, wantWarn: "could not learn this phone's address",
		},
		{
			name: "a member with only a private address: a warning", spec: `{"role":"worker","omni":true}`,
			members:     append(scwTestMembers(), private...),
			wantCreated: true, wantRules: membersOnly, wantAdds: 2, wantWarn: "the member worker-2 has no public IPv4 (only 10.0.0.12)",
		},
		{
			name: "an existing group open by default: left as it is, said so", spec: `{"role":"worker"}`,
			network:     fakeScwNetwork{exists: true, policy: "accept", rules: full},
			checkStatus: http.StatusOK, wantRules: wantFull, wantWarn: "lets in by default",
		},
		{
			name: "someone else's ichor-nodes: used, never changed", spec: `{"role":"worker"}`,
			network:     fakeScwNetwork{exists: true, foreign: true},
			checkStatus: http.StatusOK, wantRules: []string{}, wantWarn: "Ichor did not make",
		},
		{
			name: "a key without the right: the policy sentence", spec: `{"role":"worker"}`,
			network: fakeScwNetwork{forbidden: true}, checkStatus: http.StatusOK,
			wantErr: errScwNoNetwork.Error(),
		},
		{
			name: "no member with a public address: refused before any write", spec: `{"role":"worker"}`,
			members: private, wantErr: "no member of the cluster has a public IPv4",
		},
		{
			name: "the members cannot be listed: refused before any write", spec: `{"role":"worker"}`,
			membersErr: errNoMembers, wantErr: "could not list the cluster's members",
		},
		{name: "a role is required", spec: `{"role":"etcd"}`, wantErr: "is not a node role"},
		{name: "a zone is checked", spec: `{"role":"worker","zone":"../x"}`, wantErr: "unknown Scaleway zone"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			network, members := c.network, c.members
			if members == nil {
				members = scwTestMembers()
			}

			startFakeScwNetwork(t, &network, members, c.membersErr)
			startFakeCheckIP(t, c.checkStatus, scwTestPhoneIP+"\n")

			out, err := ScalewayNetworkPrepare("", "", scwTestCred, c.spec)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}

				if len(network.creates)+len(network.adds) != 0 && !network.forbidden {
					t.Errorf("a refused call wrote: creates %v, adds %v", network.creates, network.adds)
				}

				return
			}

			r := decodeJSON[scwNetworkResult](t, out, err)
			if r.SecurityGroupID != scwTestGroup || r.Name != scwNetworkGroup || r.Created != c.wantCreated {
				t.Errorf("result = %+v", r)
			}

			if got := scwTestRules(r.Rules); !slices.Equal(got, c.wantRules) {
				t.Errorf("rules = %v, want %v", got, c.wantRules)
			}

			wantIP := ""
			if slices.ContainsFunc(c.wantRules, func(s string) bool { return strings.HasPrefix(s, "tcp:") }) && !c.network.foreign {
				wantIP = scwTestPhoneIP
			}

			if r.PhoneIP != wantIP {
				t.Errorf("phoneIp = %q, want %q", r.PhoneIP, wantIP)
			}

			warned := slices.ContainsFunc(r.Warnings, func(w string) bool { return c.wantWarn != "" && strings.Contains(w, c.wantWarn) })
			if warned != (c.wantWarn != "") || (c.wantWarn == "" && len(r.Warnings) != 0) {
				t.Errorf("warnings = %v, want %q", r.Warnings, c.wantWarn)
			}

			if len(network.adds) != c.wantAdds {
				t.Errorf("%d rules added, want %d: %v", len(network.adds), c.wantAdds, network.adds)
			}

			scwCheckNetworkWrites(t, &network)
		})
	}
}

func scwTestRules(rules []scwNetworkRule) []string {
	out := []string{}

	for _, r := range rules {
		out = append(out, fmt.Sprintf("%s:%d:%s:%s", r.Protocol, r.Port, r.Source, r.Description))
	}

	return out
}

// scwCheckNetworkWrites checks what the app sent: the group is tagged, stateful, drops inbound and
// is not the project's default; every rule is an inbound accept from a /32, never the world or 6443.
func scwCheckNetworkWrites(t *testing.T, n *fakeScwNetwork) {
	t.Helper()

	for _, c := range n.creates {
		tags, _ := c["tags"].([]any)
		if c["name"] != scwNetworkGroup || c["project"] != scwTestProject || len(tags) != 1 || tags[0] != scwTagManaged ||
			c["stateful"] != true || c["project_default"] != false || c["inbound_default_policy"] != "drop" || c["outbound_default_policy"] != "accept" {
			t.Errorf("security group create = %v", c)
		}
	}

	for _, a := range n.adds {
		source, _ := a["ip_range"].(string)
		if a["direction"] != "inbound" || a["action"] != "accept" || !strings.HasSuffix(source, "/32") || strings.HasPrefix(source, "0.0.0.0") {
			t.Errorf("rule = %v", a)
		}

		switch a["protocol"] {
		case "ANY":
			if a["dest_port_from"] != nil || source == scwTestPhoneIP+"/32" {
				t.Errorf("member rule = %v", a)
			}
		case "TCP":
			if a["dest_port_from"] != float64(50000) || a["dest_port_to"] != float64(50000) || source != scwTestPhoneIP+"/32" {
				t.Errorf("phone rule = %v", a)
			}
		default:
			t.Errorf("rule protocol = %v", a)
		}
	}
}

func TestScalewayNetworkPrepareRecorded(t *testing.T) {
	withDataDir(t)

	startFakeScwNetwork(t, &fakeScwNetwork{}, scwTestMembers(), nil)
	startFakeCheckIP(t, http.StatusOK, scwTestPhoneIP)

	if _, err := ScalewayNetworkPrepare("", "", scwTestCred, `{"role":"worker"}`); err != nil {
		t.Fatal(err)
	}

	entries := auditEntries("", scwNetworkAuditAction)
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "group="+scwTestGroup) ||
		!strings.Contains(entries[0].Params, "zone=fr-par-1") || !strings.Contains(entries[0].Params, "created=true") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestScalewayNetworkDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	network := fakeScwNetwork{}
	startFakeScwNetwork(t, &network, scwTestMembers(), nil)

	if _, err := ScalewayNetworkPrepare(cfg, "", scwTestCred, `{"role":"worker"}`); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Errorf("demo = %v", err)
	}

	if len(network.creates)+len(network.adds) != 0 {
		t.Errorf("the demo wrote: %v %v", network.creates, network.adds)
	}
}

func TestScalewayOptionsManagedGroup(t *testing.T) {
	withDataDir(t)

	startFakeScaleway(t, &fakeScaleway{extra: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != scwZonePath+"/security_groups" {
			return false
		}

		fmt.Fprint(w, `{"security_groups":[{"id":"sg-1","name":"Default security group","project_default":true},`+
			`{"id":"sg-nodes","name":"ichor-nodes","tags":["ichor-managed"]}]}`)

		return true
	}})

	out, err := ScalewayOptions("", "", scwTestCred)
	o := decodeJSON[scwOptions](t, out, err)

	managed := map[string]bool{}
	for _, g := range o.SecurityGroups {
		managed[g.ID] = g.Managed
	}

	if !managed["sg-nodes"] || managed["sg-1"] || len(managed) != 2 {
		t.Errorf("groups = %+v", o.SecurityGroups)
	}

	if o.GroupWarning != scwGroupWarning || slices.Contains(o.Warnings, scwGroupWarning) {
		t.Errorf("groupWarning = %q, warnings = %v", o.GroupWarning, o.Warnings)
	}
}

func TestScwRuleCovers(t *testing.T) {
	port := func(p uint32) *uint32 { return &p }

	cases := []struct {
		rule     scwAPIRule
		protocol string
		cidr     string
		port     int
		want     bool
	}{
		{scwAPIRule{Protocol: "ANY", Direction: "inbound", Action: "accept", IPRange: "198.51.100.10"}, "ANY", "198.51.100.10/32", 0, true},
		{scwAPIRule{Protocol: "ANY", Direction: "inbound", Action: "accept", IPRange: "198.51.100.10/32"}, "TCP", "198.51.100.10/32", 50000, true},
		{scwAPIRule{Protocol: "TCP", Direction: "inbound", Action: "accept", IPRange: "198.51.100.10/32"}, "ANY", "198.51.100.10/32", 0, false},
		{scwAPIRule{Protocol: "TCP", Direction: "inbound", Action: "accept", IPRange: "203.0.113.7/32", DestPortFrom: port(50000)}, "TCP", "203.0.113.7/32", 50000, true},
		{scwAPIRule{Protocol: "TCP", Direction: "inbound", Action: "accept", IPRange: "203.0.113.7/32", DestPortFrom: port(49000), DestPortTo: port(49999)}, "TCP", "203.0.113.7/32", 50000, false},
		{scwAPIRule{Protocol: "TCP", Direction: "inbound", Action: "drop", IPRange: "203.0.113.7/32", DestPortFrom: port(50000)}, "TCP", "203.0.113.7/32", 50000, false},
		{scwAPIRule{Protocol: "UDP", Direction: "inbound", Action: "accept", IPRange: "203.0.113.7/32", DestPortFrom: port(50000)}, "TCP", "203.0.113.7/32", 50000, false},
		{scwAPIRule{Protocol: "ANY", Direction: "outbound", Action: "accept", IPRange: "198.51.100.10/32"}, "ANY", "198.51.100.10/32", 0, false},
	}

	for i, c := range cases {
		if got := c.rule.covers(c.protocol, c.cidr, c.port); got != c.want {
			t.Errorf("%d: covers = %v, want %v", i, got, c.want)
		}
	}
}

func TestScwMemberSources(t *testing.T) {
	members := []clusterMember{
		{hostname: "a", addresses: []netip.Addr{netip.MustParseAddr("100.64.1.2"), netip.MustParseAddr("192.0.2.5")}},
		{hostname: "b", addresses: []netip.Addr{netip.MustParseAddr("::ffff:192.0.2.5")}}, // the same address twice
		{hostname: "c", addresses: []netip.Addr{netip.MustParseAddr("100.64.1.3"), netip.MustParseAddr("fe80::1")}},
	}

	sources, warnings := scwMemberSources(members)
	if len(sources) != 1 || sources[0].cidr != "192.0.2.5/32" || sources[0].hostname != "a" {
		t.Errorf("sources = %+v", sources)
	}

	if len(warnings) != 1 || !strings.Contains(warnings[0], "the member c has no public IPv4 (only 100.64.1.3)") {
		t.Errorf("warnings = %v", warnings)
	}

	if _, w := scwMemberSources(nil); len(w) != 0 {
		t.Errorf("no member: warnings %v", w)
	}
}
