package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/plugin"
)

// The dns-check tool (run: native:dns-check) of the ExternalDNS plugin: every hostname the
// plugin read is resolved from the phone and compared with its record's targets (dnsCheckWith),
// the owner id read from the TXT registry against its controller's. Its words are the plugin's
// (tools.<id>.texts), translated through its i18n.

// dnsCheckResolver is the resolver the tool asks: the device's own. Tests swap it.
var dnsCheckResolver dnsChecker = net.DefaultResolver

// dnsCheckItem is one record object of the plugin's input: what the check is asked, and what
// its row says of the record.
type dnsCheckItem struct {
	req    dnsCheckRequest
	source extDNSSource
}

func runDNSCheckTool(ctx context.Context, _ *kubeClient, c *plugin.Compiled, in plugin.Input, locale string) (plugin.ToolResult, error) {
	items := dnsCheckItems(c, in)

	reqs := make([]dnsCheckRequest, 0, len(items))
	for _, it := range items {
		reqs = append(reqs, it.req)
	}

	return dnsCheckToolResult(c, locale, items, dnsCheckEntries(ctx, dnsCheckResolver, reqs, dnsCheckDefaultTimeout)), nil
}

// demoDNSCheckTool answers the demo's canned verdicts (demoDNSCheckEntries) over the samples.
func demoDNSCheckTool(_ context.Context, _ *kubeClient, c *plugin.Compiled, in plugin.Input, locale string) (plugin.ToolResult, error) {
	items := dnsCheckItems(c, in)

	reqs := make([]dnsCheckRequest, 0, len(items))
	for _, it := range items {
		reqs = append(reqs, it.req)
	}

	return dnsCheckToolResult(c, locale, items, demoDNSCheckEntries(reqs)), nil
}

// dnsCheckItems are the plugin's ExternalDNSRecord objects in the order read (worst first),
// each with the owner id and TXT affixes of the controller that publishes it.
func dnsCheckItems(c *plugin.Compiled, in plugin.Input) []dnsCheckItem {
	type owner struct{ id, prefix, suffix string }

	owners := map[string]owner{}
	var records []map[string]any

	for _, key := range c.Resources {
		switch c.Plugin.Resources[key].Kind {
		case "ExternalDNSController":
			for _, obj := range in.Objects[key] {
				ns, name := digString(obj, "metadata", "namespace"), digString(obj, "metadata", "name")
				owners[ns+"/"+name] = owner{digString(obj, "spec", "ownerId"), digString(obj, "spec", "txtPrefix"), digString(obj, "spec", "txtSuffix")}
			}
		case "ExternalDNSRecord":
			records = append(records, in.Objects[key]...)
		}
	}

	out := make([]dnsCheckItem, 0, len(records))

	for _, obj := range records {
		var targets []string

		list, _ := dig(obj, "spec", "targets").([]any)
		for _, t := range list {
			if s, ok := t.(string); ok {
				targets = append(targets, s)
			}
		}

		o := owners[digString(obj, "spec", "controller")]
		out = append(out, dnsCheckItem{
			req: dnsCheckRequest{
				DNSName: digString(obj, "spec", "dnsName"), RecordType: digString(obj, "spec", "recordType"), Targets: targets,
				OwnerID: o.id, TxtPrefix: o.prefix, TxtSuffix: o.suffix,
			},
			source: extDNSSource{
				Group: digString(obj, "source", "group"), Version: digString(obj, "source", "version"), Resource: digString(obj, "source", "resource"),
				Kind: digString(obj, "source", "kind"), Namespace: digString(obj, "source", "namespace"), Name: digString(obj, "source", "name"),
			},
		})
	}

	return out
}

// digString is the string at path, "" when absent or another type.
func digString(v any, path ...string) string {
	s, _ := dig(v, path...).(string)
	return s
}

// dnsCheckVerdict is how a row says a verdict: its tone and its words.
var dnsCheckVerdict = map[string]struct{ tone, text string }{
	dnsStatusMatch:      {healthOK, "Resolves here"},
	dnsStatusMismatch:   {healthWarning, "Points elsewhere"},
	dnsStatusMissing:    {healthCritical, "Not found"},
	dnsStatusUnverified: {"info", "Resolves, nothing to compare"},
	dnsStatusError:      {"unknown", "Could not check"},
}

// dnsCheckToneRank orders the rows: the names to look at first.
var dnsCheckToneRank = map[string]int{healthCritical: 0, healthWarning: 1, "unknown": 2, "info": 3, healthOK: 4}

// dnsCheckToolResult is the result card: one row per name, worst first, under a title saying
// how many point at the cluster. Any name not found makes it critical, any other name that
// does not point here a warning.
func dnsCheckToolResult(c *plugin.Compiled, locale string, items []dnsCheckItem, entries []dnsCheckEntry) plugin.ToolResult {
	tr := func(s string) string { return c.Translate(locale, s) }

	card := plugin.TreeCard{ID: "names", Type: "list", Empty: tr("No hostnames."), Rows: []plugin.TreeRow{}}
	matched, missing := 0, false

	for i, e := range entries {
		it := items[i]
		verdict := dnsCheckVerdict[e.Status]

		switch e.Status {
		case dnsStatusMatch:
			matched++
		case dnsStatusMissing:
			missing = true
		}

		text := tr(verdict.text)
		if e.Status == dnsStatusMismatch && len(e.Answers) > 0 {
			text += ": " + strings.Join(e.Answers, ", ")
		}

		ref := &plugin.Ref{Group: it.source.Group, Version: it.source.Version, Resource: it.source.Resource, Kind: it.source.Kind, Namespace: it.source.Namespace, Name: it.source.Name}
		card.Rows = append(card.Rows, plugin.TreeRow{
			Ref:      ref,
			Title:    e.DNSName,
			Subtitle: it.req.RecordType + " · " + it.source.Kind + " " + it.source.Namespace + "/" + it.source.Name,
			Status:   &plugin.TreeStatus{Tone: verdict.tone, Text: text},
			Trailing: &plugin.TreeTrailing{Text: strconv.FormatInt(e.MS, 10) + " ms"},
			Detail:   dnsCheckDetail(tr, it, e, ref),
		})
	}

	slices.SortStableFunc(card.Rows, func(a, b plugin.TreeRow) int {
		return cmp.Compare(dnsCheckToneRank[a.Status.Tone], dnsCheckToneRank[b.Status.Tone])
	})

	health, total := healthWarning, len(entries)

	switch {
	case missing:
		health = healthCritical
	case matched == total:
		health = healthOK
	}

	title := tr("%d of %d names point at this cluster")
	if total == 1 {
		title = tr("%d of %d name points at this cluster")
	}

	return plugin.ToolResult{Title: fmt.Sprintf(title, matched, total), Health: health, Cards: []plugin.TreeCard{card}}
}

// dnsCheckDetail is a row's detail: the answers, the targets and the TXT owner, why a name
// points elsewhere or could not be checked, and the object the record comes from (ref), as a
// relation the apps open.
func dnsCheckDetail(tr func(string) string, it dnsCheckItem, e dnsCheckEntry, ref *plugin.Ref) []plugin.TreeSection {
	var kv []plugin.TreeItem

	for _, item := range []struct {
		label  string
		values []string
	}{{"Answers", e.Answers}, {"Targets", it.req.Targets}} {
		if len(item.values) > 0 {
			kv = append(kv, plugin.TreeItem{Label: tr(item.label), Value: strings.Join(item.values, ", "), Mono: true})
		}
	}

	switch {
	case e.Owner == "":
	case e.OwnerMatch != nil && !*e.OwnerMatch:
		kv = append(kv, plugin.TreeItem{Label: tr("Owner"), Value: fmt.Sprintf(tr("owned by %s"), e.Owner), Tone: healthWarning})
	default:
		kv = append(kv, plugin.TreeItem{Label: tr("Owner"), Value: e.Owner, Mono: true})
	}

	var out []plugin.TreeSection
	if len(kv) > 0 {
		out = append(out, plugin.TreeSection{Type: "keyValues", Items: kv})
	}

	switch {
	case e.Status == dnsStatusMismatch:
		out = append(out, plugin.TreeSection{Type: "text", Tone: healthWarning, Text: tr(dnsCheckMismatchHint)})
	case e.Message != "":
		out = append(out, plugin.TreeSection{Type: "text", Tone: "unknown", Text: tr(e.Message)})
	}

	if ref.Resource != "" && ref.Name != "" {
		label := strings.TrimSpace(ref.Kind + " " + strings.Trim(ref.Namespace+"/"+ref.Name, "/"))
		out = append(out, plugin.TreeSection{Type: "relations", Items: []plugin.TreeItem{{Label: tr("Source"), Value: label, Ref: ref}}})
	}

	return out
}

// dnsCheckMismatchHint says why a name may point elsewhere.
const dnsCheckMismatchHint = "A name that points elsewhere may be split-horizon DNS at home, a change still propagating, or another ExternalDNS owning it."
