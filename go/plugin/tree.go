package plugin

import (
	"strings"
	"time"
)

// The render tree is what KubePluginData returns and the apps draw (Plan §6, docs/plugins.md
// "Render tree"): every value is resolved, every tone is a word, every label is translated.
// The apps evaluate nothing.

// Tree is one plugin rendered over one cluster (or its samples).
type Tree struct {
	Plugin      TreePlugin `json:"plugin"`
	Active      bool       `json:"active"`
	DetectedVia string     `json:"detectedVia"`
	APIVersion  string     `json:"apiVersion"`
	Health      string     `json:"health"`
	// Partial is set when a resource held more objects than its list.limit: the counts and
	// the health cover only the first ones.
	Partial  bool        `json:"partial"`
	Summary  TreeSummary `json:"summary"`
	Cards    []TreeCard  `json:"cards"`
	Findings []TreeFind  `json:"findings"`
	Errors   []TreeError `json:"errors"`
}

// TreePlugin says which plugin the tree is.
type TreePlugin struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Category string `json:"category"`
	Version  string `json:"version"`
	Screen   string `json:"screen"`
	Website  string `json:"website,omitempty"`
}

// TreeSummary is the tile and the hub header.
type TreeSummary struct {
	Title  string `json:"title"`
	Health string `json:"health"`
	// Partial: see Tree.Partial; the tile says its counts are a lower bound.
	Partial bool        `json:"partial"`
	Badges  []TreeBadge `json:"badges"`
}

// TreeBadge is a chip.
type TreeBadge struct {
	Label string `json:"label"`
	Value string `json:"value,omitempty"`
	Tone  string `json:"tone"`
}

// TreeCard is a list card: one row per object or data service element.
type TreeCard struct {
	ID    string    `json:"id"`
	Type  string    `json:"type"`
	Empty string    `json:"empty,omitempty"`
	Rows  []TreeRow `json:"rows"`
}

// Ref names an object the apps can open.
type Ref struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// TreeRow is one row; Ref and Health are set on a row of objects.
type TreeRow struct {
	Ref      *Ref          `json:"ref,omitempty"`
	Title    string        `json:"title"`
	Subtitle string        `json:"subtitle,omitempty"`
	Status   *TreeStatus   `json:"status,omitempty"`
	Trailing *TreeTrailing `json:"trailing,omitempty"`
	Health   *TreeHealth   `json:"health,omitempty"`
	Detail   []TreeSection `json:"detail,omitempty"`
}

// TreeStatus is a row's colored status.
type TreeStatus struct {
	Tone string `json:"tone"`
	Text string `json:"text"`
}

// TreeTrailing is the text at a row's end.
type TreeTrailing struct {
	Text string `json:"text"`
}

// TreeHealth is an object's health and why.
type TreeHealth struct {
	Tone    string `json:"tone"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// TreeSection is a detail primitive; which keys are set depends on Type (keyValues, badges,
// gauge, conditions, relations, table, text, actions). Items are []TreeItem (keyValues,
// badges, relations), []TreeCondItem (conditions) or []TreeAction (actions).
type TreeSection struct {
	Type    string       `json:"type"`
	Items   any          `json:"items,omitempty"`
	Label   string       `json:"label,omitempty"`
	Value   *float64     `json:"value,omitempty"`
	Max     *float64     `json:"max,omitempty"`
	Unit    string       `json:"unit,omitempty"`
	Tone    string       `json:"tone,omitempty"`
	Text    string       `json:"text,omitempty"`
	Mono    bool         `json:"mono,omitempty"`
	Columns []TreeColumn `json:"columns,omitempty"`
	Rows    [][]string   `json:"rows,omitempty"`
}

// TreeItem is a keyValues, badges or relations entry.
type TreeItem struct {
	Label string `json:"label"`
	Value string `json:"value,omitempty"`
	Tone  string `json:"tone,omitempty"`
	Mono  bool   `json:"mono,omitempty"`
	Ref   *Ref   `json:"ref,omitempty"`
	Text  string `json:"text,omitempty"`
}

// TreeColumn is a table's column.
type TreeColumn struct {
	Label string `json:"label"`
}

// TreeCondItem is one status condition.
type TreeCondItem struct {
	Type           string `json:"type"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Tone           string `json:"tone"`
	LastTransition int64  `json:"lastTransition"`
}

// TreeAction is a button. Allowed (the can-I answer of its action's rbac) is not the
// render's: KubePluginData sets it (Tree.SetAllowed); absent, the answer is unknown.
type TreeAction struct {
	ID             string      `json:"id"`
	Label          string      `json:"label"`
	Enabled        bool        `json:"enabled"`
	Confirm        string      `json:"confirm"`
	DisabledReason string      `json:"disabledReason,omitempty"`
	Allowed        *bool       `json:"allowed,omitempty"`
	Params         []TreeParam `json:"params,omitempty"`
}

// TreeParam is an action's typed input.
type TreeParam struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Values  []string `json:"values,omitempty"`
	Default any      `json:"default,omitempty"`
}

// TreeFind is a finding raised by one object.
type TreeFind struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Ref      *Ref   `json:"ref,omitempty"`
	Opens    string `json:"opens,omitempty"`
}

// TreeError is a resource that could not be read (forbidden, not served) or a value that
// could not be evaluated: the rest of the tree still renders.
type TreeError struct {
	Resource string `json:"resource,omitempty"`
	Where    string `json:"where,omitempty"`
	Message  string `json:"message"`
}

// Detection says whether and how the plugin was found on the cluster.
type Detection struct {
	Active bool
	// Via is api, pods, service or demo; "" when not active.
	Via string
}

// Input is what Render evaluates: the objects listed per resource key, the version each
// resource was read at, and how the plugin was detected.
type Input struct {
	Objects  map[string][]map[string]any
	Detected Detection
	// APIVersions are the versions each resource key was read at (version: preferred
	// resolved); a resource missing here uses its declared version.
	APIVersions map[string]string
	// Errors are the read errors, carried into the tree.
	Errors []TreeError
	// Partial: a list stopped at its limit (see Tree.Partial).
	Partial bool
	// Locale picks the i18n map ("fr", "pt-BR"); "" or a missing one is English.
	Locale string
	// Heuristics read the health of an object no rule covers, and a condition's tone; nil
	// leaves such objects ok and their conditions unknown.
	Heuristics Heuristics
}

// Heuristics are the generic health readings of the Go core (objectHealth, conditionTone),
// given by the caller so that the plugin package stays free of the Kubernetes client.
type Heuristics interface {
	// ObjectHealth is an object's tone and what it says, from its conditions and phase.
	ObjectHealth(obj map[string]any) (tone, reason string)
	// ConditionTone is one status condition's tone.
	ConditionTone(cond map[string]any) string
}

// SamplesInput is the demo input: every resource's samples, detected as demo, their relative
// times ("now-6h") resolved against now (see sampleTimes).
func SamplesInput(c *Compiled, locale string, h Heuristics, now time.Time) Input {
	objects := make(map[string][]map[string]any, len(c.Plugin.Samples))
	for res, list := range c.Plugin.Samples {
		objects[res] = sampleTimes(list, now)
	}

	return Input{Objects: objects, Detected: Detection{Active: true, Via: "demo"}, Locale: locale, Heuristics: h}
}

// healthRank orders the tones for worst() and sorting: ok < idle < info < unknown < warning < critical.
var healthRank = map[string]int{"ok": 0, "idle": 1, "info": 2, "unknown": 3, "warning": 4, "critical": 5}

// worse is the worse of two tones; an unknown word counts as unknown.
func worse(a, b string) string {
	if rank(b) > rank(a) {
		return b
	}

	return a
}

func rank(tone string) int {
	if r, ok := healthRank[tone]; ok {
		return r
	}

	return healthRank["unknown"]
}

// toneOf reads a value as a tone word; anything else is unknown.
func toneOf(v any) string {
	s, _ := v.(string)
	if _, ok := healthRank[s]; ok {
		return s
	}

	return "unknown"
}

// formatVersion is the plugin format's version, the last part of its schemaVersion.
func formatVersion(schemaVersion string) string {
	return schemaVersion[strings.LastIndex(schemaVersion, "/")+1:]
}
