package ichorgo

import (
	"fmt"
	"slices"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/config/config"
)

// containerdV4Version is the Talos minor that generates the containerd configuration with
// version = 4: custom CRI fragments are then read as version 4, and the settings that moved
// are silently ignored (containerd only logs "Ignoring unknown key in TOML").
const containerdV4Version = "v1.15"

// smartNote is what an upgraded node gains but does not get by default: Talos 1.15 enables
// SMART collection only in configs it generates, an upgraded config keeps it off.
const smartNote = "Talos 1.15 can collect the disks' SMART health; an upgraded node needs a DiskSMARTConfig document in its machine config to turn it on"

// criDoc is one CRI customization fragment of a node's machine config: a
// CRICustomizationConfig document, or the legacy /etc/cri/conf.d/20-customization.part
// machine file under its reserved name.
type criDoc struct{ name, content string }

// criDocs reads the CRI customization fragments of a machine config (nil-safe).
func criDocs(cfg config.Config) []criDoc {
	if cfg == nil {
		return nil
	}

	var out []criDoc

	for _, d := range cfg.CRICustomizationConfigs() {
		out = append(out, criDoc{name: d.Name(), content: d.Content()})
	}

	return out
}

// versionNotes is what crossing into a new Talos minor changes for one node.
type versionNotes struct {
	warnings  []string // settings of the node's config the new version ignores
	unchecked string   // why those settings could not be checked ("" when they were)
	notes     []string // what the new version offers once enabled
}

// versionUpgradeNotes is what upgrading a node from one Talos version to another changes
// in the node's own configuration; readErr is why its config could not be read. Only an
// upgrade that crosses into 1.15 from an older minor has notes.
func versionUpgradeNotes(fromVersion, toVersion string, docs []criDoc, readErr string) versionNotes {
	if fromVersion == "" || toVersion == "" ||
		compareMinor(fromVersion, containerdV4Version) >= 0 || compareMinor(toVersion, containerdV4Version) < 0 {
		return versionNotes{}
	}

	out := versionNotes{notes: []string{smartNote}}

	if readErr != "" {
		out.unchecked = "could not check the containerd settings Talos 1.15 ignores (" + readErr + "; reading the machine config needs os:admin)"

		return out
	}

	for _, d := range docs {
		out.warnings = append(out.warnings, containerdV4Warnings(d)...)
	}

	return out
}

// applyVersionNotes adds what the supported next minor changes in the node's own config.
// A node plan knows no target version, so it assumes one minor up; a cluster plan
// recomputes them for its version (clusterNodeOf).
func applyVersionNotes(plan *upgradePlan, in planInput) {
	plan.cri, plan.criErr = in.cri, in.imageErr

	vn := versionUpgradeNotes(in.target.version, nextMinor(in.target.version), in.cri, in.imageErr)

	plan.versionWarnings = vn.warnings
	plan.Warnings = append(plan.Warnings, vn.warnings...)
	plan.upgradeOnly = append(plan.upgradeOnly, vn.warnings...)

	if vn.unchecked != "" {
		plan.Notes = append(plan.Notes, vn.unchecked)
	}

	plan.Notes = append(plan.Notes, vn.notes...)
}

// nextMinor is the version a node on version upgrades to along the supported path, one
// minor at a time ("" when version is unknown).
func nextMinor(version string) string {
	if version == "" {
		return ""
	}

	n := versionNumbers(version)

	return fmt.Sprintf("v%d.%d.0", n[0], n[1]+1)
}

// containerdMove is where a containerd v3 setting lives in a v4 configuration.
type containerdMove struct {
	section string // the v4 table, without brackets
	key     string // the key there ("" keeps the v3 name)
}

// containerdV4Moves maps "table.key" of the v3 top-level sections to their v4 plugin block
// (the "containerd Configuration Version 4" table of the Talos 1.15 release notes).
var containerdV4Moves = map[string]containerdMove{
	"metrics.address":            {section: `plugins."io.containerd.server.v1.metrics"`},
	"metrics.grpc_histogram":     {section: `plugins."io.containerd.metrics.v1.grpc-prometheus"`},
	"grpc.address":               {section: `plugins."io.containerd.server.v1.grpc"`},
	"grpc.uid":                   {section: `plugins."io.containerd.server.v1.grpc"`},
	"grpc.gid":                   {section: `plugins."io.containerd.server.v1.grpc"`},
	"grpc.max_recv_message_size": {section: `plugins."io.containerd.server.v1.grpc"`},
	"grpc.max_send_message_size": {section: `plugins."io.containerd.server.v1.grpc"`},
	"grpc.tcp_address":           {section: `plugins."io.containerd.server.v1.grpc-tcp"`, key: "address"},
	"grpc.tcp_tls_ca":            {section: `plugins."io.containerd.server.v1.grpc-tcp"`, key: "tls_ca"},
	"grpc.tcp_tls_cert":          {section: `plugins."io.containerd.server.v1.grpc-tcp"`, key: "tls_cert"},
	"grpc.tcp_tls_key":           {section: `plugins."io.containerd.server.v1.grpc-tcp"`, key: "tls_key"},
	"grpc.tcp_tls_common_name":   {section: `plugins."io.containerd.server.v1.grpc-tcp"`, key: "tls_common_name"},
	"ttrpc.address":              {section: `plugins."io.containerd.server.v1.ttrpc"`},
	"ttrpc.uid":                  {section: `plugins."io.containerd.server.v1.ttrpc"`},
	"ttrpc.gid":                  {section: `plugins."io.containerd.server.v1.ttrpc"`},
	"debug.address":              {section: `plugins."io.containerd.server.v1.debug"`},
	"debug.uid":                  {section: `plugins."io.containerd.server.v1.debug"`},
	"debug.gid":                  {section: `plugins."io.containerd.server.v1.debug"`},
}

// containerdV4Warnings names each v3 top-level setting of a CRI fragment that containerd v4
// ignores, grouped by the table it moves to. go.mod has no TOML parser and this needs only
// table headers and key names, so the fragment is read line by line: "[table]" headers,
// "key = value" lines, and dotted "table.key = value" lines before the first header.
func containerdV4Warnings(d criDoc) []string {
	type group struct{ from, to string }

	moved := map[group][]string{}

	var order []group

	table := ""

	for _, line := range strings.Split(d.content, "\n") {
		line = strings.TrimSpace(stripTOMLComment(line))

		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "[["):
			table = "-" // an array of tables: none of the moved settings
		case strings.HasPrefix(line, "["):
			table = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
		default:
			key, _, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}

			t, k := table, strings.TrimSpace(key)
			if t == "" {
				t, k, _ = strings.Cut(k, ".")
			}

			mv, ok := containerdV4Moves[t+"."+k]
			if !ok {
				continue
			}

			g := group{from: t, to: mv.section}
			if _, seen := moved[g]; !seen {
				order = append(order, g)
			}

			moved[g] = append(moved[g], keyMove(k, mv.key))
		}
	}

	out := make([]string, 0, len(order))

	for _, g := range order {
		out = append(out, fmt.Sprintf("%s: Talos 1.15 ignores [%s] %s; move it to [%s] before upgrading",
			criDocLabel(d.name), g.from, strings.Join(slices.Compact(moved[g]), ", "), g.to))
	}

	return out
}

// keyMove is "key", or "key (now new)" when the key is renamed in its new table.
func keyMove(key, renamed string) string {
	if renamed == "" || renamed == key {
		return key
	}

	return key + " (now " + renamed + ")"
}

// criDocLabel names a CRI fragment as the person wrote it.
func criDocLabel(name string) string {
	if name == config.LegacyCRICustomizationConfigName {
		return "the machine file /etc/cri/conf.d/20-customization.part"
	}

	return fmt.Sprintf("CRICustomizationConfig %q", name)
}

// stripTOMLComment drops a "#" comment outside a quoted string.
func stripTOMLComment(line string) string {
	quote := rune(0)

	for i, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote == 0 && r == '#':
			return line[:i]
		}
	}

	return line
}
