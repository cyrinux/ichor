package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// evalResult is what the probe prints of ichorgo.AIEvalRun's answer.
type evalResult struct {
	ID        string   `json:"id"`
	Pass      bool     `json:"pass"`
	Score     float64  `json:"score"`
	Missing   []string `json:"missing"`
	Forbidden []string `json:"forbidden"`
	Words     int      `json:"words"`
	Seconds   float64  `json:"seconds"`
}

var evalCommands = []command{
	{name: "diag-eval", args: "anthropic|openai|device [MODEL] [CASE]", noConfig: true, run: func(e env) (string, error) {
		return noOutput, diagEval(flag.Arg(1), flag.Arg(2), flag.Arg(3), e.minScore)
	}},
}

// diagEval runs the evaluation set, or one case, against a model and prints a table. Below
// minScore (the mean of the scores) it fails. It sends only the set's invented reports.
func diagEval(provider, model, only string, minScore float64) error {
	switch provider {
	case "anthropic", "openai":
	case "device":
		fmt.Println("skipped: the probe has no on-device engine, only the apps run the phone's model")

		return nil
	default:
		return fmt.Errorf("unknown provider %q: anthropic, openai or device", provider)
	}

	ids, err := evalCaseIDs(only)
	if err != nil {
		return err
	}

	table := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "CASE\tPASS\tSCORE\tWORDS\tSECONDS\tDETAIL")

	var total float64

	passed := 0

	for _, id := range ids {
		r, err := runEvalCase(provider, model, id)
		if err != nil {
			fmt.Fprintf(table, "%s\terror\t0.00\t-\t-\t%s\n", id, firstLine(err.Error()))

			continue
		}

		total += r.Score

		if r.Pass {
			passed++
		}

		fmt.Fprintf(table, "%s\t%v\t%.2f\t%d\t%.1f\t%s\n", id, r.Pass, r.Score, r.Words, r.Seconds, r.detail())
	}

	if err := table.Flush(); err != nil {
		return err
	}

	mean := total / float64(len(ids))
	fmt.Printf("total: %d/%d passed, mean score %.2f\n", passed, len(ids), mean)

	if mean < minScore {
		return fmt.Errorf("mean score %.2f is under %.2f", mean, minScore)
	}

	return nil
}

func evalCaseIDs(only string) ([]string, error) {
	if only != "" {
		return []string{only}, nil
	}

	out, err := ichorgo.AIEvalCases()
	if err != nil {
		return nil, err
	}

	var cases []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &cases); err != nil {
		return nil, err
	}

	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = c.ID
	}

	return ids, nil
}

func runEvalCase(provider, model, id string) (evalResult, error) {
	var r evalResult

	out, err := ichorgo.AIEvalRun(provider, aiKey(provider), model, os.Getenv("ICHOR_AI_BASE_URL"), id)
	if err != nil {
		return r, err
	}

	return r, json.Unmarshal([]byte(out), &r)
}

// detail says why a case lost points.
func (r evalResult) detail() string {
	var parts []string
	if len(r.Missing) > 0 {
		parts = append(parts, "missing "+strings.Join(r.Missing, ", "))
	}

	if len(r.Forbidden) > 0 {
		parts = append(parts, "forbidden "+strings.Join(r.Forbidden, ", "))
	}

	return strings.Join(parts, "; ")
}
