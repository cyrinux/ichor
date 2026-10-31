// Package plugin is the declarative CRD integration format: a YAML file says which
// resources a plugin reads, the fields and health it derives, the data services it
// aggregates, the cards it shows, the actions it offers and the findings it raises. Go loads
// it strictly, validates every reference and expression, and compiles it once; the apps only
// draw the render tree. docs/plugins.md is the reference; schema/plugin.schema.json is
// generated from these types.
package plugin

//go:generate go run ./internal/schemagen -o schema/plugin.schema.json

// SchemaVersion is the format this loader reads; another major version is refused.
const SchemaVersion = "ichor.dev/plugin/v1alpha1"

// Plugin is one plugin file.
type Plugin struct {
	// SchemaVersion is ichor.dev/plugin/v1alpha1; the loader refuses another major version.
	SchemaVersion string `yaml:"schemaVersion" json:"schemaVersion" schema:"required"`
	// ID is unique across built-in and user plugins: lowercase letters, digits and dashes.
	ID string `yaml:"id" json:"id" schema:"required,pattern=^[a-z0-9-]+$"`
	// Name is the product name, a proper noun, never translated.
	Name string `yaml:"name" json:"name" schema:"required"`
	// Description is one line about the plugin, shown by the plugin index.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Maintainer is who keeps the plugin file, shown by the plugin index.
	Maintainer string `yaml:"maintainer,omitempty" json:"maintainer,omitempty"`
	// Website is the project's home page.
	Website string `yaml:"website,omitempty" json:"website,omitempty"`
	// Icon is an app catalog id (assets/appicons).
	Icon string `yaml:"icon,omitempty" json:"icon,omitempty"`
	// Category picks the hub that lists the plugin (default data-services).
	Category string `yaml:"category,omitempty" json:"category,omitempty" schema:"enum=data-services|networking|gitops|policies|workloads|compute|security"`
	// CatalogID is the app catalog id used to detect the product from its pods.
	CatalogID string `yaml:"catalogId,omitempty" json:"catalogId,omitempty"`
	// Screen is native:<route> to open a native screen from the hub tile; empty is the generic plugin screen.
	Screen string `yaml:"screen,omitempty" json:"screen,omitempty" schema:"pattern=^native:[A-Za-z0-9_-]+$"`
	// Detect activates the plugin when any entry matches.
	Detect []Detect `yaml:"detect" json:"detect" schema:"required,minItems=1"`
	// Resources are the kinds the plugin reads, by local name (the key used everywhere else).
	Resources map[string]Resource `yaml:"resources" json:"resources" schema:"required,minProperties=1,keys=^[a-zA-Z][a-zA-Z0-9]*$"`
	// Fields are named expressions per resource, evaluated per object in order; a field may use the earlier ones as fields.<name>.
	Fields map[string]map[string]Expr `yaml:"fields,omitempty" json:"fields,omitempty"`
	// Health are the rules per resource; the first matching rule wins, and an object no rule matches is ok.
	Health map[string][]HealthRule `yaml:"health,omitempty" json:"health,omitempty"`
	// DataServices are derived data (aggregations, groupings, projections), by name, in order.
	DataServices map[string]DataService `yaml:"dataServices,omitempty" json:"dataServices,omitempty" schema:"keys=^[a-zA-Z][a-zA-Z0-9]*$"`
	// Cards are what the apps show: summary (the tile and the hub header) and list cards.
	Cards map[string]Card `yaml:"cards" json:"cards" schema:"required"`
	// Actions are declared mutations, by id.
	Actions map[string]Action `yaml:"actions,omitempty" json:"actions,omitempty" schema:"keys=^[a-zA-Z][a-zA-Z0-9]*$"`
	// Findings feed the cluster checkup and the background monitor.
	Findings []Finding `yaml:"findings,omitempty" json:"findings,omitempty"`
	// Tools are buttons of the plugin screen, by id, each run by a Go function over what the plugin read.
	Tools map[string]Tool `yaml:"tools,omitempty" json:"tools,omitempty" schema:"keys=^[a-zA-Z][a-zA-Z0-9]*$"`
	// Samples are demo objects per resource, in the API's shape: the demo context and the tests render them.
	Samples map[string][]map[string]any `yaml:"samples" json:"samples" schema:"required"`
	// I18n maps an English label to its translation, per locale; a missing label stays English.
	I18n map[string]map[string]string `yaml:"i18n,omitempty" json:"i18n,omitempty"`

	src source // set by Load
}

// Detect is one way to tell the product is installed: an API group (with a resource when
// the group is shared), running pods of an app catalog id, or a Prometheus-style service.
type Detect struct {
	// Group is an API group served by the cluster (GET /apis).
	Group string `yaml:"group,omitempty" json:"group,omitempty"`
	// Resource must be served too, for a group several products share.
	Resource string `yaml:"resource,omitempty" json:"resource,omitempty"`
	// Pods is the app catalog id of running pods.
	Pods string `yaml:"pods,omitempty" json:"pods,omitempty"`
	// Service is a service kind matched by name.
	Service string `yaml:"service,omitempty" json:"service,omitempty"`
}

// Resource is a kind the plugin lists.
type Resource struct {
	// Group is the API group, "" for the core group.
	Group string `yaml:"group" json:"group"`
	// Version is the API version, or preferred for the group's preferred version.
	Version string `yaml:"version" json:"version" schema:"required"`
	// Resource is the plural resource name (sleepinfos).
	Resource string `yaml:"resource" json:"resource" schema:"required"`
	// Kind is the object kind (SleepInfo).
	Kind string `yaml:"kind" json:"kind" schema:"required"`
	// Scope is Namespaced or Cluster.
	Scope string `yaml:"scope" json:"scope" schema:"required,enum=Namespaced|Cluster"`
	// List narrows and pages the list call.
	List *ListOptions `yaml:"list,omitempty" json:"list,omitempty"`
	// Watch refreshes the plugin screen on watch events.
	Watch bool `yaml:"watch,omitempty" json:"watch,omitempty"`
	// Reveal shows a Secret's data; it stays masked otherwise.
	Reveal bool `yaml:"reveal,omitempty" json:"reveal,omitempty"`
	// Optional is a resource the plugin does without (a decorative relation): forbidden or not served, it reads as empty with no error.
	Optional bool `yaml:"optional,omitempty" json:"optional,omitempty"`
	// Source is native:<id> to read through a Go reader instead of a list call.
	Source string `yaml:"source,omitempty" json:"source,omitempty" schema:"pattern=^native:[A-Za-z0-9_-]+$"`
	// Ref is the object a row of this resource opens, with source only: an expression giving
	// a map of group, version, resource, kind, namespace and name ($.source).
	Ref Expr `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// ListOptions narrow a list call.
type ListOptions struct {
	LabelSelector string `yaml:"labelSelector,omitempty" json:"labelSelector,omitempty"`
	FieldSelector string `yaml:"fieldSelector,omitempty" json:"fieldSelector,omitempty"`
	// Limit is the page size (0 or absent is 500).
	Limit int `yaml:"limit,omitempty" json:"limit,omitempty" schema:"minimum=0,maximum=5000"`
	// MetadataOnly reads each object's metadata alone (PartialObjectMetadata): a Secret's
	// values, or a large spec, never leave the API server.
	MetadataOnly bool `yaml:"metadataOnly,omitempty" json:"metadataOnly,omitempty"`
}

// HealthRule sets an object's health when its condition holds.
type HealthRule struct {
	// When is a boolean expression over the object and its fields.
	When Expr `yaml:"when" json:"when" schema:"required"`
	// Health is the tone the object takes.
	Health string `yaml:"health" json:"health" schema:"required,enum=ok|idle|info|warning|critical|unknown"`
	// Reason is a stable id the apps may map to a string.
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`
	// Message explains the health; a template.
	Message Template `yaml:"message,omitempty" json:"message,omitempty"`
}

// DataService is derived data: aggregate over resources, or from + groupBy + aggregate, or
// from + where + select.
type DataService struct {
	// Aggregate are named values: count(res), count(res, pred), sum/min/max(res, expr), worst(res.health); with groupBy, count(), count(pred), sum(expr)…
	Aggregate map[string]Expr `yaml:"aggregate,omitempty" json:"aggregate,omitempty"`
	// Health is the data service's health, e.g. worst(sleepinfo.health).
	Health Expr `yaml:"health,omitempty" json:"health,omitempty"`
	// From is the resource the data service walks.
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	// GroupBy is the key of each group (one row per key).
	GroupBy Expr `yaml:"groupBy,omitempty" json:"groupBy,omitempty"`
	// Where keeps the objects it holds for.
	Where Expr `yaml:"where,omitempty" json:"where,omitempty"`
	// Select projects each kept object into named values.
	Select map[string]Expr `yaml:"select,omitempty" json:"select,omitempty"`
}

// Card is the summary card or a list card.
type Card struct {
	// Title is the summary's title: an expression, or a count with singular and plural.
	Title *Title `yaml:"title,omitempty" json:"title,omitempty"`
	// Health is the summary's health.
	Health Expr `yaml:"health,omitempty" json:"health,omitempty"`
	// Badges are the summary's chips.
	Badges []Item `yaml:"badges,omitempty" json:"badges,omitempty"`
	// From is the resource or data service (dataServices.<name>) a list card shows, one row per element.
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	// Sort orders the rows: expressions, each optionally followed by asc or desc.
	Sort []string `yaml:"sort,omitempty" json:"sort,omitempty"`
	// Empty is the text shown when there is no row.
	Empty string `yaml:"empty,omitempty" json:"empty,omitempty"`
	// Row is one row of a list card.
	Row *Row `yaml:"row,omitempty" json:"row,omitempty"`
}

// Title is an expression, or {count, one, other}: "%d" in one and other is the count.
type Title struct {
	Expr  Expr   `yaml:"-" json:"-"`
	Count Expr   `yaml:"count" json:"count"`
	One   string `yaml:"one" json:"one"`
	Other string `yaml:"other" json:"other"`
}

// Row is one row of a list card.
type Row struct {
	Title    Expr      `yaml:"title" json:"title" schema:"required"`
	Subtitle Expr      `yaml:"subtitle,omitempty" json:"subtitle,omitempty"`
	Status   *Status   `yaml:"status,omitempty" json:"status,omitempty"`
	Trailing *Trailing `yaml:"trailing,omitempty" json:"trailing,omitempty"`
	// Detail are the sections shown when the row is expanded.
	Detail []Section `yaml:"detail,omitempty" json:"detail,omitempty"`
}

// Status is a row's colored status.
type Status struct {
	Tone Tone `yaml:"tone,omitempty" json:"tone,omitempty"`
	Text Expr `yaml:"text,omitempty" json:"text,omitempty"`
}

// Trailing is the text at a row's end.
type Trailing struct {
	Text Expr `yaml:"text" json:"text" schema:"required"`
}

// Section is a primitive of a row's detail; which keys apply depends on its type.
type Section struct {
	// Type is the primitive.
	Type string `yaml:"type" json:"type" schema:"required,enum=keyValues|badges|gauge|conditions|relations|table|text|actions"`
	// Items are the rows of keyValues, badges and relations, and the action ids of actions.
	Items []Item `yaml:"items,omitempty" json:"items,omitempty"`
	// Label names a gauge.
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
	// Value is a gauge's value.
	Value Expr `yaml:"value,omitempty" json:"value,omitempty"`
	// Max is a gauge's maximum.
	Max Expr `yaml:"max,omitempty" json:"max,omitempty"`
	// Unit formats a gauge's value: bytes, percent, or a word.
	Unit string `yaml:"unit,omitempty" json:"unit,omitempty"`
	// Tone colors a gauge or a text.
	Tone Tone `yaml:"tone,omitempty" json:"tone,omitempty"`
	// Text is a text section's template, or a gauge's caption.
	Text Template `yaml:"text,omitempty" json:"text,omitempty"`
	// Mono sets a text in a monospace font.
	Mono bool `yaml:"mono,omitempty" json:"mono,omitempty"`
	// From is the data service (dataServices.<name>) or resource a table walks.
	From string `yaml:"from,omitempty" json:"from,omitempty"`
	// Columns are a table's columns.
	Columns []Column `yaml:"columns,omitempty" json:"columns,omitempty"`
}

// Column is one column of a table.
type Column struct {
	Label string `yaml:"label" json:"label" schema:"required"`
	Value Expr   `yaml:"value" json:"value" schema:"required"`
}

// Item is a keyValues, badges or relations entry, or an action id (a plain string) in an
// actions section.
type Item struct {
	// Action is an action id, when the item is written as a plain string.
	Action string `yaml:"-" json:"-"`
	Label  string `yaml:"label,omitempty" json:"label,omitempty"`
	// Value is the shown value (keyValues, badges).
	Value Expr `yaml:"value,omitempty" json:"value,omitempty"`
	Tone  Tone `yaml:"tone,omitempty" json:"tone,omitempty"`
	Mono  bool `yaml:"mono,omitempty" json:"mono,omitempty"`
	// On lights a badge (ok) or dims it (idle).
	On Expr `yaml:"on,omitempty" json:"on,omitempty"`
	// HideWhenZero drops a badge whose value is 0.
	HideWhenZero bool `yaml:"hideWhenZero,omitempty" json:"hideWhenZero,omitempty"`
	// Ref is the related object (relations): an object, as join() returns.
	Ref Expr `yaml:"ref,omitempty" json:"ref,omitempty"`
	// Text replaces the related object's name (relations).
	Text Expr `yaml:"text,omitempty" json:"text,omitempty"`
}

// Action is a declared mutation of one object.
type Action struct {
	Label string `yaml:"label" json:"label" schema:"required"`
	// On is the resource the action applies to.
	On string `yaml:"on" json:"on" schema:"required"`
	// Verb is how the object is changed.
	Verb string `yaml:"verb" json:"verb" schema:"required,enum=mergePatch|jsonPatch|subresource|delete|annotate|create"`
	// Body is the patch or object; its strings are templates (now, name, namespace, params.*).
	Body any `yaml:"body,omitempty" json:"body,omitempty"`
	// Subresource is the subresource of verb subresource (start, scale…).
	Subresource string `yaml:"subresource,omitempty" json:"subresource,omitempty" schema:"pattern=^[a-z][a-z0-9-]*$"`
	// Method is how verb subresource calls it: PUT (the default, as virtctl does), POST, or
	// PATCH (a merge patch, as kubectl scale does).
	Method string `yaml:"method,omitempty" json:"method,omitempty" schema:"enum=PUT|POST|PATCH"`
	// PropagationPolicy is verb delete's: what happens to the object's dependents.
	PropagationPolicy string `yaml:"propagationPolicy,omitempty" json:"propagationPolicy,omitempty" schema:"enum=Foreground|Background|Orphan"`
	// RBAC is what the account needs; it becomes a can-I check.
	RBAC *RBAC `yaml:"rbac" json:"rbac" schema:"required"`
	// Confirm is how the app confirms: none, plain, or the object's name typed.
	Confirm string `yaml:"confirm,omitempty" json:"confirm,omitempty" schema:"enum=none|plain|typedName"`
	// Audit is the audit log key.
	Audit string `yaml:"audit" json:"audit" schema:"required"`
	// EnabledWhen is a boolean expression over the row's object.
	EnabledWhen Expr `yaml:"enabledWhen,omitempty" json:"enabledWhen,omitempty"`
	// DisabledReason is shown when EnabledWhen is false; a template.
	DisabledReason Template `yaml:"disabledReason,omitempty" json:"disabledReason,omitempty"`
	// Params are typed inputs, available to the body as params.<name>.
	Params []Param `yaml:"params,omitempty" json:"params,omitempty"`
}

// RBAC is the permission an action needs.
type RBAC struct {
	Verbs    []string `yaml:"verbs" json:"verbs" schema:"required,minItems=1"`
	Group    string   `yaml:"group" json:"group"`
	Resource string   `yaml:"resource" json:"resource" schema:"required"`
}

// Param is a typed input of an action.
type Param struct {
	Name  string `yaml:"name" json:"name" schema:"required,pattern=^[a-zA-Z][a-zA-Z0-9]*$"`
	Label string `yaml:"label" json:"label" schema:"required"`
	Type  string `yaml:"type" json:"type" schema:"required,enum=string|int|bool|enum"`
	// Values are the choices of an enum.
	Values  []string `yaml:"values,omitempty" json:"values,omitempty"`
	Default any      `yaml:"default,omitempty" json:"default,omitempty"`
}

// Finding is a problem the checkup and the monitor report, one per matching object.
type Finding struct {
	ID string `yaml:"id" json:"id" schema:"required,pattern=^[a-z0-9-]+$"`
	// From is the resource whose objects are tested.
	From string `yaml:"from" json:"from" schema:"required"`
	// When is a boolean expression over the object, its fields and health.
	When     Expr   `yaml:"when" json:"when" schema:"required"`
	Severity string `yaml:"severity" json:"severity" schema:"required,enum=info|warning|critical"`
	// Title is a template (name, namespace, message, reason, fields.*).
	Title Template `yaml:"title" json:"title" schema:"required"`
	// Detail is a template.
	Detail Template `yaml:"detail,omitempty" json:"detail,omitempty"`
	// Opens is the card the finding links to.
	Opens string `yaml:"opens,omitempty" json:"opens,omitempty"`
}

// Tool is a button of the plugin screen run by Go (a check no expression can make).
type Tool struct {
	Label string `yaml:"label" json:"label" schema:"required"`
	// Hint says what the tool does, under its button.
	Hint string `yaml:"hint,omitempty" json:"hint,omitempty"`
	// Run is native:<id>, the Go function that runs the tool.
	Run string `yaml:"run" json:"run" schema:"required,pattern=^native:[A-Za-z0-9_-]+$"`
	// Texts are the English words the Go function shows in its result, translated through
	// i18n like the labels.
	Texts []string `yaml:"texts,omitempty" json:"texts,omitempty"`
}

// Tones are the colors the apps know; Health order for worst(): ok < idle < info < unknown < warning < critical.
var Tones = []string{"ok", "idle", "info", "warning", "critical", "unknown"}
