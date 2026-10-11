package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Config snippets: the handful of machine config changes made most often, as a form of a few
// parameters. A snippet turns into edits of the draft (MachineConfigEdit), so the change goes
// through the usual review. The edits depend on the draft: Talos 1.12+ configs hold some of
// these settings in their own documents (ResolverConfig, TimeSyncConfig, KubeletConfig…),
// and Talos refuses the v1alpha1 field when such a document sets it too.

// Snippet groups and parameter types, in display order; the apps translate the titles.
const (
	snippetGroupNetwork    = "network"
	snippetGroupKernel     = "kernel"
	snippetGroupKubernetes = "kubernetes"
	snippetGroupCluster    = "cluster"

	snippetParamText = "text"
	snippetParamList = "list" // several values, one per line or comma separated
	snippetParamInt  = "int"
	snippetParamBool = "bool"
)

type snippetParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Hint is an example value (verbatim: no translation).
	Hint string `json:"hint"`
}

type configSnippet struct {
	ID     string         `json:"id"`
	Group  string         `json:"group"`
	Params []snippetParam `json:"params"`
	// Staged: the change only takes effect at the next boot (it cannot be tried).
	Staged bool `json:"staged"`

	build func(p snippetValues, d configDocs) ([]configEdit, error)
}

var configSnippets = []configSnippet{
	{ID: "ntp", Group: snippetGroupNetwork, Params: []snippetParam{{"servers", snippetParamList, "time.cloudflare.com"}}, build: buildNTP},
	{ID: "dns", Group: snippetGroupNetwork, Params: []snippetParam{{"nameservers", snippetParamList, "1.1.1.1"}}, build: buildDNS},
	{ID: "mtu", Group: snippetGroupNetwork, Params: []snippetParam{{"interface", snippetParamText, "eth0"}, {"mtu", snippetParamInt, "9000"}}, build: buildMTU},
	{ID: "sysctl", Group: snippetGroupKernel, Params: []snippetParam{{"key", snippetParamText, "net.core.somaxconn"}, {"value", snippetParamText, "65535"}}, build: buildSysctl},
	{ID: "kernel_args", Group: snippetGroupKernel, Staged: true, Params: []snippetParam{{"args", snippetParamList, "console=ttyS0"}}, build: buildKernelArgs},
	{ID: "kubelet_arg", Group: snippetGroupKubernetes, Params: []snippetParam{{"key", snippetParamText, "max-pods"}, {"value", snippetParamText, "250"}}, build: buildKubeletArg},
	{ID: "apiserver_arg", Group: snippetGroupKubernetes, Params: []snippetParam{{"key", snippetParamText, "audit-log-maxage"}, {"value", snippetParamText, "30"}}, build: buildAPIServerArg},
	{ID: "node_label", Group: snippetGroupKubernetes, Params: []snippetParam{{"key", snippetParamText, "example.test/rack"}, {"value", snippetParamText, "r1"}}, build: buildNodeLabel},
	{ID: "registry_mirror", Group: snippetGroupCluster, Params: []snippetParam{{"host", snippetParamText, "docker.io"}, {"endpoint", snippetParamText, "https://mirror.example.test"}}, build: buildRegistryMirror},
	{ID: "kubespan", Group: snippetGroupCluster, Params: []snippetParam{{"enabled", snippetParamBool, "true"}}, build: buildKubeSpan},
}

// ConfigSnippets lists the snippets: {"snippets":[{id,group,params:[{name,type,hint}],staged}]}.
func ConfigSnippets() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(struct {
		Snippets []configSnippet `json:"snippets"`
	}{configSnippets})
}

// snippetResult is what ConfigSnippetEdits and DriftFixEdits return.
type snippetResult struct {
	// Fixable is false when no config change can do it (DriftFixEdits); Reason says why.
	Fixable bool         `json:"fixable"`
	Reason  string       `json:"reason,omitempty"`
	Edits   []configEdit `json:"edits"`
	Staged  bool         `json:"staged"`
}

// ConfigSnippetEdits turns snippet id with its parameters (paramsJSON: {"name":"value"}; a
// list as lines or comma separated) into the edits of draftYAML that make the change:
// {"fixable":true,"edits":[…],"staged"}. Each edit is applied with MachineConfigEdit, as is.
func ConfigSnippetEdits(draftYAML, id, paramsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	snippet, err := findSnippet(id)
	if err != nil {
		return "", err
	}

	values := map[string]string{}
	if strings.TrimSpace(paramsJSON) != "" {
		if err := json.Unmarshal([]byte(paramsJSON), &values); err != nil {
			return "", fmt.Errorf("invalid parameters: %w", err)
		}
	}

	docs, err := parseDraftDocs(draftYAML)
	if err != nil {
		return "", err
	}

	p := snippetValues{values: values, params: snippet.Params}
	if err := p.check(); err != nil {
		return "", err
	}

	edits, err := snippet.build(p, docs)
	if err != nil {
		return "", err
	}

	return toJSON(snippetResult{Fixable: true, Edits: edits, Staged: snippet.Staged})
}

// DriftFixEdits are the edits of draftYAML that give the node the reference's value of a
// drift key (Insights): before is the reference's value, after the node's. Only dns, ntp and
// mtu are config settings; the others are {"fixable":false,"reason":…}.
func DriftFixEdits(draftYAML, key, before, after string) (out string, err error) {
	defer maskResult(&out, &err)

	docs, err := parseDraftDocs(draftYAML)
	if err != nil {
		return "", err
	}

	var edits []configEdit

	switch key {
	case "dns", "ntp":
		list := splitList(before)
		if len(list) == 0 {
			return toJSON(snippetResult{Reason: "the reference has none: remove them in the editor if they should go"})
		}

		build := buildDNS
		name := "nameservers"

		if key == "ntp" {
			build, name = buildNTP, "servers"
		}

		edits, err = build(snippetValues{values: map[string]string{name: before}}, docs)
	case "mtu":
		edits, err = mtuFixEdits(docs, before, after)
	default:
		return toJSON(snippetResult{Reason: "not a machine config setting: " + driftKeyReason(key)})
	}

	if err != nil {
		return "", err
	}

	if len(edits) == 0 {
		return toJSON(snippetResult{Reason: "nothing to change"})
	}

	return toJSON(snippetResult{Fixable: true, Edits: edits})
}

func driftKeyReason(key string) string {
	switch key {
	case "version":
		return "upgrade the node instead"
	case "extensions":
		return "extensions come with the installer image: upgrade to an image that has them"
	case "secureBoot", "uki":
		return "how the node boots is set by its installation"
	}

	return key
}

func findSnippet(id string) (configSnippet, error) {
	for _, s := range configSnippets {
		if s.ID == id {
			return s, nil
		}
	}

	return configSnippet{}, fmt.Errorf("unknown snippet %q", id)
}

// snippetValues are a snippet's typed parameters.
type snippetValues struct {
	values map[string]string
	params []snippetParam
}

func (p snippetValues) text(name string) string { return strings.TrimSpace(p.values[name]) }

func (p snippetValues) list(name string) []string { return splitList(p.values[name]) }

// check refuses a missing or malformed parameter.
func (p snippetValues) check() error {
	for _, param := range p.params {
		value := p.text(param.Name)
		if value == "" {
			return fmt.Errorf("%s is missing", param.Name)
		}

		switch param.Type {
		case snippetParamInt:
			if n, err := strconv.Atoi(value); err != nil || n <= 0 {
				return fmt.Errorf("%s: %q is not a positive whole number", param.Name, value)
			}
		case snippetParamBool:
			if value != "true" && value != "false" {
				return fmt.Errorf("%s: %q is not true or false", param.Name, value)
			}
		case snippetParamList:
			if len(p.list(param.Name)) == 0 {
				return fmt.Errorf("%s is missing", param.Name)
			}
		}
	}

	return nil
}

// splitList reads values one per line or comma separated, blanks dropped.
func splitList(text string) []string {
	out := []string{}

	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}

	return out
}

// configDocs are the documents of a draft, to find where a setting goes.
type configDocs []*yaml.Node

func parseDraftDocs(draftYAML string) (configDocs, error) {
	docs, err := parseConfigDocs(draftYAML)
	if err != nil {
		return nil, fmt.Errorf("the draft is not valid YAML: %w", err)
	}

	return docs, nil
}

// find is the index of the first document of kind (named name when not ""), or -1.
func (d configDocs) find(kind, name string) int {
	for i, doc := range d {
		if configDocKind(doc) == kind && (name == "" || yamlScalarAt(doc, []string{"name"}) == name) {
			return i
		}
	}

	return -1
}

// main is the v1alpha1 document's index.
func (d configDocs) main() (int, error) {
	if i := d.find("", ""); i >= 0 && yamlScalarAt(d[i], []string{"version"}) == "v1alpha1" {
		return i, nil
	}

	return -1, errors.New("this config has no v1alpha1 document")
}

// put is the edit that sets value (any YAML) at path of document doc.
func put(doc int, path []string, value any) (configEdit, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return configEdit{}, err
	}

	return configEdit{Doc: doc, Path: path, Op: configOpPut, Type: configTypeYAML, Value: string(b)}, nil
}

// putIn is put in the first document of kind when the draft has one, else at legacy in the
// v1alpha1 document (always there when kind is "").
func putIn(d configDocs, kind string, path, legacy []string, value any) ([]configEdit, error) {
	doc := -1
	if kind != "" {
		doc = d.find(kind, "")
	}

	if doc < 0 {
		var err error
		if doc, err = d.main(); err != nil {
			return nil, err
		}

		path = legacy
	}

	edit, err := put(doc, path, value)

	return []configEdit{edit}, err
}

func buildNTP(p snippetValues, d configDocs) ([]configEdit, error) {
	return putIn(d, "TimeSyncConfig", []string{"ntp", "servers"}, []string{"machine", "time", "servers"}, p.list("servers"))
}

func buildDNS(p snippetValues, d configDocs) ([]configEdit, error) {
	servers := p.list("nameservers")
	if d.find("ResolverConfig", "") < 0 {
		return putIn(d, "", nil, []string{"machine", "network", "nameservers"}, servers)
	}

	addresses := make([]map[string]string, len(servers))
	for i, s := range servers {
		addresses[i] = map[string]string{"address": s}
	}

	return putIn(d, "ResolverConfig", []string{"nameservers"}, nil, addresses)
}

func buildSysctl(p snippetValues, d configDocs) ([]configEdit, error) {
	key := p.text("key")

	return putIn(d, "SysctlConfig", []string{"params", key}, []string{"machine", "sysctls", key}, p.text("value"))
}

func buildKernelArgs(p snippetValues, d configDocs) ([]configEdit, error) {
	return putIn(d, "", nil, []string{"machine", "install", "extraKernelArgs"}, p.list("args"))
}

func buildKubeletArg(p snippetValues, d configDocs) ([]configEdit, error) {
	key := p.text("key")

	return putIn(d, "KubeletConfig", []string{"extraArgs", key}, []string{"machine", "kubelet", "extraArgs", key}, p.text("value"))
}

func buildAPIServerArg(p snippetValues, d configDocs) ([]configEdit, error) {
	if d.find("KubeAPIServerConfig", "") < 0 {
		if i, err := d.main(); err != nil || yamlScalarAt(d[i], []string{"machine", "type"}) == joinRoleWorker {
			return nil, errors.New("only a control plane runs the API server")
		}
	}

	key := p.text("key")

	return putIn(d, "KubeAPIServerConfig", []string{"extraArgs", key}, []string{"cluster", "apiServer", "extraArgs", key}, p.text("value"))
}

func buildNodeLabel(p snippetValues, d configDocs) ([]configEdit, error) {
	key := p.text("key")

	return putIn(d, "KubeNodeConfig", []string{"labels", key}, []string{"machine", "nodeLabels", key}, p.text("value"))
}

func buildRegistryMirror(p snippetValues, d configDocs) ([]configEdit, error) {
	host, endpoint := p.text("host"), p.text("endpoint")
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		return nil, fmt.Errorf("%q is not a URL (https://…)", endpoint)
	}

	if doc := d.find("RegistryMirrorConfig", host); doc >= 0 {
		edit, err := put(doc, []string{"endpoints"}, []map[string]string{{"url": endpoint}})

		return []configEdit{edit}, err
	}

	return putIn(d, "", nil, []string{"machine", "registries", "mirrors", host, "endpoints"}, []string{endpoint})
}

func buildKubeSpan(p snippetValues, d configDocs) ([]configEdit, error) {
	enabled := p.text("enabled") == "true"

	return putIn(d, "KubeSpanConfig", []string{"enabled"}, []string{"machine", "network", "kubespan", "enabled"}, enabled)
}

func buildMTU(p snippetValues, d configDocs) ([]configEdit, error) {
	mtu, _ := strconv.Atoi(p.text("mtu")) //nolint:errcheck // checked by check()

	return mtuEdits(d, p.text("interface"), mtu)
}

// mtuEdits set link's MTU: in its LinkConfig document when the draft has one, else in its
// v1alpha1 interface (added when missing).
func mtuEdits(d configDocs, link string, mtu int) ([]configEdit, error) {
	if doc := d.find("LinkConfig", link); doc >= 0 {
		edit, err := put(doc, []string{"mtu"}, mtu)

		return []configEdit{edit}, err
	}

	main, err := d.main()
	if err != nil {
		return nil, err
	}

	ifaces := yamlNodeAt(d[main], []string{"machine", "network", "interfaces"})
	if ifaces == nil || ifaces.Kind != yaml.SequenceNode {
		edit, err := put(main, []string{"machine", "network", "interfaces"}, []map[string]any{{"interface": link, "mtu": mtu}})

		return []configEdit{edit}, err
	}

	for i, iface := range ifaces.Content {
		if yamlScalarAt(iface, []string{"interface"}) == link {
			edit, err := put(main, []string{"machine", "network", "interfaces", strconv.Itoa(i), "mtu"}, mtu)

			return []configEdit{edit}, err
		}
	}

	item, err := json.Marshal(map[string]any{"interface": link, "mtu": mtu})
	if err != nil {
		return nil, err
	}

	return []configEdit{{Doc: main, Path: []string{"machine", "network", "interfaces"}, Op: configOpAdd, Type: configTypeYAML, Value: string(item)}}, nil
}

// mtuFixEdits give each link the reference's MTU where the node's differs (drift values are
// "eth0=1500, eth1=9000").
func mtuFixEdits(d configDocs, before, after string) ([]configEdit, error) {
	current := map[string]string{}

	for _, entry := range splitList(after) {
		if name, value, ok := strings.Cut(entry, "="); ok {
			current[name] = value
		}
	}

	var edits []configEdit

	for _, entry := range splitList(before) {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || current[name] == value {
			continue
		}

		if _, present := current[name]; !present {
			continue // the node has no such link: nothing to set on it
		}

		mtu, err := strconv.Atoi(value)
		if err != nil || mtu <= 0 {
			continue
		}

		more, err := mtuEdits(d, name, mtu)
		if err != nil {
			return nil, err
		}

		// A later edit is computed on the draft before the earlier ones: a second interface
		// added to a missing list would replace the first. Keep them on one list instead.
		edits = mergeMTUEdits(edits, more)
	}

	return edits, nil
}

// mergeMTUEdits folds an edit that replaces the whole interfaces list into a previous one.
func mergeMTUEdits(edits, more []configEdit) []configEdit {
	for _, m := range more {
		merged := false

		for i, e := range edits {
			if e.Op == configOpPut && m.Op == configOpPut && e.Doc == m.Doc && strings.Join(e.Path, ".") == "machine.network.interfaces" &&
				strings.Join(m.Path, ".") == "machine.network.interfaces" {
				var a, b []map[string]any
				if json.Unmarshal([]byte(e.Value), &a) == nil && json.Unmarshal([]byte(m.Value), &b) == nil {
					value, _ := json.Marshal(append(a, b...)) //nolint:errcheck // maps of strings and ints
					edits[i].Value = string(value)
					merged = true
				}
			}
		}

		if !merged {
			edits = append(edits, m)
		}
	}

	return edits
}
