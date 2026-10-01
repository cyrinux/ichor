// Command probe exercises the talosmobile API against a real cluster from the desktop.
//
//	go run ./cmd/probe [-config ~/.talos/config] [-context name] [-mask [-mask-words a,b]] overview|services NODE|resources NODE|logs NODE SERVICE|dmesg NODE|logstats NODE SERVICE...|network NODE|connections NODE|time NODE|cluster-time|hardware NODE|images NODE|talosconfig-probe|kubeconfig|etcd|health|parse
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyrinux/talosdev-apk/go/talosmobile"
)

type printer struct{ done chan string }

// eventPrinter prints the first max events, then cancels.
type eventPrinter struct {
	run  *talosmobile.EventsRun
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
	run  *talosmobile.SnapshotRun
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

func (p printer) OnProgress(node, message string) { fmt.Printf("[%s] %s\n", node, message) }
func (p printer) OnDone(errMessage string)        { p.done <- errMessage }

func main() {
	home, _ := os.UserHomeDir() //nolint:errcheck
	configPath := flag.String("config", filepath.Join(home, ".talos", "config"), "talosconfig path")
	contextName := flag.String("context", "", "context name (default: current)")
	mask := flag.Bool("mask", false, "screenshot mode: mask IPs, hostnames, domains and context names")
	maskWords := flag.String("mask-words", "", "with -mask: comma-separated extra words to hide")
	flag.Parse()

	if flag.NArg() == 0 {
		fail(fmt.Errorf("usage: probe [flags] overview|services NODE|resources NODE|logs NODE SERVICE|dmesg NODE|logstats NODE SERVICE...|stats NODE|processes NODE|machineconfig NODE|network NODE|connections NODE|time NODE|cluster-time|hardware NODE|images NODE|talosconfig-probe|kubespan|kubeconfig|etcd|health|parse"))
	}

	raw, err := os.ReadFile(*configPath)
	if err != nil {
		fail(err)
	}

	cfg := string(raw)

	if *mask {
		talosmobile.SetPrivacyMask(true, *maskWords)
		// Like the app, which shows the overview first: it teaches the mask the hostnames.
		_, _ = talosmobile.ClusterOverview(cfg, *contextName) //nolint:errcheck
	}

	var out string

	switch cmd := flag.Arg(0); cmd {
	case "parse":
		out, err = talosmobile.ParseConfig(cfg)
	case "overview":
		out, err = talosmobile.ClusterOverview(cfg, *contextName)
	case "services":
		out, err = talosmobile.NodeServices(cfg, *contextName, flag.Arg(1))
	case "resources":
		out, err = talosmobile.NodeResources(cfg, *contextName, flag.Arg(1))
	case "logs":
		out, err = talosmobile.ServiceLogs(cfg, *contextName, flag.Arg(1), flag.Arg(2), 20)
	case "dmesg":
		out, err = talosmobile.KernelLogs(cfg, *contextName, flag.Arg(1), 20)
	case "logstats":
		out = logStats(cfg, *contextName, flag.Arg(1), flag.Args()[2:])
	case "kubeconfig":
		// Never print the credential itself.
		var kc string
		if kc, err = talosmobile.Kubeconfig(cfg, *contextName); err == nil {
			out = fmt.Sprintf("kubeconfig: %d bytes, starts with %q", len(kc), firstLine(kc))
		}
	case "machineconfig":
		// Redacted: never print secrets from the probe.
		out, err = talosmobile.NodeMachineConfig(cfg, *contextName, flag.Arg(1), false)
	case "snapshot-probe":
		dest := filepath.Join(os.TempDir(), "etcd-probe.snapshot")
		p := &snapshotProbe{done: make(chan string, 1)}
		p.run = talosmobile.StartEtcdSnapshot(cfg, *contextName, flag.Arg(1), dest, p)
		out = <-p.done
		if _, statErr := os.Stat(dest + ".part"); statErr == nil {
			out += " (partial file left!)"
		}
	case "containers":
		out, err = talosmobile.NodeContainers(cfg, *contextName, flag.Arg(1))
	case "events":
		l := &eventPrinter{done: make(chan string, 1), max: 15}
		l.run = talosmobile.StartEvents(cfg, *contextName, flag.Arg(1), 10, l)
		out = "done: " + <-l.done
	case "processes":
		out, err = talosmobile.NodeProcesses(cfg, *contextName, flag.Arg(1))
	case "talosconfig-probe":
		// Issue a short-lived read-only config, print only its summary (no key material) and
		// discard it.
		var tc string
		if tc, err = talosmobile.GenerateTalosconfig(cfg, *contextName, "os:reader", 1); err == nil {
			out, err = talosmobile.ParseConfig(tc)
		}
	case "network":
		out, err = talosmobile.NodeNetwork(cfg, *contextName, flag.Arg(1))
	case "connections":
		out, err = talosmobile.NodeConnections(cfg, *contextName, flag.Arg(1))
	case "time":
		out, err = talosmobile.NodeTime(cfg, *contextName, flag.Arg(1))
	case "cluster-time":
		out, err = talosmobile.ClusterTime(cfg, *contextName)
	case "hardware":
		out, err = talosmobile.NodeHardware(cfg, *contextName, flag.Arg(1))
	case "images":
		out, err = talosmobile.NodeImages(cfg, *contextName, flag.Arg(1))
	case "kubespan":
		out, err = talosmobile.KubeSpanStatus(cfg, *contextName)
	case "stats":
		out, err = talosmobile.NodeStats(cfg, *contextName, flag.Arg(1))
	case "etcd":
		out, err = talosmobile.EtcdStatus(cfg, *contextName)
	case "health":
		p := printer{done: make(chan string, 1)}
		talosmobile.StartClusterHealth(cfg, *contextName, p)

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

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")

	return line
}
