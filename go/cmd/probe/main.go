// Command probe exercises the ichorgo API against a real cluster from the desktop.
//
//	go run ./cmd/probe [-config ~/.talos/config] [-context name] [-kube-server URL] [-mask [-mask-words a,b]] overview|services NODE|resources NODE|logs NODE SERVICE|dmesg NODE|logstats NODE SERVICE...|network NODE|connections NODE|time NODE|cluster-time|hardware NODE|images NODE|talosconfig-probe|kubeconfig|workloads|rollout-restart KIND NAMESPACE NAME|pods|delete-pod NAMESPACE NAME|netperf-nodes|netperf SERVER CLIENT [pod|host] [SECONDS]|dataservices [HINTS]|etcd|health|parse|pcap NODE IFACE FILTER SECONDS|upgrade-plan NODE|talos-releases|container-logs NODE ID|container-follow NODE ID|mounts NODE|volumes NODE|usage NODE PATH DEPTH|resource-types NODE|resource-list NODE TYPE [NAMESPACE]|resource-get NODE TYPE ID [NAMESPACE]|disk-health NODE|features NODE|etcd-member-plan MEMBERID|support-probe [NODES]|diagnose-report|diagnose anthropic|openai [MODEL]|ai-models anthropic|openai
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

type printer struct{ done chan string }

// eventPrinter prints the first max events, then cancels.
type eventPrinter struct {
	run  *ichorgo.EventsRun
	done chan string
	max  int
}

func (p *eventPrinter) OnEvent(json string) {
	fmt.Println(json)
	if p.max--; p.max == 0 && p.run != nil {
		p.run.Cancel()
	}
}

func (p *eventPrinter) OnDone(errMessage string) { p.done <- errMessage }

// snapshotProbe cancels the snapshot after 20 MiB: it proves streaming works without
// leaving a copy of the cluster's secrets on disk.
type snapshotProbe struct {
	run  *ichorgo.SnapshotRun
	done chan string
}

func (p *snapshotProbe) OnProgress(n int64) {
	if n >= 20<<20 && p.run != nil {
		fmt.Printf("received %d MiB, cancelling\n", n>>20)
		p.run.Cancel()
	}
}

func (p *snapshotProbe) OnDone(path string, size int64, sum, errMessage string) {
	p.done <- fmt.Sprintf("done path=%q size=%d err=%q", path, size, errMessage)
}

// lineCounter counts followed log lines.
type lineCounter struct {
	lines atomic.Int64
	done  chan string
}

func (l *lineCounter) OnLine(string)            { l.lines.Add(1) }
func (l *lineCounter) OnDone(errMessage string) { l.done <- errMessage }

func (p printer) OnProgress(node, message string) { fmt.Printf("[%s] %s\n", node, message) }
func (p printer) OnDone(errMessage string)        { p.done <- errMessage }

func main() {
	home, _ := os.UserHomeDir() //nolint:errcheck
	configPath := flag.String("config", filepath.Join(home, ".talos", "config"), "talosconfig path")
	contextName := flag.String("context", "", "context name (default: current)")
	kubeServer := flag.String("kube-server", "", "Kubernetes API address to use instead of the kubeconfig's (host[:port] or https URL)")
	mask := flag.Bool("mask", false, "screenshot mode: mask IPs, hostnames, domains and context names")
	maskWords := flag.String("mask-words", "", "with -mask: comma-separated extra words to hide")
	flag.Parse()

	if flag.NArg() == 0 {
		fail(fmt.Errorf("usage: probe [flags] overview|services NODE|resources NODE|logs NODE SERVICE|dmesg NODE|logstats NODE SERVICE...|stats NODE|clusterstats|inventory|processes NODE|machineconfig NODE|network NODE|connections NODE|time NODE|cluster-time|hardware NODE|images NODE|talosconfig-probe|kubespan|topology|kubeconfig|pods|delete-pod NAMESPACE NAME|netperf-nodes|netperf SERVER CLIENT [pod|host] [SECONDS]|workloads|rollout-restart KIND NAMESPACE NAME|dataservices [HINTS]|etcd|health|parse|pcap NODE IFACE FILTER SECONDS|upgrade-plan NODE|talos-releases|container-logs NODE ID|container-follow NODE ID|mounts NODE|volumes NODE|usage NODE PATH DEPTH|resource-types NODE|resource-list NODE TYPE [NAMESPACE]|resource-get NODE TYPE ID [NAMESPACE]|disk-health NODE|features NODE|etcd-member-plan MEMBERID|support-probe [NODES]|diagnose-report|diagnose anthropic|openai [MODEL]|ai-models anthropic|openai"))
	}

	raw, err := os.ReadFile(*configPath)
	if err != nil {
		fail(err)
	}

	cfg := string(raw)

	if *mask {
		ichorgo.SetPrivacyMask(true, *maskWords)
		// Like the app, which shows the overview first: it teaches the mask the hostnames.
		_, _ = ichorgo.ClusterOverview(cfg, *contextName) //nolint:errcheck
	}

	var out string

	switch cmd := flag.Arg(0); cmd {
	case "parse":
		out, err = ichorgo.ParseConfig(cfg)
	case "overview":
		out, err = ichorgo.ClusterOverview(cfg, *contextName)
	case "services":
		out, err = ichorgo.NodeServices(cfg, *contextName, flag.Arg(1))
	case "resources":
		out, err = ichorgo.NodeResources(cfg, *contextName, flag.Arg(1))
	case "logs":
		out, err = ichorgo.ServiceLogs(cfg, *contextName, flag.Arg(1), flag.Arg(2), 20)
	case "dmesg":
		out, err = ichorgo.KernelLogs(cfg, *contextName, flag.Arg(1), 20)
	case "logstats":
		out = logStats(cfg, *contextName, flag.Arg(1), flag.Args()[2:])
	case "kubeconfig":
		// Never print the credential itself.
		var kc string
		if kc, err = ichorgo.Kubeconfig(cfg, *contextName, *kubeServer); err == nil {
			out = fmt.Sprintf("kubeconfig: %d bytes, starts with %q", len(kc), firstLine(kc))
		}
	case "workloads":
		out, err = ichorgo.KubeWorkloads(cfg, *contextName, *kubeServer)
	case "rollout-restart":
		// rollout-restart KIND NAMESPACE NAME
		if err = ichorgo.KubeRolloutRestart(cfg, *contextName, *kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3)); err == nil {
			out = "restarted"
		}
	case "pods":
		out, err = ichorgo.KubePods(cfg, *contextName, *kubeServer)
	case "dataservices":
		// dataservices [HINTS], e.g. "garage" or "longhorn,cloudnative-pg"; none checks all.
		out, err = ichorgo.KubeDataServices(cfg, *contextName, *kubeServer, flag.Arg(1))
	case "netperf-nodes":
		out, err = ichorgo.NetPerfNodes(cfg, *contextName, *kubeServer)
	case "netperf":
		// netperf SERVER CLIENT [pod|host] [SECONDS]: creates pods in a temporary namespace.
		out = netPerfRun(cfg, *contextName, *kubeServer, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	case "delete-pod":
		// delete-pod NAMESPACE NAME
		if err = ichorgo.KubeDeletePod(cfg, *contextName, *kubeServer, flag.Arg(1), flag.Arg(2)); err == nil {
			out = "deleted"
		}
	case "machineconfig":
		// Redacted: never print secrets from the probe.
		out, err = ichorgo.NodeMachineConfig(cfg, *contextName, flag.Arg(1), false)
	case "snapshot-probe":
		dest := filepath.Join(os.TempDir(), "etcd-probe.snapshot")
		p := &snapshotProbe{done: make(chan string, 1)}
		p.run = ichorgo.StartEtcdSnapshot(cfg, *contextName, flag.Arg(1), dest, p)
		out = <-p.done
		if _, statErr := os.Stat(dest + ".part"); statErr == nil {
			out += " (partial file left!)"
		}
	case "inventory":
		out, err = ichorgo.ClusterInventory(cfg, *contextName)
	case "containers":
		out, err = ichorgo.NodeContainers(cfg, *contextName, flag.Arg(1))
	case "events":
		l := &eventPrinter{done: make(chan string, 1), max: 15}
		l.run = ichorgo.StartEvents(cfg, *contextName, flag.Arg(1), 10, l)
		out = "done: " + <-l.done
	case "processes":
		out, err = ichorgo.NodeProcesses(cfg, *contextName, flag.Arg(1))
	case "talosconfig-probe":
		// Issue a short-lived read-only config, print only its summary (no key material) and
		// discard it.
		var tc string
		if tc, err = ichorgo.GenerateTalosconfig(cfg, *contextName, "os:reader", 1); err == nil {
			out, err = ichorgo.ParseConfig(tc)
		}
	case "network":
		out, err = ichorgo.NodeNetwork(cfg, *contextName, flag.Arg(1))
	case "connections":
		out, err = ichorgo.NodeConnections(cfg, *contextName, flag.Arg(1))
	case "time":
		out, err = ichorgo.NodeTime(cfg, *contextName, flag.Arg(1))
	case "cluster-time":
		out, err = ichorgo.ClusterTime(cfg, *contextName)
	case "hardware":
		out, err = ichorgo.NodeHardware(cfg, *contextName, flag.Arg(1))
	case "images":
		out, err = ichorgo.NodeImages(cfg, *contextName, flag.Arg(1))
	case "kubespan":
		out, err = ichorgo.KubeSpanStatus(cfg, *contextName)
	case "topology":
		out, err = ichorgo.ClusterTopology(cfg, *contextName)
	case "stats":
		out, err = ichorgo.NodeStats(cfg, *contextName, flag.Arg(1))
	case "clusterstats":
		out, err = ichorgo.ClusterStats(cfg, *contextName)
	case "etcd":
		out, err = ichorgo.EtcdStatus(cfg, *contextName)
	case "pcap":
		out = pcapProbe(cfg, *contextName, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), *mask)
	case "upgrade-plan":
		// Read-only: never calls the upgrade itself.
		out, err = ichorgo.UpgradePlan(cfg, *contextName, flag.Arg(1))
	case "talos-releases":
		out, err = ichorgo.TalosReleases()
	case "container-logs":
		out, err = ichorgo.ContainerLogs(cfg, *contextName, flag.Arg(1), flag.Arg(2), 20)
	case "container-follow":
		// Follows for a few seconds and prints only how many lines came.
		l := &lineCounter{done: make(chan string, 1)}
		run := ichorgo.StartContainerLogFollow(cfg, *contextName, flag.Arg(1), flag.Arg(2), 10, l)

		select {
		case msg := <-l.done:
			out = fmt.Sprintf("ended by itself after %d lines: %q", l.lines.Load(), msg)
		case <-time.After(5 * time.Second):
			run.Cancel()
			out = fmt.Sprintf("%d lines in 5 s, then cancelled: %q", l.lines.Load(), <-l.done)
		}
	case "mounts":
		out, err = ichorgo.NodeMounts(cfg, *contextName, flag.Arg(1))
	case "volumes":
		out, err = ichorgo.NodeVolumes(cfg, *contextName, flag.Arg(1))
	case "usage":
		depth, _ := strconv.Atoi(flag.Arg(3)) //nolint:errcheck
		out, err = ichorgo.NodeDiskUsage(cfg, *contextName, flag.Arg(1), flag.Arg(2), depth)
	case "resource-types":
		out, err = ichorgo.ResourceTypes(cfg, *contextName, flag.Arg(1))
	case "resource-list":
		out, err = ichorgo.ResourceList(cfg, *contextName, flag.Arg(1), flag.Arg(3), flag.Arg(2))
	case "resource-get":
		out, err = ichorgo.ResourceGet(cfg, *contextName, flag.Arg(1), flag.Arg(4), flag.Arg(2), flag.Arg(3))
	case "disk-health":
		out, err = ichorgo.NodeDiskHealth(cfg, *contextName, flag.Arg(1))
	case "features":
		out, err = ichorgo.NodeFeatures(cfg, *contextName, flag.Arg(1))
	case "etcd-member-plan":
		// Read-only: never removes a member.
		out, err = ichorgo.EtcdMemberPlan(cfg, *contextName, flag.Arg(1))
	case "support-probe":
		out = supportProbe(cfg, *contextName, flag.Arg(1))
	case "diagnose-report":
		// What the AI diagnosis would send, anonymized; nothing is sent.
		var d *ichorgo.Diagnosis
		if d, err = ichorgo.CollectDiagnosis(cfg, *contextName, true); err == nil {
			out = d.Report()
		}
	case "diagnose":
		err = diagnose(cfg, *contextName, flag.Arg(1), flag.Arg(2))
	case "ai-models":
		out, err = ichorgo.AIModels(flag.Arg(1), aiKey(flag.Arg(1)), os.Getenv("ICHOR_AI_BASE_URL"))
	case "health":
		p := printer{done: make(chan string, 1)}
		ichorgo.StartClusterHealth(cfg, *contextName, p)

		if msg := <-p.done; msg != "" {
			fail(fmt.Errorf("health check failed: %s", msg))
		}

		fmt.Println("cluster healthy")

		return
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}

	if err != nil {
		fail(err)
	}

	fmt.Println(out)
}

// answerPrinter prints the answer as it grows.
type answerPrinter struct {
	printed int
	done    chan string
}

func (p *answerPrinter) OnAnswer(text string) {
	if len(text) > p.printed {
		fmt.Print(text[p.printed:])
		p.printed = len(text)
	}
}

func (p *answerPrinter) OnDone(errMessage string) { p.done <- errMessage }

// aiKey reads the provider's API key from the environment, never from the command line.
func aiKey(provider string) string {
	if provider == "openai" {
		return os.Getenv("OPENAI_API_KEY")
	}

	return os.Getenv("ANTHROPIC_API_KEY")
}

// diagnose sends the report as collected (real names, so the printed answer reads like the
// cluster) and prints the model's answer. Unlike the rest of the probe, it sends cluster
// data to the provider.
func diagnose(cfg, contextName, provider, model string) error {
	d, err := ichorgo.CollectDiagnosis(cfg, contextName, false)
	if err != nil {
		return err
	}

	p := &answerPrinter{done: make(chan string, 1)}
	d.Ask(provider, aiKey(provider), model, os.Getenv("ICHOR_AI_BASE_URL"), "en", "", p)

	if msg := <-p.done; msg != "" {
		return fmt.Errorf("diagnosis failed: %s", msg)
	}

	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")

	return line
}
