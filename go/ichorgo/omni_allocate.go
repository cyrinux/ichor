package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// S8 step 7: a machine that registered in Omni (booted with its join config or kernel arguments)
// waits unallocated until something puts it in a cluster. A cluster allocates by machine class,
// where the class's labels pick it up, or by hand, where one MachineSetNode binds it to a machine
// set, as omnictl does. Specs are read and written as their protobuf (Spec.ProtoSpec), field
// numbers from Omni's client/api/omni/specs/omni.proto: what Omni stores, while its YAML is only
// a rendering of it. A write keeps every field the app does not know.

const (
	omniMachineSetType        = "MachineSets.omni.sidero.dev"
	omniMachineSetNodeType    = "MachineSetNodes.omni.sidero.dev"
	omniMachineClassType      = "MachineClasses.omni.sidero.dev"
	omniMachineStatusType     = "MachineStatuses.omni.sidero.dev"
	omniClusterMachineStatus  = "ClusterMachineStatuses.omni.sidero.dev"
	omniLabelCluster          = "omni.sidero.dev/cluster"
	omniLabelRoleControlPlane = "omni.sidero.dev/role-controlplane"
	omniLabelRoleWorker       = "omni.sidero.dev/role-worker"
	omniLabelMachineSet       = "omni.sidero.dev/machine-set"
	omniNamespace             = "default"
	omniReservedLabelPrefix   = "omni.sidero.dev/"

	omniAllocationStatic    = "static"
	omniAllocationUnlimited = "unlimited"
	omniAllocationNone      = "none"

	// An install takes minutes, a poll costs one Get.
	omniAllocateTimeout = 20 * time.Minute
	omniAllocatePoll    = 5 * time.Second

	// Phases of StartOmniAllocate, in order, then joinPhaseInstalling and joinPhaseDone.
	omniPhaseAllocated = "allocated"
	omniPhaseBooting   = "booting"
)

// ClusterMachineStatusSpec.Stage.
const (
	omniStageBooting     = 1
	omniStageInstalling  = 2
	omniStageConfiguring = 3
	omniStageRunning     = 4
	omniStageUpgrading   = 6
	omniStageRebooting   = 7
	omniStagePoweringOn  = 10
)

var (
	errOmniAllocateStopped = errors.New("stopped following: the allocation stays, and Omni goes on with the machine")
	errOmniAllocatePrivacy = errors.New("turn privacy mode off to allocate a machine: the confirmation needs its real name")
	errNotOmni             = errors.New("this cluster is not managed by Omni")
)

// omniAllocation is how a machine set takes its machines.
type omniAllocation struct {
	ClassName string `json:"className"`
	Type      string `json:"type"`
	Count     uint64 `json:"count"`
}

// omniMachineSet is a machine set of the cluster.
type omniMachineSet struct {
	ID          string         `json:"id"`
	Role        string         `json:"role"`
	Allocation  omniAllocation `json:"allocation"`
	MatchLabels []string       `json:"matchLabels"`
}

type omniMachineSets struct {
	Cluster     string           `json:"cluster"`
	MachineSets []omniMachineSet `json:"machineSets"`
}

// omniMachine is a machine no cluster has yet.
type omniMachine struct {
	ID           string            `json:"id"`
	Hostname     string            `json:"hostname"`
	Addresses    []string          `json:"addresses"`
	TalosVersion string            `json:"talosVersion"`
	Connected    bool              `json:"connected"`
	Maintenance  bool              `json:"maintenance"`
	Labels       map[string]string `json:"labels"`
}

// OmniMachineSets lists the machine sets of the context's cluster and how each takes machines
// (Omni's Reader role). JSON: {"cluster","machineSets":[{"id","role":"controlplane"|"worker",
// "allocation":{"className","type":"static"|"unlimited"|"none","count"},"matchLabels"}]}.
func OmniMachineSets(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoOmniMachineSets())
	}

	sets, err := withOmni(configYAML, contextName, func(ctx context.Context, cfgCtx *clientconfig.Context, cc *grpc.ClientConn) (omniMachineSets, error) {
		return listOmniMachineSets(ctx, cc, cfgCtx.Cluster)
	})
	if err != nil {
		return "", err
	}

	return toJSON(sets)
}

// OmniUnallocatedMachines lists the machines of the Omni instance no cluster has (Omni's Reader
// role). JSON: [{"id","hostname","addresses","talosVersion","connected","maintenance","labels"}].
func OmniUnallocatedMachines(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoOmniMachines())
	}

	machines, err := withOmni(configYAML, contextName, func(ctx context.Context, _ *clientconfig.Context, cc *grpc.ClientConn) ([]omniMachine, error) {
		return listOmniUnallocated(ctx, cc)
	})
	if err != nil {
		return "", err
	}

	return toJSON(machines)
}

// withOmni runs fn with a connection to the Omni API of an Omni context.
func withOmni[T any](configYAML, contextName string, fn func(context.Context, *clientconfig.Context, *grpc.ClientConn) (T, error)) (T, error) {
	var zero T

	s, release, err := acquireSigned(configYAML, contextName)
	if err != nil {
		return zero, err
	}

	defer release()

	cc, err := omniAllocateDial(s)
	if err != nil {
		return zero, err
	}

	defer cc.Close() //nolint:errcheck

	parent := callParent(configYAML, contextName)

	ctx, cancel := context.WithTimeout(parent, callTimeout)
	defer cancel()

	out, err := fn(ctx, s.context, cc)

	return scopedResult(parent, out, err)
}

// omniAllocateDial connects to the Omni API of an Omni context that names its cluster.
func omniAllocateDial(s *session) (*grpc.ClientConn, error) {
	if !isOmni(s.context) {
		return nil, errNotOmni
	}

	if s.context.Cluster == "" {
		return nil, errOmniNoCluster
	}

	cc, err := omniDial(s.context, s.signing)
	if err != nil {
		return nil, fmt.Errorf("connect to Omni: %w", err)
	}

	return cc, nil
}

func listOmniMachineSets(ctx context.Context, cc *grpc.ClientConn, cluster string) (omniMachineSets, error) {
	out := omniMachineSets{Cluster: cluster, MachineSets: []omniMachineSet{}}

	var resources []*v1alpha1.Resource

	err := omniListResources(ctx, cc, omniMachineSetType, func(res *v1alpha1.Resource) {
		if res.GetMetadata().GetLabels()[omniLabelCluster] == cluster {
			resources = append(resources, res)
		}
	})
	if err != nil {
		return out, omniReadError(err)
	}

	for _, res := range resources {
		set := decodeOmniMachineSet(res)

		if set.Allocation.ClassName != "" {
			if set.MatchLabels, err = omniClassLabels(ctx, cc, set.Allocation.ClassName); err != nil {
				return out, omniReadError(err)
			}
		}

		out.MachineSets = append(out.MachineSets, set)
	}

	slices.SortFunc(out.MachineSets, func(a, b omniMachineSet) int { return strings.Compare(a.ID, b.ID) })

	return out, nil
}

func decodeOmniMachineSet(res *v1alpha1.Resource) omniMachineSet {
	labels := res.GetMetadata().GetLabels()
	set := omniMachineSet{ID: res.GetMetadata().GetId(), Role: joinRoleWorker, MatchLabels: []string{}}

	if _, ok := labels[omniLabelRoleControlPlane]; ok {
		set.Role = joinRoleControlPlane
	}

	set.Allocation = decodeOmniAllocation(res.GetSpec().GetProtoSpec())

	return set
}

// omniAllocationField is where a machine set's spec says how it allocates: machine_allocation,
// else the deprecated machine_class (the same message).
func omniAllocationField(spec []byte) protowire.Number {
	if v, err := protoField(spec, 7); err == nil && v != nil {
		return 7
	}

	return 2
}

// decodeOmniAllocation reads MachineAllocation {name = 1; machine_count = 2; allocation_type = 3}.
func decodeOmniAllocation(spec []byte) omniAllocation {
	msg, err := protoField(spec, omniAllocationField(spec))
	if err != nil {
		return omniAllocation{Type: omniAllocationNone}
	}

	name, _ := protoField(msg, 1) //nolint:errcheck // malformed: no class

	if len(name) == 0 {
		return omniAllocation{Type: omniAllocationNone}
	}

	a := omniAllocation{ClassName: string(name), Type: omniAllocationStatic, Count: protoVarint(msg, 2)}

	if protoVarint(msg, 3) == 1 {
		a.Type, a.Count = omniAllocationUnlimited, 0
	}

	return a
}

// protoVarint is the value of varint field num in msg, 0 when absent (as proto3 means it).
func protoVarint(msg []byte, num protowire.Number) uint64 {
	var out uint64

	for len(msg) > 0 {
		n, typ, size := protowire.ConsumeTag(msg)
		if size < 0 {
			return out
		}

		msg = msg[size:]

		if n == num && typ == protowire.VarintType {
			v, size := protowire.ConsumeVarint(msg)
			if size < 0 {
				return out
			}

			out = v
		}

		size = protowire.ConsumeFieldValue(n, typ, msg)
		if size < 0 {
			return out
		}

		msg = msg[size:]
	}

	return out
}

// omniClassLabels reads MachineClassSpec.match_labels; none for a name that is no class (a
// machine request set takes the same place, and has no labels to check).
func omniClassLabels(ctx context.Context, cc *grpc.ClientConn, class string) ([]string, error) {
	res, err := omniGet(ctx, cc, omniMachineClassType, class)
	if status.Code(err) == codes.NotFound {
		return []string{}, nil
	}

	if err != nil {
		return nil, err
	}

	values, err := protoFields(res.GetSpec().GetProtoSpec(), 1)
	if err != nil {
		return nil, fmt.Errorf("read machine class %s: %w", class, err)
	}

	labels := make([]string, 0, len(values))
	for _, v := range values {
		labels = append(labels, string(v))
	}

	return labels, nil
}

func omniGet(ctx context.Context, cc *grpc.ClientConn, resourceType, id string) (*v1alpha1.Resource, error) {
	resp, err := v1alpha1.NewStateClient(cc).Get(ctx, &v1alpha1.GetRequest{
		Namespace: omniNamespace, Type: resourceType, Id: id, Options: &v1alpha1.GetOptions{},
	})
	if err != nil {
		return nil, err
	}

	return resp.GetResource(), nil
}

func listOmniUnallocated(ctx context.Context, cc *grpc.ClientConn) ([]omniMachine, error) {
	machines := []omniMachine{}

	err := omniListResources(ctx, cc, omniMachineStatusType, func(res *v1alpha1.Resource) {
		if m, cluster := decodeOmniMachine(res); cluster == "" {
			machines = append(machines, m)
		}
	})
	if err != nil {
		return machines, omniReadError(err)
	}

	slices.SortFunc(machines, func(a, b omniMachine) int { return strings.Compare(a.Hostname+a.ID, b.Hostname+b.ID) })

	return machines, nil
}

// decodeOmniMachine reads MachineStatusSpec: talos_version 1, network 3 {hostname 1, addresses
// 3}, connected 6, maintenance 7, cluster 9. Its labels are the resource's: Omni's own and those
// signed into the join token.
func decodeOmniMachine(res *v1alpha1.Resource) (omniMachine, string) {
	spec := res.GetSpec().GetProtoSpec()
	m := omniMachine{
		ID: res.GetMetadata().GetId(), Addresses: []string{}, Labels: map[string]string{},
		Connected: protoVarint(spec, 6) != 0, Maintenance: protoVarint(spec, 7) != 0,
	}

	for k, v := range res.GetMetadata().GetLabels() {
		m.Labels[k] = v
	}

	version, _ := protoField(spec, 1) //nolint:errcheck // malformed: not shown
	m.TalosVersion = string(version)

	network, _ := protoField(spec, 3)     //nolint:errcheck // malformed: not shown
	hostname, _ := protoField(network, 1) //nolint:errcheck // malformed: not shown
	m.Hostname = string(hostname)

	addresses, _ := protoFields(network, 3) //nolint:errcheck // malformed: not shown
	for _, a := range addresses {
		m.Addresses = append(m.Addresses, string(a))
	}

	cluster, _ := protoField(spec, 9) //nolint:errcheck // malformed: seen as unallocated, and Omni refuses

	return m, string(cluster)
}

func omniReadError(err error) error {
	if status.Code(err) == codes.PermissionDenied {
		return errors.New("your Omni role cannot read the machines: ask for the Reader role")
	}

	return omniAPIError(err)
}

func omniWriteError(err error) error {
	if status.Code(err) == codes.PermissionDenied {
		return errors.New("your Omni role cannot allocate a machine: ask for the Operator role")
	}

	return omniAPIError(err)
}

// parseOmniLabels reads the labels to sign into the join request: a JSON object of strings, ""
// for none. Omni's own prefix is refused: it would read as something Omni measured.
func parseOmniLabels(labelsJSON string) (map[string]string, error) {
	if strings.TrimSpace(labelsJSON) == "" {
		return nil, nil
	}

	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		return nil, errors.New("invalid labels")
	}

	for key := range labels {
		if strings.HasPrefix(key, omniReservedLabelPrefix) || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("label %q cannot be set: use your own prefix", key)
		}
	}

	return labels, nil
}

// appendOmniLabels adds GetMachineJoinConfigRequest.machine_labels (field 3, a map: one embedded
// {1: key, 2: value} per entry), in a stable order.
func appendOmniLabels(req []byte, labels map[string]string) []byte {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, key := range keys {
		var entry []byte
		entry = protowire.AppendTag(entry, 1, protowire.BytesType)
		entry = protowire.AppendString(entry, key)
		entry = protowire.AppendTag(entry, 2, protowire.BytesType)
		entry = protowire.AppendString(entry, labels[key])

		req = protowire.AppendTag(req, 3, protowire.BytesType)
		req = protowire.AppendBytes(req, entry)
	}

	return req
}

func demoOmniMachineSets() omniMachineSets {
	return omniMachineSets{Cluster: "demo", MachineSets: []omniMachineSet{
		{ID: "demo-control-planes", Role: joinRoleControlPlane, Allocation: omniAllocation{Type: omniAllocationNone}, MatchLabels: []string{}},
		{
			ID: "demo-workers", Role: joinRoleWorker,
			Allocation:  omniAllocation{ClassName: "small", Type: omniAllocationStatic, Count: 2},
			MatchLabels: []string{"omni.sidero.dev/arch = amd64, ichor/role = worker"},
		},
	}}
}

func demoOmniMachines() []omniMachine {
	return []omniMachine{
		{
			ID: "11111111-2222-4333-8444-555555555551", Hostname: "talos-new-1", Addresses: []string{"192.0.2.41"}, TalosVersion: demoTalosVersion,
			Connected: true, Labels: map[string]string{"omni.sidero.dev/arch": "amd64", "ichor/role": "worker"},
		},
		{
			ID: "11111111-2222-4333-8444-555555555552", Hostname: "talos-new-2", Addresses: []string{"192.0.2.42"}, TalosVersion: demoTalosVersion,
			Connected: true, Maintenance: true, Labels: map[string]string{"omni.sidero.dev/arch": "arm64"},
		},
	}
}
