package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The probe keeps no sign-in between runs (the AuthStore is in memory): an Omni context
// signs in at start, with OMNI_SERVICE_ACCOUNT_KEY from the environment (never a flag: it
// would show in the shell history), or in the browser with -omni-browser.

var omniCommands = []command{
	// Everything an Omni account gets in the app, read only: sign in (confirm the key at the
	// printed URL), list the clusters, then the first one's nodes and Kubernetes namespaces.
	{"omni-account", "URL EMAIL", func(e env) (string, error) {
		if flag.NArg() != 3 {
			return "", errors.New("usage: omni-account URL EMAIL")
		}

		url, email := flag.Arg(1), flag.Arg(2)

		done := make(chan string, 1)
		ichorgo.StartOmniAccountSignIn(url, email, browserPrompt{done: done})

		if msg := <-done; msg != "" {
			return "", fmt.Errorf("sign in: %s", msg)
		}

		fmt.Fprintln(os.Stderr, "signed in")

		talosconfig, err := ichorgo.DiscoverOmniClusters(url, email)
		if err != nil {
			return "", fmt.Errorf("list the clusters: %w", err)
		}

		summary, err := ichorgo.ParseConfig(talosconfig)
		if err != nil {
			return "", fmt.Errorf("the clusters' talosconfig: %w", err)
		}

		fmt.Println("clusters:", summary)

		var parsed struct {
			Current string `json:"current"`
		}

		_ = json.Unmarshal([]byte(summary), &parsed) //nolint:errcheck

		overview, err := ichorgo.ClusterOverview(talosconfig, parsed.Current)
		fmt.Println("overview:", overview, err)

		namespaces, err := ichorgo.KubeNamespaces(talosconfig, parsed.Current, "")
		fmt.Println("kubernetes namespaces:", namespaces, err)

		return noOutput, nil
	}},
	{"omni-clusters", "", func(e env) (string, error) {
		endpoint, err := omniEndpoint(e.cfg, e.context)
		if err != nil {
			return "", err
		}

		key := os.Getenv("OMNI_SERVICE_ACCOUNT_KEY")
		if key == "" {
			return "", errors.New("set OMNI_SERVICE_ACCOUNT_KEY to list the clusters of a service account")
		}

		identity, err := ichorgo.SetOmniServiceAccount(endpoint, key)
		if err != nil {
			return "", err
		}

		return ichorgo.DiscoverOmniClusters(endpoint, identity)
	}},
}

// omniEndpoint is the Omni endpoint of the named context (the current one when empty).
func omniEndpoint(cfg, contextName string) (string, error) {
	out, err := ichorgo.ParseConfig(cfg)
	if err != nil {
		return "", err
	}

	var summary struct {
		Current  string `json:"current"`
		Contexts []struct {
			Name      string   `json:"name"`
			Endpoints []string `json:"endpoints"`
			Omni      bool     `json:"omni"`
		} `json:"contexts"`
	}

	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		return "", err
	}

	if contextName == "" {
		contextName = summary.Current
	}

	for _, c := range summary.Contexts {
		if c.Name == contextName && c.Omni && len(c.Endpoints) > 0 {
			return c.Endpoints[0], nil
		}
	}

	return "", fmt.Errorf("context %q is not an Omni context", contextName)
}

// omniSignIn signs an Omni context in before a command: with the service account key from
// the environment, or in the browser when asked. A certificate context is left alone.
func omniSignIn(cfg, contextName string, browser bool) error {
	if ichorgo.IsKubeconfig(cfg) {
		return nil
	}

	info, err := ichorgo.TalosSignInInfo(cfg, contextName)
	if err != nil || info == "" {
		return err
	}

	if key := os.Getenv("OMNI_SERVICE_ACCOUNT_KEY"); key != "" {
		secrets, _ := json.Marshal(map[string]string{"serviceAccountKey": key}) //nolint:errcheck

		return ichorgo.TalosSetCredentials(cfg, contextName, string(secrets))
	}

	if !browser {
		return nil
	}

	done := make(chan string, 1)
	ichorgo.StartTalosSignIn(cfg, contextName, browserPrompt{done: done})

	if msg := <-done; msg != "" {
		return errors.New(msg)
	}

	return nil
}

// browserPrompt prints the page to confirm the key on.
type browserPrompt struct{ done chan string }

func (b browserPrompt) OnPrompt(json string) {
	var p struct {
		URL string `json:"url"`
	}

	if err := jsonUnmarshal(json, &p); err == nil {
		fmt.Fprintf(os.Stderr, "Confirm the key in Omni: %s\n", p.URL)
	}
}

func (b browserPrompt) OnDone(errMessage string) { b.done <- errMessage }

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
