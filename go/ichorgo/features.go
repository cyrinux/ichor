package ichorgo

import (
	"context"
	"errors"
	"fmt"
)

// featureRule is the Talos version range a feature of the app needs on the node.
type featureRule struct {
	name string
	// min is the first minor version ("v1.8") whose server has the API or resource; "" when
	// every Talos version this client can talk to has it.
	min string
	// until is the first minor version that no longer has it ("" = still there).
	until string
	// hint completes the reason shown when the feature is not supported.
	hint string
}

// oldestCOSI is the first Talos serving the COSI resource API (cosi.resource.State, v1.3.0):
// every resource read of this app (machine status, network, hardware...) needs it.
const oldestCOSI = "v1.3"

// featureRules is the version table behind NodeFeatures. Each minimum is the first Talos
// release tag (v1.0.0 ... v1.14.0, plus main for 1.15) whose sources have the RPC in
// api/machine/*.proto or the resource type in pkg/machinery/resources, found with
// `git grep` on github.com/siderolabs/talos; the comment of each entry names what was looked
// up. Entries without a minimum are in v1.0.0 already. Should the table be wrong for some
// version, the call itself still answers "not available on this node's Talos version"
// (friendlyError) on a node that lacks the API.
var featureRules = []featureRule{
	// rpc Events (machine.proto): v1.0.0.
	{name: "events"},
	// rpc Containers, rpc Stats (machine.proto): v1.0.0.
	{name: "containers"},
	// rpc Processes (machine.proto): v1.0.0.
	{name: "processes"},
	// rpc Logs, rpc Dmesg with follow (machine.proto): v1.0.0.
	{name: "logFollow"},
	// rpc ServiceRestart/ServiceStart/ServiceStop (machine.proto): v1.0.0.
	{name: "serviceControl"},
	// rpc PacketCapture (machine.proto): first in v1.2.0.
	{name: "packetCapture", min: "v1.2"},
	// rpc Upgrade of MachineService: v1.0.0, deprecated ("to be removed in Talos 1.18" in
	// cmd/talosctl/cmd/talos/upgrade.go on main); service LifecycleService: first in v1.13.0.
	// StartUpgrade uses whichever the node has, so there is no version limit here.
	{name: "upgrade"},
	// VolumeStatuses.block.talos.dev: first in v1.8.0 (MountStatuses.block.talos.dev only in
	// v1.10.0: on 1.8 and 1.9 NodeVolumes has no mountedOn).
	{name: "volumes", min: "v1.8"},
	// rpc DiskUsage (machine.proto): v1.0.0.
	{name: "diskUsage"},
	// rpc Mounts (machine.proto): v1.0.0.
	{name: "mounts"},
	// KubeSpanPeerStatuses.kubespan.talos.dev: v1.0.0, read through the COSI API.
	{name: "kubespan", min: oldestCOSI},
	// rpc EtcdStatus, EtcdAlarmList, EtcdAlarmDisarm, EtcdDefragment (machine.proto): first in
	// v1.4.0 (EtcdMemberList is in v1.0.0: on older nodes EtcdStatus still lists the members).
	{name: "etcd", min: "v1.4", hint: "the etcd status API"},
	// rpc EtcdSnapshot (machine.proto): v1.0.0.
	{name: "etcdSnapshot"},
	// rpc EtcdRemoveMemberByID (machine.proto): first in v1.3.0 (EtcdForfeitLeadership: v1.0.0).
	{name: "etcdMemberActions", min: "v1.3", hint: "removing an etcd member by id"},
	// cosi.resource.State served by apid/machined (internal/app): first in v1.3.0.
	{name: "resourceBrowser", min: oldestCOSI, hint: "the COSI resource API"},
	// Built from logs, dmesg, mounts, processes and resources; degrades file by file.
	{name: "supportBundle"},
	// SMARTStatuses.block.talos.dev and the DiskSMARTConfig document: not in v1.14.0, on main
	// (1.15, "Disk SMART Status" in hack/release.toml).
	{name: "diskHealth", min: "v1.15", hint: "the SMARTStatus resource"},
	// rpc GenerateClientConfiguration (machine.proto): v1.0.0.
	{name: "issueConfig"},
	// LinkStatuses/AddressStatuses/RouteStatuses/ResolverStatuses/TimeServerStatuses
	// .net.talos.dev: v1.0.0, read through the COSI API.
	{name: "network", min: oldestCOSI},
	// rpc Netstat (machine.proto): first in v1.4.0.
	{name: "connections", min: "v1.4"},
	// TimeService (api/time/time.proto): v1.0.0.
	{name: "time"},
	// Processors/MemoryModules.hardware.talos.dev: v1.1.0; SystemInformations: v1.2.0;
	// ExtensionStatuses: v1.0.0; SecurityStates.talos.dev: v1.5.0; Disks.block.talos.dev:
	// v1.8.0 (before: the Disks API). All read through the COSI API; a section missing on a
	// version is reported in NodeHardware's "errors", not as a failure.
	{name: "hardware", min: oldestCOSI},
	// rpc ImageList (machine.proto): first in v1.5.0; service ImageService (v1.13.0) is the
	// fallback once ImageList is gone.
	{name: "images", min: "v1.5"},
	// MachineConfigs.config.talos.dev: v1.0.0, read through the COSI API.
	{name: "machineConfig", min: oldestCOSI},
	// service DebugService (api/machine/debug.proto) and ImageService.Pull: first in v1.13.0.
	{name: "debugShell", min: "v1.13"},
}

type nodeFeatures struct {
	Version  string                  `json:"version"`
	Features map[string]featureState `json:"features"`
}

type featureState struct {
	Supported  bool   `json:"supported"`
	MinVersion string `json:"minVersion"` // "" when every version has it
	Reason     string `json:"reason"`     // why it is not supported, else ""
}

// NodeFeatures tells which features of the app node's Talos version supports, so the UI can
// hide or explain the others: {"version","features":{name:{"supported","minVersion","reason"}}}
// (os:reader). It is based on the version the node reports and a table of minimum versions;
// a feature without a known minimum counts as supported and its call reports
// "not available on this node's Talos version" when it is not. The version is asked again
// on every call, so a node upgraded while the app runs reports its new features.
func NodeFeatures(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeFeatures", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		previous, _ := s.versions.Load(node)

		version := s.refreshNodeVersion(ctx, node)
		if version == "" {
			// Unreachable right now: keep what was known and report why.
			if previous != nil {
				s.versions.Store(node, previous)
			}

			if version, err = s.requireNodeVersion(ctx, node); err != nil {
				return "", err
			}
		} else if previous != nil && previous != version {
			s.definitions.Delete(node)
		}

		return toJSON(computeFeatures(version))
	})
}

// requireNodeVersion is nodeVersion with the reason when the node does not answer.
func (s *session) requireNodeVersion(ctx context.Context, node string) (string, error) {
	if v := s.nodeVersion(ctx, node); v != "" {
		return v, nil
	}

	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	if _, err := s.client.Version(withNode(ctx, node)); err != nil {
		return "", errors.New(friendlyError(err))
	}

	return "", errors.New("the node did not report its Talos version")
}

func computeFeatures(version string) nodeFeatures {
	out := nodeFeatures{Version: version, Features: make(map[string]featureState, len(featureRules))}

	for _, r := range featureRules {
		st := featureState{Supported: true, MinVersion: r.min}

		switch {
		case r.min != "" && compareMinor(version, r.min) < 0:
			st.Supported = false
			st.Reason = unsupportedReason(r, version, "needs Talos "+r.min+" or newer")
		case r.until != "" && compareMinor(version, r.until) >= 0:
			st.Supported = false
			st.Reason = unsupportedReason(r, version, "removed in Talos "+r.until)
		}

		out.Features[r.name] = st
	}

	return out
}

func unsupportedReason(r featureRule, version, need string) string {
	if r.hint != "" {
		need = r.hint + " " + need
	}

	return fmt.Sprintf("%s: %s", notAvailableOn(version), need)
}

// compareMinor compares the major.minor of two versions: a prerelease of 1.15 has the
// features of 1.15.
func compareMinor(a, b string) int {
	na, nb := versionNumbers(a), versionNumbers(b)

	for i := range 2 {
		if na[i] != nb[i] {
			if na[i] < nb[i] {
				return -1
			}

			return 1
		}
	}

	return 0
}

// featureMinVersion returns the minimum Talos version of a feature ("" when none).
func featureMinVersion(name string) string {
	for _, r := range featureRules {
		if r.name == name {
			return r.min
		}
	}

	return ""
}
