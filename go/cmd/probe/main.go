// Command probe exercises the ichorgo API against a real cluster from the desktop.
//
//	go run ./cmd/probe [-config ~/.talos/config] [-context name] [-kube-server URL] [-mask [-mask-words a,b]] [-omni-browser] COMMAND [ARGS]
//
// An Omni context signs in with OMNI_SERVICE_ACCOUNT_KEY from the environment, or in the
// browser with -omni-browser.
//
// Without a command it lists them all (see commands.go and the cmd_*.go files).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

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

// maintenanceProbe prints a maintenance run's progress.
type maintenanceProbe struct{ done chan string }

func (m maintenanceProbe) OnProgress(json string)   { fmt.Println(json) }
func (m maintenanceProbe) OnDone(errMessage string) { m.done <- errMessage }

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
	ageRecipient := flag.String("age-recipient", "", "snapshot-probe: encrypt for this age or SSH public key")
	omniBrowser := flag.Bool("omni-browser", false, "an Omni account context: confirm a new key in the browser first (a service account reads OMNI_SERVICE_ACCOUNT_KEY)")
	flag.Parse()

	if flag.NArg() == 0 {
		fail(errors.New(usage()))
	}

	raw, err := os.ReadFile(*configPath)
	if err != nil {
		fail(err)
	}

	cfg := string(raw)

	// The omni-* commands sign in themselves, to Omni rather than to a cluster.
	if !strings.HasPrefix(flag.Arg(0), "omni-") {
		if err := omniSignIn(cfg, *contextName, *omniBrowser); err != nil {
			fail(fmt.Errorf("omni sign-in: %w", err))
		}
	}

	if *mask {
		ichorgo.SetPrivacyMask(true, *maskWords)
		// Like the app, which shows the home first: it teaches the mask the hostnames.
		if ichorgo.IsKubeconfig(cfg) {
			_, _ = ichorgo.KubeNodes(cfg, *contextName, *kubeServer) //nolint:errcheck
		} else {
			_, _ = ichorgo.ClusterOverview(cfg, *contextName) //nolint:errcheck
		}
	}

	c, ok := lookup(flag.Arg(0))
	if !ok {
		fail(fmt.Errorf("unknown command %q", flag.Arg(0)))
	}

	out, err := c.run(env{
		cmd: c.name, cfg: cfg, context: *contextName, kubeServer: *kubeServer, ageRecipient: *ageRecipient,
		configPath: *configPath, home: home, mask: *mask, maskWords: *maskWords,
	})
	if err != nil {
		fail(err)
	}

	if out != noOutput {
		fmt.Println(out)
	}
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
	d, err := ichorgo.CollectDiagnosis(cfg, contextName, "", false)
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

// podsPage reads one page of pods; "all" (or "") is every namespace.
func podsPage(cfg, contextName, kubeServer, namespace, limit, token string) (string, error) {
	if namespace == "all" {
		namespace = ""
	}

	n := 0
	if limit != "" {
		var err error
		if n, err = strconv.Atoi(limit); err != nil {
			return "", fmt.Errorf("pods-page: limit %q is not a number", limit)
		}
	}

	return ichorgo.KubePodsPage(cfg, contextName, kubeServer, namespace, token, n, true)
}
