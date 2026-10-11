package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// stderrTrace prints the startup trace (-timing) to stderr, one step per line, so the
// command's JSON on stdout stays clean: `probe overview -timing`, `probe parse -timing`.
type stderrTrace struct{}

func (stderrTrace) OnTrace(raw string) { fmt.Fprintln(os.Stderr, formatTrace(raw)) }

// formatTrace turns a trace step's JSON into "trace overview.all_nodes 1234 ms count=8 unreachable=1".
func formatTrace(raw string) string {
	var s struct {
		Step        string `json:"step"`
		Ms          int64  `json:"ms"`
		Count       int    `json:"count"`
		Unreachable int    `json:"unreachable"`
	}

	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return "trace: " + raw
	}

	line := fmt.Sprintf("trace %s %d ms", s.Step, s.Ms)
	if s.Count > 0 {
		line += fmt.Sprintf(" count=%d", s.Count)
	}

	if s.Unreachable > 0 {
		line += fmt.Sprintf(" unreachable=%d", s.Unreachable)
	}

	return line
}

// Talos nodes and the cluster.
var nodeCommands = []command{
	{name: "join-inspect", args: "ADDRESS", noConfig: true, run: func(e env) (out string, err error) {
		// Read-only: a node in maintenance mode over the insecure maintenance API.
		return ichorgo.MaintenanceNodeInspect(flag.Arg(1), 0)
	}},
	{name: "upgrade-ext", args: "NODE IMAGE", run: func(e env) (out string, err error) {
		// Read-only: the node's extensions against the Image Factory's list for IMAGE's version.
		return ichorgo.UpgradeExtensionCheck(e.cfg, e.context, flag.Arg(1), flag.Arg(2))
	}},
	{name: "kubespan-diag", args: "[NODE]", run: func(e env) (out string, err error) {
		// Read-only: one node's peers and their verdicts, or every node compared.
		if flag.Arg(1) == "" {
			return ichorgo.KubeSpanDiagnosticsAll(e.cfg, e.context)
		}

		return ichorgo.KubeSpanDiagnostics(e.cfg, e.context, flag.Arg(1))
	}},
	{name: "nettool", args: "NODE dns|ping|port|trace|http TARGET [OPTIONS-JSON]", run: func(e env) (out string, err error) {
		// Read-only, but runs a privileged netshoot container on NODE (os:admin).
		return netToolRun(e.cfg, e.context, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4)), nil
	}},
	{name: "cluster-upgrade-plan", args: "VERSION", run: func(e env) (out string, err error) {
		// Read-only: the order, each node's checks.
		return ichorgo.ClusterUpgradePlan(e.cfg, e.context, e.kubeServer, flag.Arg(1))
	}},
	{name: "cluster-upgrade", args: "VERSION [drain]", run: func(e env) (out string, err error) {
		// Upgrades every node for real, one at a time; a rerun resumes. Acknowledges the risks.
		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartClusterUpgrade(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2) == "drain", true, m)
		out = "done: " + <-m.done

		return out, err
	}},
	{name: "parse", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ParseConfig(e.cfg)

		return out, err
	}},
	{name: "overview", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ClusterOverview(e.cfg, e.context)

		return out, err
	}},
	{name: "services", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeServices(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "resources", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeResources(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "logs", args: "NODE SERVICE", run: func(e env) (out string, err error) {
		out, err = ichorgo.ServiceLogs(e.cfg, e.context, flag.Arg(1), flag.Arg(2), 20)

		return out, err
	}},
	{name: "dmesg", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.KernelLogs(e.cfg, e.context, flag.Arg(1), 20)

		return out, err
	}},
	{name: "logstats", args: "NODE SERVICE...", run: func(e env) (out string, err error) {
		if flag.NArg() < 2 {
			fail(fmt.Errorf("usage: probe logstats NODE [SERVICE...]"))
		}

		out = logStats(e.cfg, e.context, flag.Arg(1), flag.Args()[2:])

		return out, err
	}},
	{name: "machineconfig", args: "NODE", run: func(e env) (out string, err error) {
		// Redacted: never print secrets from the probe.
		out, err = ichorgo.NodeMachineConfig(e.cfg, e.context, flag.Arg(1), false)

		return out, err
	}},
	{name: "containers", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeContainers(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "container-restart", args: "NODE NAMESPACE ID", run: func(e env) (out string, err error) {
		// Restarts the container for real (NAMESPACE: system or k8s.io).
		err = ichorgo.ContainerRestart(e.cfg, e.context, flag.Arg(1), flag.Arg(2), flag.Arg(3))

		return "restarted", err
	}},
	{name: "events", args: "NODE", run: func(e env) (out string, err error) {
		l := &eventPrinter{done: make(chan string, 1), max: 15}
		l.run = ichorgo.StartEvents(e.cfg, e.context, flag.Arg(1), 10, l)
		out = "done: " + <-l.done

		return out, err
	}},
	{name: "processes", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeProcesses(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "talosconfig-probe", args: "", run: func(e env) (out string, err error) {
		// Issue a short-lived read-only config, print only its summary (no key material) and
		// discard it.
		var tc string
		if tc, err = ichorgo.GenerateTalosconfig(e.cfg, e.context, "os:reader", 1); err == nil {
			out, err = ichorgo.ParseConfig(tc)
		}

		return out, err
	}},
	{name: "network", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeNetwork(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "connections", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeConnections(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "time", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeTime(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "cluster-time", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ClusterTime(e.cfg, e.context)

		return out, err
	}},
	{name: "hardware", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeHardware(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "sensor-health", run: func(e env) (string, error) {
		// What the monitor's temperature alert reads: every node's hottest sensor.
		return ichorgo.ClusterSensorHealth(e.cfg, e.context)
	}},
	{name: "sensors", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeSensors(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "images", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeImages(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "image-pull", args: "IMAGE [NODES] [system|cri]", run: func(e env) (out string, err error) {
		// Pulls IMAGE for real on NODES (comma-separated; empty: every node), into the system
		// namespace unless told otherwise.
		ns := flag.Arg(3)
		if ns == "" {
			ns = "system"
		}

		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartImagePull(e.cfg, e.context, flag.Arg(2), flag.Arg(1), ns, m)
		out = "done: " + <-m.done

		return out, err
	}},
	{name: "system-images", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.TalosSystemImages(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "topology", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ClusterTopology(e.cfg, e.context)

		return out, err
	}},
	{name: "stats", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeStats(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "clusterstats", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.ClusterStats(e.cfg, e.context)

		return out, err
	}},
	{name: "pcap-pair", args: "NODEA,NODEB IFACE[,IFACE] FILTER SECONDS", run: func(e env) (string, error) {
		// Captures on both nodes at once, merged into one file (deleted after).
		return pcapPairProbe(e.cfg, e.context, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4)), nil
	}},
	{name: "pcap", args: "NODE IFACE FILTER SECONDS", run: func(e env) (out string, err error) {
		out = pcapProbe(e.cfg, e.context, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), e.mask)

		return out, err
	}},
	{name: "talos-releases", args: "", run: func(e env) (out string, err error) {
		out, err = ichorgo.TalosReleases()

		return out, err
	}},
	{name: "container-logs", args: "NODE ID", run: func(e env) (out string, err error) {
		out, err = ichorgo.ContainerLogs(e.cfg, e.context, flag.Arg(1), flag.Arg(2), 20)

		return out, err
	}},
	{name: "container-follow", args: "NODE ID", run: func(e env) (out string, err error) {
		// Follows for a few seconds and prints only how many lines came.
		l := &lineCounter{done: make(chan string, 1)}
		run := ichorgo.StartContainerLogFollow(e.cfg, e.context, flag.Arg(1), flag.Arg(2), 10, l)

		select {
		case msg := <-l.done:
			out = fmt.Sprintf("ended by itself after %d lines: %q", l.lines.Load(), msg)
		case <-time.After(5 * time.Second):
			run.Cancel()
			out = fmt.Sprintf("%d lines in 5 s, then cancelled: %q", l.lines.Load(), <-l.done)
		}

		return out, err
	}},
	{name: "mounts", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeMounts(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "volumes", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeVolumes(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "disks", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeDisks(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "usage", args: "NODE PATH DEPTH", run: func(e env) (out string, err error) {
		depth, _ := strconv.Atoi(flag.Arg(3)) //nolint:errcheck
		out, err = ichorgo.NodeDiskUsage(e.cfg, e.context, flag.Arg(1), flag.Arg(2), depth)

		return out, err
	}},
	{name: "resource-types", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.ResourceTypes(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "resource-list", args: "NODE TYPE [NAMESPACE]", run: func(e env) (out string, err error) {
		out, err = ichorgo.ResourceList(e.cfg, e.context, flag.Arg(1), flag.Arg(3), flag.Arg(2))

		return out, err
	}},
	{name: "resource-get", args: "NODE TYPE ID [NAMESPACE]", run: func(e env) (out string, err error) {
		out, err = ichorgo.ResourceGet(e.cfg, e.context, flag.Arg(1), flag.Arg(4), flag.Arg(2), flag.Arg(3))

		return out, err
	}},
	{name: "disk-health", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeDiskHealth(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "features", args: "NODE", run: func(e env) (out string, err error) {
		out, err = ichorgo.NodeFeatures(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "support-probe", args: "[NODES]", run: func(e env) (out string, err error) {
		out = supportProbe(e.cfg, e.context, flag.Arg(1))

		return out, err
	}},
	{name: "diagnose", args: "anthropic|openai [MODEL]", run: func(e env) (out string, err error) {
		err = diagnose(e.cfg, e.context, flag.Arg(1), flag.Arg(2))

		return out, err
	}},
	{name: "ai-models", args: "anthropic|openai", run: func(e env) (out string, err error) {
		out, err = ichorgo.AIModels(flag.Arg(1), aiKey(flag.Arg(1)), os.Getenv("ICHOR_AI_BASE_URL"))

		return out, err
	}},
	{name: "health", args: "", run: func(e env) (out string, err error) {
		p := printer{done: make(chan string, 1)}
		ichorgo.StartClusterHealth(e.cfg, e.context, p)

		if msg := <-p.done; msg != "" {
			fail(fmt.Errorf("health check failed: %s", msg))
		}

		fmt.Println("cluster healthy")

		return noOutput, nil
	}},
}
