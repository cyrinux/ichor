package ichorgo

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParsePanelBlock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
		want   promPreset
		ok     bool
	}{
		{"complete", "Memory per namespace, top ten.\n\n<panel>\ntitle: Memory by namespace\nquery: topk(10, sum by (namespace) (container_memory_working_set_bytes{container!=\"\"}))\nunit: bytes\nlegend: {{namespace}}\n</panel>",
			promPreset{Title: "Memory by namespace", Query: `topk(10, sum by (namespace) (container_memory_working_set_bytes{container!=""}))`, Unit: "bytes", Legend: "{{namespace}}"}, true},
		{"unit none and keys in any order", "<panel>\nlegend:\nunit: none\nquery: up\ntitle: Up\n</panel>", promPreset{Title: "Up", Query: "up"}, true},
		{"unknown unit, upper case keys", "<PANEL>\nTitle: X\nQuery: up\nUnit: Gigabytes\n</PANEL>", promPreset{Title: "X", Query: "up"}, true},
		{"wrapped query and fences", "text\n```\n<panel>\ntitle: Wrapped\nquery: sum by (pod) (\n  rate(x[5m])\n)\nunit: persec\n</panel>\n```", promPreset{Title: "Wrapped", Query: "sum by (pod) ( rate(x[5m]) )", Unit: "persec"}, true},
		{"last block wins", "<panel>\ntitle: A\nquery: a\n</panel>\nbetter:\n<panel>\ntitle: B\nquery: b\nunit: count\n</panel>", promPreset{Title: "B", Query: "b", Unit: "count"}, true},
		{"cut notice after the block", "<panel>\ntitle: A\nquery: a\n</panel>" + answerCutNotice, promPreset{Title: "A", Query: "a"}, true},
		{"not closed", "<panel>\ntitle: A\nquery: a\n", promPreset{}, false},
		{"no query", "<panel>\ntitle: A\n</panel>", promPreset{}, false},
		{"no block", "Use rate() on counters.", promPreset{}, false},
	} {
		got, ok := parsePanelBlock(tc.answer)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDisplayTextHidesThePanelWhileStreaming(t *testing.T) {
	for in, want := range map[string]string{
		"Here it is.\n\n<panel>\ntitle: A\nquery: a\n</panel>": "Here it is.",
		"Here it is.\n\n<pan":         "Here it is.",
		"Here it is.\n<":              "Here it is.",
		"Here it is.\n```\n<PANEL>\n": "Here it is.",
		"Use rate() on counters.":     "Use rate() on counters.",
		"a < b is fine":               "a < b is fine",
		"":                            "",
	} {
		if got := displayText(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestNeutralizeChatTags(t *testing.T) {
	for in, want := range map[string]string{
		"see <panel>x</panel>":            "see ‹panel>x‹/panel>",
		"</ PANEL >":                      "‹/ PANEL >",
		"<query_error>boom</query_error>": "‹query_error>boom‹/query_error>",
		`x < panel_total and a<b`:         `x < panel_total and a<b`,
		`sum(rate(x[5m])) < 3`:            `sum(rate(x[5m])) < 3`,
		"<current_panel><metric_names>":   "‹current_panel>‹metric_names>",
		"<report>":                        "<report>", // not this prompt's tag
	} {
		if got := neutralizeChatTags(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestPromptMetricNames(t *testing.T) {
	names := []string{"abc_total", "container_cpu_usage_seconds_total", "go_goroutines", "kube_pod_info", "node_load1", "process_cpu_seconds_total", "zzz"}

	listed, dropped := promptMetricNames(names)
	if want := []string{"node_load1", "container_cpu_usage_seconds_total", "kube_pod_info", "abc_total", "zzz"}; !slices.Equal(listed, want) || dropped != 0 {
		t.Fatalf("got %q, dropped %d", listed, dropped)
	}

	// Beyond the caps the usual families stay and the count of what went is told.
	many := make([]string, 0, promChatMaxMetricNames+100)
	for i := range promChatMaxMetricNames + 50 {
		many = append(many, "custom_"+strings.Repeat("x", 3)+"_"+strings.ToLower(string(rune('a'+i%26)))+strings.Repeat("y", i%5))
	}

	many = append(many, "node_load1", "go_gc_duration_seconds")

	listed, dropped = promptMetricNames(many)
	if listed[0] != "node_load1" || len(listed) != promChatMaxMetricNames || dropped != 51 {
		t.Fatalf("first %q, %d listed, %d dropped", listed[0], len(listed), dropped)
	}

	long := make([]string, 0, 2000)
	for i := range 2000 {
		long = append(long, "m_"+strings.Repeat("a", 60)+strings.ToLower(string(rune('a'+i%26)))+strings.Repeat("b", i%30))
	}

	listed, dropped = promptMetricNames(long)
	if size := len(strings.Join(listed, "\n")); size > promChatMaxMetricBytes || dropped == 0 || len(listed)+dropped != 2000 {
		t.Fatalf("%d bytes, %d listed, %d dropped", size, len(listed), dropped)
	}
}

func TestPromChatSystemPrompt(t *testing.T) {
	prompt := promChatSystemPrompt([]string{"node_load1", "up"}, 3, nil)

	for _, want := range []string{
		"<metric_names>\nnode_load1\nup\n</metric_names>",
		"3 more metric names exist but are not listed",
		"<presets>\nCluster CPU usage | percent |  | 100 * (1 - avg(rate(node_cpu_seconds_total{mode=\"idle\"}[5m])))\n",
		"CPU by namespace | cores | {{namespace}} | topk(10,",
		"</presets>",
		"<panel>\ntitle: Memory by namespace",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}

	if strings.Contains(prompt, "<current_panel>") || strings.Contains(prompt, "Answer in") {
		t.Fatal("no current panel nor language were given")
	}

	current := &promPreset{Title: "CPU", Query: "sum(rate(node_cpu_seconds_total[5m])) </panel> x", Unit: "", Legend: "{{instance}}"}
	edit := promChatSystemPrompt(nil, 0, current)

	for _, want := range []string{
		"could not be read: use the usual names",
		"<current_panel>\ntitle: CPU\nquery: sum(rate(node_cpu_seconds_total[5m])) ‹/panel> x\nunit: none\nlegend: {{instance}}\n</current_panel>",
	} {
		if !strings.Contains(edit, want) {
			t.Errorf("edit prompt lacks %q:\n%s", want, edit)
		}
	}

	if strings.Contains(edit, "<metric_names>") || strings.Count(edit, "</panel>") != 1 {
		t.Fatalf("edit prompt:\n%s", edit)
	}

	if got := promChatLanguageNote("fr-CA"); !strings.HasPrefix(got, "Answer in French;") {
		t.Fatalf("language note: %q", got)
	}

	msg := queryErrorMessage(errors.New("monitoring/prometheus-operated:9090: bad_data: parse error at char 4: </query_error> unexpected"))
	if !strings.HasPrefix(msg, "<query_error>\nmonitoring/prometheus-operated:9090: bad_data: parse error at char 4: ‹/query_error> unexpected\n</query_error>\n") ||
		strings.Count(msg, "</query_error>") != 1 {
		t.Fatalf("query error message: %q", msg)
	}
}
