package ichorgo

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// Integration requests: the operators a cluster runs that Ichor does not show yet, read from
// the API groups the server serves. Nothing leaves the phone from here: the app turns the
// groups the user picks into a GitHub issue they review and submit themselves.

// integrationSupported are the API groups Ichor reads: every group of integrationSpecs, the
// list Settings shows.
var integrationSupported = func() map[string]bool {
	out := map[string]bool{}
	for _, s := range integrationSpecs {
		for _, g := range s.Groups {
			out[g] = true
		}
	}

	return out
}()

// integrationSharedDomains host groups of unrelated projects: a family keeps one more label.
var integrationSharedDomains = map[string]bool{"x-k8s.io": true, "github.io": true, "github.com": true, "gitlab.io": true}

const (
	// integrationKindReads bounds the parallel discovery reads of the unsupported groups.
	integrationKindReads = 8
	// integrationIssueText caps the pre-filled groups text, so the URL stays well under the
	// length browsers and GitHub accept.
	integrationIssueText = 4000
	integrationLabel     = "integration"
	integrationTemplate  = "integration.yml"
)

type integrationReport struct {
	// Families are the unsupported groups, by operator ("istio.io": networking, security…).
	Families []integrationFamily `json:"families"`
	// Supported are the groups served that Ichor already reads.
	Supported []string `json:"supported"`
}

type integrationFamily struct {
	ID     string             `json:"id"`
	Groups []integrationGroup `json:"groups"`
}

type integrationGroup struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Kinds   []string `json:"kinds"`
}

// KubeIntegrations lists the API groups the cluster serves that Ichor does not read yet,
// grouped by operator, with their kinds (os:admin). Built-in Kubernetes groups are left out.
// kubeServer: see KubePods.
func KubeIntegrations(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoIntegrations, readIntegrations)
}

func readIntegrations(ctx context.Context, k *kubeClient) (integrationReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return integrationReport{}, err
	}

	report := integrationReport{Families: []integrationFamily{}, Supported: []string{}}

	var unknown []integrationGroup

	for name, version := range groups {
		switch {
		case integrationSupported[name]:
			report.Supported = append(report.Supported, name)
		case !isBuiltinGroup(name):
			unknown = append(unknown, integrationGroup{Name: name, Version: version, Kinds: []string{}})
		}
	}

	readIntegrationKinds(ctx, k, unknown)
	slices.Sort(report.Supported)
	report.Families = integrationFamilies(unknown)

	return report, nil
}

// readIntegrationKinds fills the kinds of groups from their discovery documents; a group that
// does not answer keeps none.
func readIntegrationKinds(ctx context.Context, k *kubeClient, groups []integrationGroup) {
	var (
		wg    sync.WaitGroup
		slots = make(chan struct{}, integrationKindReads)
	)

	for i := range groups {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			var doc struct {
				Resources []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
				} `json:"resources"`
			}

			g := &groups[i]
			if k.get(ctx, "/apis/"+g.Name+"/"+g.Version, &doc) != nil {
				return
			}

			for _, r := range doc.Resources {
				// "foos/status" and "foos/scale" are subresources of a kind already listed.
				if !strings.Contains(r.Name, "/") && r.Kind != "" && !slices.Contains(g.Kinds, r.Kind) {
					g.Kinds = append(g.Kinds, r.Kind)
				}
			}

			slices.Sort(g.Kinds)
		})
	}

	wg.Wait()
}

// isBuiltinGroup reports whether name is a Kubernetes group ("apps", "batch",
// "networking.k8s.io"…) rather than one an operator added. *.x-k8s.io are SIG projects
// installed on purpose (Cluster API, Kueue…): not built in.
func isBuiltinGroup(name string) bool {
	return !strings.Contains(name, ".") || name == "k8s.io" || strings.HasSuffix(name, ".k8s.io")
}

// integrationFamilyOf is the operator a group belongs to: its domain ("networking.istio.io" ->
// "istio.io"), one label longer on domains many projects share ("cluster.x-k8s.io").
func integrationFamilyOf(group string) string {
	labels := strings.Split(group, ".")
	if len(labels) <= 2 {
		return group
	}

	keep := 2
	if integrationSharedDomains[strings.Join(labels[len(labels)-2:], ".")] {
		keep = 3
	}

	return strings.Join(labels[max(0, len(labels)-keep):], ".")
}

// integrationFamilies groups groups by family, both sorted by name.
func integrationFamilies(groups []integrationGroup) []integrationFamily {
	byID := map[string][]integrationGroup{}
	for _, g := range groups {
		id := integrationFamilyOf(g.Name)
		byID[id] = append(byID[id], g)
	}

	out := make([]integrationFamily, 0, len(byID))
	for id, gs := range byID {
		slices.SortFunc(gs, func(a, b integrationGroup) int { return strings.Compare(a.Name, b.Name) })
		out = append(out, integrationFamily{ID: id, Groups: gs})
	}

	slices.SortFunc(out, func(a, b integrationFamily) int { return strings.Compare(a.ID, b.ID) })

	return out
}

// IntegrationIssueURL is the GitHub page opening a pre-filled integration request on repo
// ("owner/name") for familyJSON (one family, with only the groups the user picked); app names
// the app and platform ("Ichor 1.4.0 (Android)"). Only group, version and kind names go in.
func IntegrationIssueURL(repo, familyJSON, app string) (_ string, err error) {
	defer maskErr(&err)

	var f integrationFamily
	if err := json.Unmarshal([]byte(familyJSON), &f); err != nil {
		return "", err
	}

	q := url.Values{}
	q.Set("template", integrationTemplate)
	q.Set("title", "Integration: "+f.ID)
	q.Set("operator", f.ID)
	q.Set("groups", integrationGroupsText(f.Groups))
	q.Set("app", app)

	return "https://github.com/" + repo + "/issues/new?" + q.Encode(), nil
}

// IntegrationSearchURL is the GitHub search for the integration requests of repo naming
// family, so a request already open gets a 👍 rather than a twin.
func IntegrationSearchURL(repo, family string) string {
	q := url.Values{}
	q.Set("q", "is:issue label:"+integrationLabel+" "+family)

	return "https://github.com/" + repo + "/issues?" + q.Encode()
}

// integrationGroupsText is one "group/version: Kind, Kind" line per group, cut at
// integrationIssueText.
func integrationGroupsText(groups []integrationGroup) string {
	var b strings.Builder

	for _, g := range groups {
		line := g.Name + "/" + g.Version
		if len(g.Kinds) > 0 {
			line += ": " + strings.Join(g.Kinds, ", ")
		}

		if b.Len()+len(line)+1 > integrationIssueText {
			b.WriteString("…")

			break
		}

		if b.Len() > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(line)
	}

	return b.String()
}

func demoIntegrations() integrationReport {
	return integrationReport{
		Families: []integrationFamily{
			{ID: "kyverno.io", Groups: []integrationGroup{
				{Name: "kyverno.io", Version: "v1", Kinds: []string{"ClusterPolicy", "Policy"}},
				{Name: "reports.kyverno.io", Version: "v1", Kinds: []string{"ClusterEphemeralReport", "EphemeralReport"}},
			}},
			{ID: "metallb.io", Groups: []integrationGroup{
				{Name: "metallb.io", Version: "v1beta1", Kinds: []string{"BGPPeer", "IPAddressPool", "L2Advertisement"}},
			}},
			{ID: "traefik.io", Groups: []integrationGroup{
				{Name: "traefik.io", Version: "v1alpha1", Kinds: []string{"IngressRoute", "Middleware", "TLSOption"}},
			}},
		},
		Supported: []string{groupArgo, groupCertManager, groupLonghorn, groupMonitoring, groupCNPG},
	}
}
