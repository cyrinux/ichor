package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// probeKubeSignIn signs a kubeconfig context (CONTEXT, else -context) in through its browser
// method, as the apps' sign-in sheet does: open the printed URL on this machine, the sign-in
// comes back to its loopback address. Prints how the context is then signed in; the sign-in
// itself lives for this run only.
func probeKubeSignIn(e env) (string, error) {
	name := cmp.Or(flag.Arg(1), e.context)
	if name == "" {
		var summary struct {
			Current string `json:"current"`
		}

		out, err := ichorgo.ParseKubeconfig(e.cfg)
		if err != nil {
			return "", err
		}

		_ = json.Unmarshal([]byte(out), &summary)
		name = summary.Current
	}

	listener := &probeSignInListener{done: make(chan string, 1)}

	ichorgo.StartKubeSignIn(e.cfg, name, listener)

	if msg := <-listener.done; msg != "" {
		return "", errors.New(msg)
	}

	return ichorgo.KubeSignInInfo(e.cfg, name)
}

type probeSignInListener struct{ done chan string }

func (l *probeSignInListener) OnPrompt(prompt string) {
	var p struct {
		URL      string `json:"url"`
		UserCode string `json:"userCode"`
	}

	_ = json.Unmarshal([]byte(prompt), &p)

	if p.UserCode != "" {
		fmt.Fprintln(os.Stderr, "code:", p.UserCode)
	}

	fmt.Fprintln(os.Stderr, "open in a browser:", p.URL)
}

func (l *probeSignInListener) OnDone(errMessage string) { l.done <- errMessage }
