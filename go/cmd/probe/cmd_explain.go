package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// "Explain with AI" for one pod, alert or checkup finding: the report it would send, and the answer.
var explainCommands = []command{
	{name: "explain-report", args: "pod NAMESPACE NAME|alert SOURCE_JSON FINGERPRINT|finding FINDING_JSON", run: func(e env) (out string, err error) {
		// What "Explain with AI" would send, anonymized; nothing is sent.
		var d *ichorgo.Diagnosis
		if d, _, err = collectExplanation(e, 1, true); err == nil {
			out = d.Report()
		}

		return out, err
	}},
	{name: "explain", args: "anthropic|openai pod NAMESPACE NAME|alert SOURCE_JSON FINGERPRINT|finding FINDING_JSON [MODEL]", run: func(e env) (string, error) {
		return noOutput, explain(e, flag.Arg(1))
	}},
}

// collectExplanation reads the subject from the arguments from first on: its kind, then two
// arguments for a pod or an alert and one for a finding (the finding as the checkup lists it).
// next is the index of the argument after them.
func collectExplanation(e env, first int, anonymize bool) (d *ichorgo.Diagnosis, next int, err error) {
	kind := flag.Arg(first)

	var subject any

	switch kind {
	case "pod":
		subject, next = map[string]string{"namespace": flag.Arg(first + 1), "name": flag.Arg(first + 2)}, first+3
	case "alert":
		subject, next = map[string]string{"source": flag.Arg(first + 1), "fingerprint": flag.Arg(first + 2)}, first+3
	case "finding":
		if !json.Valid([]byte(flag.Arg(first + 1))) {
			return nil, 0, fmt.Errorf("the finding is not JSON: %q", flag.Arg(first+1))
		}

		subject, next = map[string]json.RawMessage{"finding": json.RawMessage(flag.Arg(first + 1))}, first+2
	default:
		return nil, 0, fmt.Errorf("unknown subject %q: pod, alert or finding", kind)
	}

	data, err := json.Marshal(subject)
	if err != nil {
		return nil, 0, err
	}

	d, err = ichorgo.CollectExplanation(e.cfg, e.context, e.kubeServer, kind, string(data), anonymize)

	return d, next, err
}

// explain asks about one subject and prints the answer. Like diagnose, it sends cluster data
// (real names) to the provider.
func explain(e env, provider string) error {
	d, next, err := collectExplanation(e, 2, false)
	if err != nil {
		return err
	}

	p := &answerPrinter{done: make(chan string, 1)}
	d.Ask(provider, aiKey(provider), flag.Arg(next), os.Getenv("ICHOR_AI_BASE_URL"), "en", "", false, p)

	if msg := <-p.done; msg != "" {
		return fmt.Errorf("explanation failed: %s", msg)
	}

	return nil
}
