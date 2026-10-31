package ichorgo

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// The evaluation set of the AI prompts: invented reports with what a good answer must and must
// not say, so a prompt or model change gets a number instead of a look. The probe runs it
// (`just ai-eval`); the apps never do. No cluster is read.
//
//go:embed testdata/ai_eval/*.yaml
var aiEvalFiles embed.FS

// aiEvalTerm is what must appear in an answer: one word, or a list of synonyms of which any
// will do. Matched without regard to case.
type aiEvalTerm []string

// UnmarshalJSON takes a string or a list of strings.
func (t *aiEvalTerm) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*t = aiEvalTerm{one}

		return nil
	}

	var anyOf []string
	if err := json.Unmarshal(raw, &anyOf); err != nil {
		return err
	}

	*t = anyOf

	return nil
}

func (t aiEvalTerm) label() string { return strings.Join(t, "|") }

func (t aiEvalTerm) foundIn(lower string) bool {
	return slices.ContainsFunc(t, func(word string) bool { return strings.Contains(lower, strings.ToLower(word)) })
}

// aiEvalCase is one file of the set.
type aiEvalCase struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`     // the kind of report: aiEvalFull or aiEvalHealth
	Language string `json:"language"` // "" for English
	Report   string `json:"report"`
	Note     string `json:"note"`
	Expect   struct {
		Must     []aiEvalTerm `json:"must"`
		MustNot  []string     `json:"mustNot"`
		MaxWords int          `json:"maxWords"` // 0 for no limit
	} `json:"expect"`
}

const (
	aiEvalFull   = "full"
	aiEvalHealth = "health"
)

// aiEvalKinds maps a case's kind to the instructions it is sent with.
var aiEvalKinds = map[string]diagnosisKind{aiEvalFull: diagnosisFull, aiEvalHealth: diagnosisHealth}

// aiEvalResult is the grade of one answer. Score is the share of the must terms found, 0 when
// the answer holds something forbidden or is too long; Pass means a score of 1.
type aiEvalResult struct {
	ID        string   `json:"id"`
	Pass      bool     `json:"pass"`
	Score     float64  `json:"score"`
	Missing   []string `json:"missing"`
	Forbidden []string `json:"forbidden"`
	Words     int      `json:"words"`
	Seconds   float64  `json:"seconds"`
	Answer    string   `json:"answer"`
}

func loadAIEvalCases() ([]aiEvalCase, error) {
	names, err := fs.Glob(aiEvalFiles, "testdata/ai_eval/*.yaml")
	if err != nil {
		return nil, err
	}

	cases := make([]aiEvalCase, 0, len(names))

	for _, name := range names {
		raw, err := aiEvalFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}

		var c aiEvalCase
		if err := yaml.UnmarshalStrict(raw, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		if err := c.validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		cases = append(cases, c)
	}

	return cases, nil
}

func (c aiEvalCase) validate() error {
	if _, ok := aiEvalKinds[c.Kind]; !ok {
		return fmt.Errorf("unknown kind %q", c.Kind)
	}

	switch {
	case c.ID == "":
		return errors.New("no id")
	case strings.TrimSpace(c.Report) == "":
		return errors.New("no report")
	case len(c.Expect.Must) == 0 && len(c.Expect.MustNot) == 0:
		return errors.New("nothing to grade: neither must nor mustNot")
	}

	return nil
}

// diagnosis is the report as the apps would hold it, not anonymized.
func (c aiEvalCase) diagnosis() *Diagnosis {
	d := &Diagnosis{report: c.Report, kind: aiEvalKinds[c.Kind]}
	d.screenshotMode, d.screenshotResets = privacy.state()

	return d
}

func (c aiEvalCase) language() string {
	if c.Language == "" {
		return "en"
	}

	return c.Language
}

// grade scores an answer against the case.
func (c aiEvalCase) grade(answer string, seconds float64) aiEvalResult {
	r := aiEvalResult{
		ID: c.ID, Missing: []string{}, Forbidden: []string{}, Score: 1,
		Words: len(strings.Fields(answer)), Seconds: seconds, Answer: answer,
	}
	lower := strings.ToLower(answer)

	for _, term := range c.Expect.Must {
		if !term.foundIn(lower) {
			r.Missing = append(r.Missing, term.label())
		}
	}

	for _, word := range c.Expect.MustNot {
		if strings.Contains(lower, strings.ToLower(word)) {
			r.Forbidden = append(r.Forbidden, word)
		}
	}

	if n := len(c.Expect.Must); n > 0 {
		r.Score = float64(n-len(r.Missing)) / float64(n)
	}

	if len(r.Forbidden) > 0 || (c.Expect.MaxWords > 0 && r.Words > c.Expect.MaxWords) {
		r.Score = 0
	}

	r.Pass = r.Score == 1

	return r
}

// AIEvalCases lists the evaluation set: [{id, kind}], by id.
func AIEvalCases() (out string, err error) {
	defer maskResult(&out, &err)

	cases, err := loadAIEvalCases()
	if err != nil {
		return "", err
	}

	type row struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}

	rows := make([]row, len(cases))
	for i, c := range cases {
		rows[i] = row{c.ID, c.Kind}
	}

	return toJSON(rows)
}

// aiEvalAnswer waits for a whole answer.
type aiEvalAnswer struct {
	text string
	done chan string
}

func (a *aiEvalAnswer) OnAnswer(text string)                    { a.text = text }
func (a *aiEvalAnswer) OnTool(_, _ string)                      {}
func (a *aiEvalAnswer) OnToolDone(_ string, _ int, _, _ string) {}
func (a *aiEvalAnswer) OnDone(errMessage string)                { a.done <- errMessage }

// AIEvalRun sends one case to a model through the same path as a diagnosis and grades the
// answer: {id, pass, score, missing, forbidden, words, seconds, answer}. provider, apiKey,
// model and baseURL: see Diagnosis.Ask. An error means the model gave no answer to grade.
func AIEvalRun(provider, apiKey, model, baseURL, caseID string) (out string, err error) {
	defer maskResult(&out, &err)

	cases, err := loadAIEvalCases()
	if err != nil {
		return "", err
	}

	i := slices.IndexFunc(cases, func(c aiEvalCase) bool { return c.ID == caseID })
	if i < 0 {
		return "", fmt.Errorf("no evaluation case %q", caseID)
	}

	c := cases[i]
	answer := &aiEvalAnswer{done: make(chan string, 1)}
	start := time.Now()

	c.diagnosis().Ask(provider, apiKey, model, baseURL, c.language(), c.Note, false, answer)

	if msg := <-answer.done; msg != "" {
		return "", errors.New(msg)
	}

	return toJSON(c.grade(answer.text, time.Since(start).Seconds()))
}
