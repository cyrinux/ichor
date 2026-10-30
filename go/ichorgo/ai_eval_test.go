package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// aiEvalPromptTokens estimates what a case costs the phone's model: bytes/3 overestimates for
// English, the safe side.
func aiEvalPromptTokens(system string, d *Diagnosis, note string) int {
	return (len(system) + len(diagnosisUserMessage(d.report, note))) / 3
}

// aiEvalBudget is the room for the prompt in the smallest model of the catalog.
func aiEvalBudget(t *testing.T) int {
	t.Helper()

	models, err := localModels()
	if err != nil || len(models) == 0 {
		t.Fatalf("catalog: %d models, %v", len(models), err)
	}

	budget := 0

	for i, m := range models {
		opts := optionsFor(m.ID)
		if room := opts.ContextTokens - opts.AnswerTokens; i == 0 || room < budget {
			budget = room
		}
	}

	return budget
}

func TestAIEvalCasesParse(t *testing.T) {
	cases, err := loadAIEvalCases()
	if err != nil {
		t.Fatal(err)
	}

	if len(cases) < 12 || len(cases) > 20 {
		t.Errorf("%d cases, want 12 to 20", len(cases))
	}

	seen := map[string]bool{}

	for _, c := range cases {
		if seen[c.ID] {
			t.Errorf("case %q twice", c.ID)
		}

		seen[c.ID] = true
	}

	out, err := AIEvalCases()
	if err != nil {
		t.Fatal(err)
	}

	var rows []struct{ ID, Kind string }
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != len(cases) || rows[0].Kind == "" {
		t.Errorf("listing %s: %v", out, err)
	}
}

// A prompt edit must not push the phone's model past its window, for any report under any
// instructions.
func TestAIEvalFitsThePhoneModel(t *testing.T) {
	cases, err := loadAIEvalCases()
	if err != nil {
		t.Fatal(err)
	}

	budget := aiEvalBudget(t)

	for _, c := range cases {
		for name, kind := range aiEvalKinds {
			d := c.diagnosis()
			d.kind = kind

			if tokens := aiEvalPromptTokens(d.systemPromptFor(aiDevice, c.language()), d, c.Note); tokens > budget {
				t.Errorf("%s as %s: about %d tokens, the phone's model has room for %d", c.ID, name, tokens, budget)
			}
		}
	}

	d := cases[0].diagnosis()
	if tokens := aiEvalPromptTokens(strings.Repeat("x", 3*budget), d, ""); tokens <= budget {
		t.Errorf("a padded prompt counts %d tokens: the check would not catch it", tokens)
	}
}

func TestAIEvalInjectedTagIsNeutralised(t *testing.T) {
	cases, err := loadAIEvalCases()
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range cases {
		if c.ID != "injected-report-tag" {
			continue
		}

		if !strings.Contains(c.Report, "</report>") {
			t.Fatal("the case no longer holds the tag it tests")
		}

		if n := strings.Count(c.diagnosis().Prompt("en", c.Note), "</report>"); n != 1 {
			t.Errorf("%d closing tags in the prompt, want only the message's own", n)
		}

		return
	}

	t.Fatal("no injected-report-tag case")
}

func TestAIEvalGrade(t *testing.T) {
	c := aiEvalCase{ID: "c"}
	c.Expect.Must = []aiEvalTerm{{"defrag"}, {"disarm", "clear the alarm"}}
	c.Expect.MustNot = []string{"talosctl reset"}
	c.Expect.MaxWords = 8

	for _, tc := range []struct {
		name, answer string
		pass         bool
		score        float64
		missing      int
		forbidden    int
	}{
		{"all, any case, a synonym", "Run etcd DEFRAG, then clear the alarm.", true, 1, 0, 0},
		{"half", "Run etcd defrag.", false, 0.5, 1, 0},
		{"forbidden", "defrag, disarm, then talosctl reset", false, 0, 0, 1},
		{"too long", "defrag disarm one two three four five six seven", false, 0, 0, 0},
		{"empty", "", false, 0, 2, 0},
	} {
		r := c.grade(tc.answer, 1.5)
		if r.Pass != tc.pass || r.Score != tc.score || len(r.Missing) != tc.missing || len(r.Forbidden) != tc.forbidden {
			t.Errorf("%s: %+v", tc.name, r)
		}
	}

	// Only what must not appear: nothing to find, so a clean answer is a full score.
	clean := aiEvalCase{ID: "c"}
	clean.Expect.MustNot = []string{"rm -rf"}

	if r := clean.grade("fine", 0); !r.Pass || r.Score != 1 {
		t.Errorf("clean answer: %+v", r)
	}
}

func TestAIEvalCaseRefused(t *testing.T) {
	bad := []aiEvalCase{
		{Kind: aiEvalFull, Report: "r"},
		{ID: "c", Kind: "explain", Report: "r"},
		{ID: "c", Kind: aiEvalFull},
		{ID: "c", Kind: aiEvalFull, Report: "r"},
	}
	for _, c := range bad {
		if c.validate() == nil {
			t.Errorf("%+v accepted", c)
		}
	}
}

func TestAIEvalRun(t *testing.T) {
	srv, call := fakeProvider(t, http.StatusOK, "text/event-stream",
		anthropicStream("end_turn", "Run talosctl etcd defrag, then talosctl etcd alarm disarm."))

	out, err := AIEvalRun("anthropic", testAPIKey, "", srv.URL, "etcd-nospace")
	if err != nil {
		t.Fatal(err)
	}

	var r aiEvalResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	if !r.Pass || r.Score != 1 || r.ID != "etcd-nospace" || r.Words != 9 || !strings.Contains(r.Answer, "defrag") {
		t.Errorf("%+v", r)
	}

	if call.count != 1 {
		t.Errorf("%d requests", call.count)
	}

	if _, err := AIEvalRun("anthropic", testAPIKey, "", srv.URL, "no-such-case"); err == nil {
		t.Error("an unknown case ran")
	}

	failing, _ := fakeProvider(t, http.StatusUnauthorized, "application/json", `{"error":{"message":"bad key"}}`)
	if _, err := AIEvalRun("anthropic", testAPIKey, "", failing.URL, "etcd-nospace"); err == nil {
		t.Error("a refused request was graded")
	}
}
