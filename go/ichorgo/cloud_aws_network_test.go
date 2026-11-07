package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	awsTestNetworkSG = "sg-0123456789abcdef7"
	awsTestPhoneIP   = "203.0.113.7"
)

// fakeAWSNetwork is the default VPC side of EC2: its subnets, and the ichor-nodes group with
// the rules it has been given.
type fakeAWSNetwork struct {
	noDefaultVPC  bool
	privateOnly   bool // no subnet assigns a public IPv4
	groupExists   bool // ichor-nodes is there from an earlier call
	foreign       bool // ... without the ichor-managed tag
	duplicateRule bool // AuthorizeSecurityGroupIngress answers InvalidPermission.Duplicate

	rules      []string // "all:sg-…" or "tcp:50000:203.0.113.7/32"
	creates    []url.Values
	authorizes []url.Values
}

// handle answers the calls of the guided network; false for the rest.
func (n *fakeAWSNetwork) handle(w http.ResponseWriter, action string, form url.Values) bool {
	const ns = ` xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"`

	switch {
	case action == "DescribeVpcs" && form.Get("Filter.1.Name") == "isDefault":
		vpcs := `<item><vpcId>` + awsTestVPC + `</vpcId><isDefault>true</isDefault></item>`
		if n.noDefaultVPC || form.Get("Filter.1.Value.1") != "true" {
			vpcs = ""
		}

		fmt.Fprint(w, `<DescribeVpcsResponse`+ns+`><vpcSet>`+vpcs+`</vpcSet></DescribeVpcsResponse>`)
	case action == "DescribeSubnets" && form.Get("Filter.1.Name") == "vpc-id":
		subnet := func(id, az string, public bool) string {
			return fmt.Sprintf(`<item><subnetId>%s</subnetId><vpcId>%s</vpcId><availabilityZone>%s</availabilityZone><mapPublicIpOnLaunch>%t</mapPublicIpOnLaunch></item>`,
				id, awsTestVPC, az, public)
		}

		fmt.Fprint(w, `<DescribeSubnetsResponse`+ns+`><subnetSet>`+subnet("subnet-0123456789abcdef3", "eu-west-1c", !n.privateOnly)+
			subnet("subnet-0123456789abcdef2", "eu-west-1b", false)+`</subnetSet></DescribeSubnetsResponse>`)
	case action == "DescribeSecurityGroups" && (form.Get("Filter.1.Name") == "group-name" || form.Get("GroupId.1") == awsTestNetworkSG):
		group := ""
		if n.groupExists && (form.Get("Filter.1.Value.1") == awsNetworkGroup || form.Get("GroupId.1") == awsTestNetworkSG) {
			group = n.group()
		}

		fmt.Fprint(w, `<DescribeSecurityGroupsResponse`+ns+`><securityGroupInfo>`+group+`</securityGroupInfo></DescribeSecurityGroupsResponse>`)
	case action == "DescribeSecurityGroups" && form.Get("Filter.1.Name") == "" && form.Get("GroupId.1") == "":
		// AWSOptions' list: the app's group and a default group opened to the world.
		n.groupExists = true
		open := `<item><groupId>sg-0123456789abcdef8</groupId><vpcId>` + awsTestVPC + `</vpcId><groupName>default</groupName><ipPermissions><item><ipProtocol>-1</ipProtocol><ipRanges><item><cidrIp>0.0.0.0/0</cidrIp></item></ipRanges></item></ipPermissions></item>`
		fmt.Fprint(w, `<DescribeSecurityGroupsResponse`+ns+`><securityGroupInfo>`+n.group()+open+`</securityGroupInfo></DescribeSecurityGroupsResponse>`)
	case action == "CreateSecurityGroup":
		n.creates = append(n.creates, form)
		n.groupExists = true
		fmt.Fprint(w, `<CreateSecurityGroupResponse`+ns+`><return>true</return><groupId>`+awsTestNetworkSG+`</groupId></CreateSecurityGroupResponse>`)
	case action == "AuthorizeSecurityGroupIngress":
		n.authorize(w, form)
	default:
		return false
	}

	return true
}

func (n *fakeAWSNetwork) authorize(w http.ResponseWriter, form url.Values) {
	n.authorizes = append(n.authorizes, form)

	rule := "all:" + form.Get("IpPermissions.1.Groups.1.GroupId")
	if form.Get("IpPermissions.1.IpProtocol") == "tcp" {
		rule = "tcp:" + form.Get("IpPermissions.1.FromPort") + ":" + form.Get("IpPermissions.1.IpRanges.1.CidrIp")
	}

	if n.duplicateRule || slices.Contains(n.rules, rule) {
		awsFail(w, http.StatusBadRequest, awsCodeDuplicateRule, "the specified rule already exists")

		return
	}

	n.rules = append(n.rules, rule)
	fmt.Fprint(w, `<AuthorizeSecurityGroupIngressResponse><return>true</return></AuthorizeSecurityGroupIngressResponse>`)
}

func (n *fakeAWSNetwork) group() string {
	var perms string

	for _, r := range n.rules {
		parts := strings.SplitN(r, ":", 3)
		if parts[0] == "all" {
			perms += `<item><ipProtocol>-1</ipProtocol><groups><item><groupId>` + parts[1] + `</groupId></item></groups></item>`

			continue
		}

		perms += `<item><ipProtocol>tcp</ipProtocol><fromPort>` + parts[1] + `</fromPort><toPort>` + parts[1] +
			`</toPort><ipRanges><item><cidrIp>` + parts[2] + `</cidrIp><description>Ichor phone 2026-11-07</description></item></ipRanges></item>`
	}

	tags := `<tagSet><item><key>ichor-managed</key><value>true</value></item></tagSet>`
	if n.foreign {
		tags = ""
	}

	return `<item><groupId>` + awsTestNetworkSG + `</groupId><vpcId>` + awsTestVPC + `</vpcId><groupName>` + awsNetworkGroup +
		`</groupName><ipPermissions>` + perms + `</ipPermissions>` + tags + `</item>`
}

// startFakeCheckIP answers the phone's address (status 0: unreachable).
func startFakeCheckIP(t *testing.T, status int, body string) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	endpoint, now := awsCheckIPEndpoint, awsNetworkNow
	awsCheckIPEndpoint = func() string { return srv.URL }
	awsNetworkNow = func() time.Time { return time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC) }

	if status == 0 {
		srv.Close()
	}

	t.Cleanup(func() { awsCheckIPEndpoint, awsNetworkNow = endpoint, now })
}

func TestAWSNetworkPrepare(t *testing.T) {
	cases := []struct {
		name        string
		network     fakeAWSNetwork
		spec        string
		checkStatus int
		checkBody   string
		wantErr     string
		wantCreated bool
		wantRules   []string
		wantWarn    string
		wantCalls   int // AuthorizeSecurityGroupIngress calls
	}{
		{
			name: "Omni: the group with the self rule only", spec: `{"role":"worker","omni":true}`,
			wantCreated: true, wantRules: []string{"all:0:" + awsTestNetworkSG + ":self"}, wantCalls: 1,
		},
		{
			name: "plain Talos: plus 50000 from the phone's /32", spec: `{"role":"controlplane"}`,
			checkStatus: http.StatusOK, checkBody: awsTestPhoneIP + "\n",
			wantCreated: true, wantRules: []string{"all:0:" + awsTestNetworkSG + ":self", "tcp:50000:" + awsTestPhoneIP + "/32"}, wantCalls: 2,
		},
		{
			name: "second call: reused, no new rule", spec: `{"role":"worker"}`,
			network:     fakeAWSNetwork{groupExists: true, rules: []string{"all:" + awsTestNetworkSG, "tcp:50000:" + awsTestPhoneIP + "/32"}},
			checkStatus: http.StatusOK, checkBody: awsTestPhoneIP,
			wantRules: []string{"all:0:" + awsTestNetworkSG + ":self", "tcp:50000:" + awsTestPhoneIP + "/32"},
		},
		{
			name: "a duplicate rule is not an error", spec: `{"role":"worker","omni":true}`,
			network:     fakeAWSNetwork{duplicateRule: true},
			wantCreated: true, wantRules: []string{}, wantCalls: 1,
		},
		{
			name: "checkip down: a warning, no 50000 rule", spec: `{"role":"worker"}`,
			wantCreated: true, wantRules: []string{"all:0:" + awsTestNetworkSG + ":self"}, wantCalls: 1,
			wantWarn: "could not learn this phone's address",
		},
		{
			name: "checkip answers no public IPv4: a warning", spec: `{"role":"worker"}`,
			checkStatus: http.StatusOK, checkBody: "10.0.0.5",
			wantCreated: true, wantRules: []string{"all:0:" + awsTestNetworkSG + ":self"}, wantCalls: 1,
			wantWarn: "could not learn this phone's address",
		},
		{
			name: "no public subnet: the first, with a warning", spec: `{"role":"worker","omni":true}`,
			network:     fakeAWSNetwork{privateOnly: true},
			wantCreated: true, wantRules: []string{"all:0:" + awsTestNetworkSG + ":self"}, wantCalls: 1,
			wantWarn: "no subnet assigns public IPs",
		},
		{
			name: "someone else's ichor-nodes: used, never changed", spec: `{"role":"worker"}`,
			network:     fakeAWSNetwork{groupExists: true, foreign: true},
			checkStatus: http.StatusOK, checkBody: awsTestPhoneIP,
			wantRules: []string{}, wantWarn: "Ichor did not make",
		},
		{
			name: "no default VPC: refused with what to do", spec: `{"role":"worker"}`,
			network: fakeAWSNetwork{noDefaultVPC: true},
			wantErr: "this account has no default VPC in eu-west-1: create one at AWS (VPC → Actions → Create default VPC)",
		},
		{name: "a role is required", spec: `{"role":"etcd"}`, wantErr: "is not a node role"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			network := c.network
			fake := &fakeEC2{network: &network}
			startFakeEC2(t, fake)
			startFakeCheckIP(t, c.checkStatus, c.checkBody)

			out, err := AWSNetworkPrepare("", "", awsTestCred, c.spec)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}

				if len(network.creates)+len(network.authorizes) != 0 {
					t.Errorf("a refused call wrote: creates %d, authorizes %d", len(network.creates), len(network.authorizes))
				}

				return
			}

			r := decodeJSON[awsNetworkResult](t, out, err)

			want := awsTestSubnetPublic
			if c.network.privateOnly {
				want = "subnet-0123456789abcdef2"
			}

			if r.SubnetID != want || r.SecurityGroupID != awsTestNetworkSG || r.VPCID != awsTestVPC || r.Created != c.wantCreated {
				t.Errorf("result = %+v", r)
			}

			if got := awsTestRules(r.Rules); !slices.Equal(got, c.wantRules) && !(len(got) == 0 && len(c.wantRules) == 0) {
				t.Errorf("rules = %v, want %v", got, c.wantRules)
			}

			wantIP := ""
			if slices.ContainsFunc(c.wantRules, func(s string) bool { return strings.HasPrefix(s, "tcp:") }) {
				wantIP = awsTestPhoneIP
			}

			if r.PhoneIP != wantIP {
				t.Errorf("phoneIp = %q, want %q", r.PhoneIP, wantIP)
			}

			warned := slices.ContainsFunc(r.Warnings, func(w string) bool { return c.wantWarn != "" && strings.Contains(w, c.wantWarn) })
			if warned != (c.wantWarn != "") || (c.wantWarn == "" && len(r.Warnings) != 0) {
				t.Errorf("warnings = %v, want %q", r.Warnings, c.wantWarn)
			}

			if len(network.authorizes) != c.wantCalls {
				t.Errorf("AuthorizeSecurityGroupIngress called %d times, want %d", len(network.authorizes), c.wantCalls)
			}

			awsCheckNetworkWrites(t, &network)
		})
	}
}

const awsTestSubnetPublic = "subnet-0123456789abcdef3"

func awsTestRules(rules []awsNetworkRule) []string {
	out := []string{}

	for _, r := range rules {
		s := fmt.Sprintf("%s:%d:%s", r.Protocol, r.Port, r.Source)
		if r.Self {
			s += ":self"
		}

		out = append(out, s)
	}

	return out
}

// awsCheckNetworkWrites checks what the app sent: the group is tagged and in the default VPC,
// and nothing is ever opened to the world or on 6443.
func awsCheckNetworkWrites(t *testing.T, n *fakeAWSNetwork) {
	t.Helper()

	for _, c := range n.creates {
		if c.Get("GroupName") != awsNetworkGroup || c.Get("VpcId") != awsTestVPC || c.Get("TagSpecification.1.ResourceType") != "security-group" ||
			c.Get("TagSpecification.1.Tag.1.Key") != awsTagManaged || c.Get("TagSpecification.1.Tag.1.Value") != "true" {
			t.Errorf("CreateSecurityGroup = %v", c)
		}
	}

	for _, a := range n.authorizes {
		cidr := a.Get("IpPermissions.1.IpRanges.1.CidrIp")
		if strings.HasPrefix(cidr, "0.0.0.0") || a.Get("IpPermissions.1.FromPort") == "6443" || a.Get("GroupId") != awsTestNetworkSG {
			t.Errorf("AuthorizeSecurityGroupIngress = %v", a)
		}

		if cidr != "" && (cidr != awsTestPhoneIP+"/32" || a.Get("IpPermissions.1.IpRanges.1.Description") != "Ichor phone 2026-11-07") {
			t.Errorf("phone rule = %v", a)
		}
	}
}

func TestAWSNetworkPrepareRecorded(t *testing.T) {
	withDataDir(t)

	network := fakeAWSNetwork{}
	startFakeEC2(t, &fakeEC2{network: &network})
	startFakeCheckIP(t, http.StatusOK, awsTestPhoneIP)

	if _, err := AWSNetworkPrepare("", "", awsTestCred, `{"role":"worker"}`); err != nil {
		t.Fatal(err)
	}

	entries := auditEntries("", awsNetworkAuditAction)
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "group="+awsTestNetworkSG) ||
		!strings.Contains(entries[0].Params, "region=eu-west-1") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestAWSNetworkDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := AWSNetworkPrepare(cfg, "", awsTestCred, `{"role":"worker"}`); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Errorf("demo = %v", err)
	}
}

func TestAWSOptionsGroupFlags(t *testing.T) {
	withDataDir(t)

	startFakeEC2(t, &fakeEC2{network: &fakeAWSNetwork{rules: []string{"all:" + awsTestNetworkSG}}})

	out, err := AWSOptions("", "", awsTestCred)
	o := decodeJSON[awsOptions](t, out, err)

	flags := map[string][2]bool{}
	for _, g := range o.SecurityGroups {
		flags[g.Name] = [2]bool{g.Managed, g.WideOpen}
	}

	if flags[awsNetworkGroup] != [2]bool{true, false} || flags["default"] != [2]bool{false, true} {
		t.Errorf("groups = %+v", o.SecurityGroups)
	}
}

func TestAWSWideOpen(t *testing.T) {
	perm := func(protocol string, from, to int, cidr string) awsAPIPermission {
		return awsAPIPermission{Protocol: protocol, FromPort: from, ToPort: to, Ranges: []awsAPIRange{{CIDR: cidr}}}
	}

	cases := []struct {
		name  string
		perms []awsAPIPermission
		want  bool
	}{
		{"all traffic from the world", []awsAPIPermission{perm("-1", 0, 0, "0.0.0.0/0")}, true},
		{"every TCP port from the world", []awsAPIPermission{perm("tcp", 0, 65535, "0.0.0.0/0")}, true},
		{"all traffic from the IPv6 world", []awsAPIPermission{{Protocol: "-1", Ranges6: []awsAPIRange6{{CIDR: "::/0"}}}}, true},
		{"one port from the world", []awsAPIPermission{perm("tcp", 443, 443, "0.0.0.0/0")}, false},
		{"all traffic from one address", []awsAPIPermission{perm("-1", 0, 0, awsTestPhoneIP+"/32")}, false},
		{"none", nil, false},
	}

	for _, c := range cases {
		if got := awsWideOpen(c.perms); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAWSIAMPolicyNetwork(t *testing.T) {
	var policy struct {
		Statement []awsPolicyStatement `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(AWSIAMPolicy()), &policy); err != nil {
		t.Fatal(err)
	}

	for _, s := range policy.Statement {
		if slices.Contains(s.Action, "ec2:AuthorizeSecurityGroupIngress") {
			eq, _ := s.Condition["StringEquals"].(map[string]any)
			if len(s.Action) != 1 || eq["aws:ResourceTag/ichor-managed"] != "true" {
				t.Errorf("authorize statement = %+v", s)
			}
		}

		if slices.Contains(s.Action, "ec2:CreateTags") {
			eq, _ := s.Condition["StringEquals"].(map[string]any)
			if actions, _ := eq["ec2:CreateAction"].([]any); !slices.Contains(actions, any("CreateSecurityGroup")) {
				t.Errorf("CreateTags statement = %+v", s)
			}
		}
	}
}
