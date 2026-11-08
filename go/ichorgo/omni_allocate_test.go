package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/siderolabs/go-api-signature/pkg/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	testMachineID = "aaaaaaaa-1111-4222-8333-bbbbbbbbbbbb"
	testCluster   = "lab"
)

// fakeOmniStore is the COSI state of the fake Omni: resources by type and id.
type fakeOmniStore struct {
	mu      sync.Mutex
	types   map[string]map[string]*v1alpha1.Resource
	denied  bool             // writes answer PermissionDenied
	flow    []*v1alpha1.Spec // what successive Gets of the cluster machine status answer
	created []*v1alpha1.Resource
	updated []*v1alpha1.Resource
}

func (s *fakeOmniStore) put(resourceType, id string, labels map[string]string, spec []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.types == nil {
		s.types = map[string]map[string]*v1alpha1.Resource{}
	}

	if s.types[resourceType] == nil {
		s.types[resourceType] = map[string]*v1alpha1.Resource{}
	}

	s.types[resourceType][id] = &v1alpha1.Resource{
		Metadata: &v1alpha1.Metadata{Namespace: "default", Type: resourceType, Id: id, Version: "3", Phase: "running", Labels: labels},
		Spec:     &v1alpha1.Spec{ProtoSpec: spec, YamlSpec: "ignored: true\n"},
	}
}

func (s *fakeOmniStore) get(resourceType, id string) *v1alpha1.Resource {
	s.mu.Lock()
	defer s.mu.Unlock()

	if resourceType == omniClusterMachineStatus && len(s.flow) > 0 {
		spec := s.flow[0]
		s.flow = s.flow[1:]

		return &v1alpha1.Resource{Metadata: &v1alpha1.Metadata{Id: id}, Spec: spec}
	}

	return s.types[resourceType][id]
}

func (s *fakeOmniStore) has(resourceType string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.types[resourceType] != nil
}

func (s *fakeOmniStore) list(resourceType string, stream grpc.ServerStreamingServer[v1alpha1.ListResponse]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, res := range s.types[resourceType] {
		if err := stream.Send(&v1alpha1.ListResponse{Resource: res}); err != nil {
			return err
		}
	}

	return nil
}

func (f *fakeOmniAPI) Create(ctx context.Context, req *v1alpha1.CreateRequest) (*v1alpha1.CreateResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if err := message.NewGRPC(md, "/cosi.resource.State/Create").VerifySignature(f.key); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	f.store.mu.Lock()
	defer f.store.mu.Unlock()

	if f.store.denied {
		return nil, status.Error(codes.PermissionDenied, "no access")
	}

	f.store.created = append(f.store.created, req.GetResource())

	return &v1alpha1.CreateResponse{Resource: req.GetResource()}, nil
}

func (f *fakeOmniAPI) Update(ctx context.Context, req *v1alpha1.UpdateRequest) (*v1alpha1.UpdateResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if err := message.NewGRPC(md, "/cosi.resource.State/Update").VerifySignature(f.key); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	f.store.mu.Lock()
	defer f.store.mu.Unlock()

	if f.store.denied {
		return nil, status.Error(codes.PermissionDenied, "no access")
	}

	f.store.updated = append(f.store.updated, req.GetNewResource())

	return &v1alpha1.UpdateResponse{Resource: req.GetNewResource()}, nil
}

// decodeTestJoinLabels reads field 3 of a GetMachineJoinConfigRequest, as Omni does.
func decodeTestJoinLabels(req []byte) (map[string]string, error) {
	entries, err := protoFields(req, 3)
	if err != nil {
		return nil, err
	}

	labels := map[string]string{}

	for _, entry := range entries {
		key, err := protoField(entry, 1)
		if err != nil {
			return nil, err
		}

		value, err := protoField(entry, 2)
		if err != nil {
			return nil, err
		}

		labels[string(key)] = string(value)
	}

	return labels, nil
}

// protoString, protoCount and protoMessage build the fake's specs.
func protoString(msg []byte, num protowire.Number, value string) []byte {
	return protowire.AppendString(protowire.AppendTag(msg, num, protowire.BytesType), value)
}

func protoCount(msg []byte, num protowire.Number, value uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(msg, num, protowire.VarintType), value)
}

func protoMessage(msg []byte, num protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(msg, num, protowire.BytesType), value)
}

// testAllocationSpec is a MachineSetSpec with update_strategy 1 and a field the app does not
// know, which a write must keep.
func testAllocationSpec(field protowire.Number, class string, count, allocationType uint64) []byte {
	var msg []byte
	msg = protoString(msg, 1, class)
	msg = protoCount(msg, 2, count)
	msg = protoCount(msg, 3, allocationType)

	spec := protoCount(nil, 1, 1)
	spec = protoMessage(spec, field, msg)

	return protoString(spec, 99, "future")
}

func testMachineSpec(hostname, cluster string) []byte {
	var network []byte
	network = protoString(network, 1, hostname)
	network = protoString(network, 3, "192.0.2.7")
	network = protoString(network, 3, "2001:db8::7")

	spec := protoString(nil, 1, "v1.11.2")
	spec = protoMessage(spec, 3, network)
	spec = protoCount(spec, 6, 1)
	spec = protoCount(spec, 7, 1)

	if cluster != "" {
		spec = protoString(spec, 9, cluster)
	}

	return spec
}

func testSetLabels(role string) map[string]string {
	return map[string]string{omniLabelCluster: testCluster, role: ""}
}

type omniAllocateFixture struct {
	f        *fakeOmniAPI
	cfg      string
	done     chan [2]string
	mu       sync.Mutex
	progress []joinProgress
}

func (o *omniAllocateFixture) OnProgress(s string) {
	var p joinProgress

	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	o.mu.Lock()
	defer o.mu.Unlock()

	o.progress = append(o.progress, p)
}

func (o *omniAllocateFixture) OnDone(address, errMessage string) {
	o.done <- [2]string{address, errMessage}
}

func (o *omniAllocateFixture) phases() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	phases := make([]string, 0, len(o.progress))
	for _, p := range o.progress {
		phases = append(phases, p.Phase)
	}

	return strings.Join(phases, " ")
}

func (o *omniAllocateFixture) run(t *testing.T, setID string) string {
	t.Helper()

	a := &omniAllocate{config: o.cfg, context: "", machineID: testMachineID, setID: setID, listener: o, poll: time.Millisecond}
	a.start(10 * time.Second)

	select {
	case got := <-o.done:
		return got[1]
	case <-time.After(15 * time.Second):
		t.Fatal("no OnDone")

		return ""
	}
}

func newOmniAllocateFixture(t *testing.T) *omniAllocateFixture {
	t.Helper()
	useMemAuthStore(t)

	f := &fakeOmniAPI{}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	t.Cleanup(func() { sessions.forgetAuth(omniAuthKeyOf(t, endpoint)) })

	cfg := fmt.Sprintf("context: lab-ctx\ncontexts:\n    lab-ctx:\n        endpoints:\n            - %s\n        auth:\n            siderov1:\n                identity: %s\n        cluster: %s\n", endpoint, testUserIdentity, testCluster)

	f.store.put(omniMachineStatusType, testMachineID, map[string]string{"omni.sidero.dev/arch": "amd64", "ichor/role": "worker"}, testMachineSpec("talos-new", ""))
	f.store.put(omniMachineStatusType, "cccccccc-1111-4222-8333-dddddddddddd", nil, testMachineSpec("taken", "other"))
	f.store.put(omniMachineSetType, "lab-control-planes", testSetLabels(omniLabelRoleControlPlane), protoCount(nil, 1, 1))
	f.store.put(omniMachineSetType, "lab-workers", testSetLabels(omniLabelRoleWorker), testAllocationSpec(7, "small", 2, 0))
	f.store.put(omniMachineSetType, "lab-all", testSetLabels(omniLabelRoleWorker), testAllocationSpec(7, "small", 0, 1))
	f.store.put(omniMachineSetType, "lab-old", testSetLabels(omniLabelRoleWorker), testAllocationSpec(2, "small", 1, 0))
	f.store.put(omniMachineSetType, "lab-big", testSetLabels(omniLabelRoleWorker), testAllocationSpec(7, "big", 1, 0))
	f.store.put(omniMachineSetType, "other-workers", map[string]string{omniLabelCluster: "other", omniLabelRoleWorker: ""}, protoCount(nil, 1, 1))
	f.store.put(omniMachineClassType, "small", nil, protoString(nil, 1, "omni.sidero.dev/arch = amd64, ichor/role = worker"))
	f.store.put(omniMachineClassType, "big", nil, protoString(nil, 1, "omni.sidero.dev/cores > 32"))

	return &omniAllocateFixture{f: f, cfg: cfg, done: make(chan [2]string, 1)}
}

func omniAuthKeyOf(t *testing.T, endpoint string) string {
	t.Helper()

	cfgCtx, err := omniInstanceContext(endpoint, testUserIdentity)
	if err != nil {
		t.Fatal(err)
	}

	return omniAuthKey(cfgCtx)
}

func testRunningFlow() []*v1alpha1.Spec {
	stage := func(stage, ready uint64) *v1alpha1.Spec {
		return &v1alpha1.Spec{ProtoSpec: protoCount(protoCount(nil, 1, ready), 2, stage)}
	}

	return []*v1alpha1.Spec{
		stage(omniStageInstalling, 0), stage(omniStageConfiguring, 0), stage(omniStageBooting, 0),
		stage(omniStageRunning, 0), stage(omniStageRunning, 1),
	}
}

func TestOmniMachineSetsList(t *testing.T) {
	o := newOmniAllocateFixture(t)

	out, err := OmniMachineSets(o.cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	var got omniMachineSets
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if got.Cluster != testCluster || len(got.MachineSets) != 5 {
		t.Fatalf("sets = %s", out)
	}

	want := map[string]omniAllocation{
		"lab-control-planes": {Type: omniAllocationNone},
		"lab-workers":        {ClassName: "small", Type: omniAllocationStatic, Count: 2},
		"lab-all":            {ClassName: "small", Type: omniAllocationUnlimited},
		"lab-old":            {ClassName: "small", Type: omniAllocationStatic, Count: 1},
		"lab-big":            {ClassName: "big", Type: omniAllocationStatic, Count: 1},
	}

	for _, set := range got.MachineSets {
		if set.Allocation != want[set.ID] {
			t.Errorf("%s = %+v", set.ID, set.Allocation)
		}

		if (set.ID == "lab-control-planes") != (set.Role == joinRoleControlPlane) {
			t.Errorf("%s role = %s", set.ID, set.Role)
		}

		if set.ID == "lab-workers" && (len(set.MatchLabels) != 1 || !strings.Contains(set.MatchLabels[0], "ichor/role")) {
			t.Errorf("match labels = %v", set.MatchLabels)
		}
	}
}

func TestOmniUnallocatedList(t *testing.T) {
	o := newOmniAllocateFixture(t)

	out, err := OmniUnallocatedMachines(o.cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	var got []omniMachine
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("machines = %s", out)
	}

	m := got[0]
	if m.ID != testMachineID || m.Hostname != "talos-new" || len(m.Addresses) != 2 || m.TalosVersion != "v1.11.2" ||
		!m.Connected || !m.Maintenance || m.Labels["ichor/role"] != "worker" {
		t.Errorf("machine = %+v", m)
	}
}

func TestOmniAllocateCreatesMachineSetNode(t *testing.T) {
	o := newOmniAllocateFixture(t)
	o.f.store.flow = testRunningFlow()

	if msg := o.run(t, "lab-control-planes"); msg != "" {
		t.Fatal(msg)
	}

	if len(o.f.store.created) != 1 || len(o.f.store.updated) != 0 {
		t.Fatalf("created %d, updated %d", len(o.f.store.created), len(o.f.store.updated))
	}

	node := o.f.store.created[0]
	md := node.GetMetadata()

	if md.GetType() != omniMachineSetNodeType || md.GetId() != testMachineID || md.GetNamespace() != "default" ||
		md.GetVersion() != "undefined" || md.GetPhase() != "running" || md.GetOwner() != "" || node.GetSpec() == nil {
		t.Errorf("node = %v", node)
	}

	want := map[string]string{omniLabelCluster: testCluster, omniLabelRoleControlPlane: "", omniLabelMachineSet: "lab-control-planes"}
	if len(md.GetLabels()) != len(want) {
		t.Errorf("labels = %v", md.GetLabels())
	}

	for k, v := range want {
		if got, ok := md.GetLabels()[k]; !ok || got != v {
			t.Errorf("label %s = %q", k, got)
		}
	}

	if got := o.phases(); got != "allocated installing booting booting done" {
		t.Errorf("phases = %s", got)
	}
}

func TestOmniAllocateStaticCountBump(t *testing.T) {
	for _, c := range []struct {
		set   string
		field protowire.Number
		count uint64
	}{{"lab-workers", 7, 3}, {"lab-old", 2, 2}} {
		t.Run(c.set, func(t *testing.T) {
			o := newOmniAllocateFixture(t)
			o.f.store.flow = testRunningFlow()

			if msg := o.run(t, c.set); msg != "" {
				t.Fatal(msg)
			}

			if len(o.f.store.updated) != 1 || len(o.f.store.created) != 0 {
				t.Fatalf("updated %d, created %d", len(o.f.store.updated), len(o.f.store.created))
			}

			res := o.f.store.updated[0]
			spec := res.GetSpec().GetProtoSpec()
			msg, _ := protoField(spec, c.field) //nolint:errcheck

			if got := protoVarint(msg, 2); got != c.count {
				t.Errorf("count = %d", got)
			}

			if name, _ := protoField(msg, 1); string(name) != "small" { //nolint:errcheck
				t.Errorf("class = %q", name)
			}

			future, _ := protoField(spec, 99) //nolint:errcheck
			if string(future) != "future" || protoVarint(spec, 1) != 1 {
				t.Errorf("a field was lost: %x", spec)
			}

			if res.GetMetadata().GetVersion() != "3" {
				t.Errorf("version = %q: the write must carry the one it read", res.GetMetadata().GetVersion())
			}
		})
	}
}

func TestOmniAllocateUnlimitedWritesNothing(t *testing.T) {
	o := newOmniAllocateFixture(t)
	o.f.store.flow = testRunningFlow()

	if msg := o.run(t, "lab-all"); msg != "" {
		t.Fatal(msg)
	}

	if len(o.f.store.created)+len(o.f.store.updated) != 0 {
		t.Errorf("wrote %d + %d", len(o.f.store.created), len(o.f.store.updated))
	}
}

func TestOmniAllocateRefusals(t *testing.T) {
	cases := []struct {
		name, set, want string
		machine         string
		denied          bool
	}{
		{name: "missing label", set: "lab-big", want: "omni.sidero.dev/cores > 32"},
		{name: "another cluster's set", set: "other-workers", want: "is not of cluster lab"},
		{name: "unknown set", set: "nope", want: "no machine set nope"},
		{name: "permission denied", set: "lab-control-planes", want: "Operator role", denied: true},
		{name: "permission denied on the count", set: "lab-workers", want: "Operator role", denied: true},
		{name: "already allocated", set: "lab-workers", machine: "cccccccc-1111-4222-8333-dddddddddddd", want: "already belongs to cluster other"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := newOmniAllocateFixture(t)
			o.f.store.denied = c.denied

			a := &omniAllocate{config: o.cfg, machineID: cmpOr(c.machine, testMachineID), setID: c.set, listener: o, poll: time.Millisecond}
			a.start(10 * time.Second)

			got := <-o.done
			if !strings.Contains(got[1], c.want) {
				t.Errorf("error = %q", got[1])
			}

			if len(o.f.store.created)+len(o.f.store.updated) != 0 {
				t.Errorf("wrote %d + %d", len(o.f.store.created), len(o.f.store.updated))
			}
		})
	}
}

func TestOmniAllocateDemoAndPlainTalos(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	sets, err := OmniMachineSets(demo, "")
	if err != nil || !strings.Contains(sets, "demo-workers") {
		t.Errorf("demo sets = %s, %v", sets, err)
	}

	machines, err := OmniUnallocatedMachines(demo, "")
	if err != nil || !strings.Contains(machines, "talos-new-1") {
		t.Errorf("demo machines = %s, %v", machines, err)
	}

	o := &omniAllocateFixture{done: make(chan [2]string, 1)}
	StartOmniAllocate(demo, "", testMachineID, "demo-workers", o)

	if got := <-o.done; !strings.Contains(got[1], "demo") {
		t.Errorf("demo run = %q", got[1])
	}
}

func TestOmniAllocateStopKeepsAllocation(t *testing.T) {
	o := newOmniAllocateFixture(t)

	a := &omniAllocate{config: o.cfg, machineID: testMachineID, setID: "lab-control-planes", listener: o, poll: time.Millisecond}
	run := a.start(10 * time.Second)

	for deadline := time.Now().Add(5 * time.Second); o.phases() == "" && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}

	run.Cancel()

	if got := <-o.done; !strings.Contains(got[1], "the allocation stays") {
		t.Errorf("stopped = %q", got[1])
	}
}

func TestOmniAllocatePrivacyMode(t *testing.T) {
	o := newOmniAllocateFixture(t)

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	a := &omniAllocate{config: o.cfg, machineID: testMachineID, setID: "lab-workers", listener: o, poll: time.Millisecond}
	a.start(10 * time.Second)

	if got := <-o.done; !strings.Contains(got[1], "privacy mode") {
		t.Errorf("privacy = %q", got[1])
	}
}

func TestParseOmniLabels(t *testing.T) {
	if labels, err := parseOmniLabels(""); err != nil || labels != nil {
		t.Errorf("empty = %v, %v", labels, err)
	}

	if labels, err := parseOmniLabels(`{"ichor/role":"worker"}`); err != nil || labels["ichor/role"] != "worker" {
		t.Errorf("labels = %v, %v", labels, err)
	}

	for _, bad := range []string{`{"omni.sidero.dev/arch":"amd64"}`, `not json`, `{" ":"x"}`} {
		if _, err := parseOmniLabels(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestOmniSelectorMissing(t *testing.T) {
	labels := map[string]string{"a": "1", "cores": "8", "arch": "amd64"}

	cases := []struct {
		selectors []string
		want      string
	}{
		{nil, ""},
		{[]string{"a"}, ""},
		{[]string{"!b"}, ""},
		{[]string{"!a"}, "!a"},
		{[]string{"a = 1"}, ""},
		{[]string{"a == 2"}, "a == 2"},
		{[]string{"a != 2, b != 2"}, ""},
		{[]string{"arch in (amd64, arm64)"}, ""},
		{[]string{"arch notin (amd64)"}, "arch notin (amd64)"},
		{[]string{"zone notin (a)"}, ""},
		{[]string{"zone in (a)"}, "zone in (a)"},
		{[]string{"cores > 4, cores <= 8"}, ""},
		{[]string{"cores >= 16"}, "cores >= 16"},
		{[]string{"memory > 1"}, "memory > 1"},
		{[]string{"arch > 1"}, "arch > 1"},
		// Alternatives: one holding is enough, else the closest one is named.
		{[]string{"nope, nada", "a"}, ""},
		{[]string{"x, y, z", "a, y"}, "y"},
		// A form the app cannot read is left to Omni.
		{[]string{"a strange form"}, ""},
	}

	for _, c := range cases {
		if got := omniSelectorMissing(c.selectors, labels); got != c.want {
			t.Errorf("%v = %q, want %q", c.selectors, got, c.want)
		}
	}
}
