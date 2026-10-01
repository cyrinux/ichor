// Command probe exercises the talosmobile API against a real cluster from the desktop.
//
//	go run ./cmd/probe [-config ~/.talos/config] [-context name] overview|services NODE|resources NODE|logs NODE SERVICE|dmesg NODE|kubeconfig|etcd|health|parse
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

func (p printer) OnProgress(node, message string) { fmt.Printf("[%s] %s\n", node, message) }
func (p printer) OnDone(errMessage string)        { p.done <- errMessage }

func main() {
	home, _ := os.UserHomeDir() //nolint:errcheck
	configPath := flag.String("config", filepath.Join(home, ".talos", "config"), "talosconfig path")
	contextName := flag.String("context", "", "context name (default: current)")
	flag.Parse()

	if flag.NArg() == 0 {
		fail(fmt.Errorf("usage: probe [flags] overview|services NODE|resources NODE|logs NODE SERVICE|dmesg NODE|stats NODE|kubespan|kubeconfig|etcd|health|parse"))
	}

	raw, err := os.ReadFile(*configPath)
	if err != nil {
		fail(err)
	}

	cfg := string(raw)

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
	case "kubeconfig":
		// Never print the credential itself.
		var kc string
		if kc, err = talosmobile.Kubeconfig(cfg, *contextName); err == nil {
			out = fmt.Sprintf("kubeconfig: %d bytes, starts with %q", len(kc), firstLine(kc))
		}
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
