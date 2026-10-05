package ichorgo

import (
	"context"
	"time"
)

// integrationSpec is a project the app reads through the Kubernetes API: the custom resources
// (API groups) of its operator, or its pods when it has none.
type integrationSpec struct {
	ID      string // catalog app id: the icon and the inventory hint
	Name    string // a proper noun: never translated
	Website string
	// Groups the readers ask; the first one tells whether the project is installed.
	Groups []string
	// Resource of Groups[0] that must be served too, when the group is shared with other
	// projects ("" when the group is enough).
	Resource string
}

// integrationSpecs lists every project the app integrates with, in the order Settings shows
// them. A reader that asks a new API group adds it here: TestIntegrationsCoverEveryGroup fails
// otherwise. Garage has no API of its own: it is detected from the inventory (hints).
var integrationSpecs = []integrationSpec{
	{ID: "argo-cd", Name: "Argo CD", Website: "https://argo-cd.readthedocs.io", Groups: []string{groupArgo}, Resource: "applications"},
	{ID: "flux", Name: "Flux", Website: "https://fluxcd.io", Groups: []string{groupFluxKustomize, groupFluxHelm, groupFluxSource}},
	{ID: "cloudnative-pg", Name: "CloudNativePG", Website: "https://cloudnative-pg.io", Groups: []string{groupCNPG, groupBarmanPlug}},
	{ID: "mariadb", Name: "MariaDB Operator", Website: "https://github.com/mariadb-operator/mariadb-operator", Groups: []string{groupMariaDB}},
	{ID: "percona-xtradb", Name: "Percona XtraDB Cluster", Website: "https://docs.percona.com/percona-operator-for-mysql/pxc/", Groups: []string{groupPercona}},
	{ID: "dragonfly", Name: "Dragonfly", Website: "https://www.dragonflydb.io", Groups: []string{groupDragonfly}},
	{ID: "longhorn", Name: "Longhorn", Website: "https://longhorn.io", Groups: []string{groupLonghorn}},
	{ID: "rook", Name: "Rook Ceph", Website: "https://rook.io", Groups: []string{groupCeph}},
	{ID: "garage", Name: "Garage", Website: "https://garagehq.deuxfleurs.fr"},
	{ID: "velero", Name: "Velero", Website: "https://velero.io", Groups: []string{groupVelero}},
	{ID: "cert-manager", Name: "cert-manager", Website: "https://cert-manager.io", Groups: []string{groupCertManager, groupACME}},
	{ID: "cilium", Name: "Cilium", Website: "https://cilium.io", Groups: []string{groupCilium}},
	{ID: "gateway-api", Name: "Gateway API", Website: "https://gateway-api.sigs.k8s.io", Groups: []string{groupGatewayAPI}},
}

// groupGatewayAPI is the Gateway API's group: HTTPRoutes and Gateways.
const groupGatewayAPI = "gateway.networking.k8s.io"

// integration is one project in Settings: what it is, and whether the cluster runs it.
type integration struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Icon    string   `json:"icon"` // bundled icon (assets/appicons/<icon>.webp), "" when none
	Website string   `json:"website"`
	Groups  []string `json:"groups"`
	// Detected on the cluster; always false when Checked is.
	Detected bool `json:"detected"`
	// API version the cluster serves for Groups[0], "" when not detected or without a group.
	Version string `json:"version,omitempty"`
}

type integrations struct {
	// Checked is false for the list alone (Integrations), true once a cluster was asked.
	Checked bool          `json:"checked"`
	Items   []integration `json:"items"`
}

// Integrations lists the projects the app integrates with, without asking any cluster:
// {"checked":false,"items":[{id,name,icon,website,groups,detected:false}]}.
func Integrations() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(integrations{Items: integrationList(func(integrationSpec) (bool, string) { return false, "" })})
}

// KubeIntegrations lists the projects the app integrates with and whether the cluster runs
// each one, from a single API discovery (os:admin); Integrations' JSON with "checked":true.
// hints: see KubeDataServices; a project without an API (Garage) is detected when hinted.
// kubeServer: see KubePods.
func KubeIntegrations(configYAML, contextName, kubeServer, hints string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoIntegrations, func(ctx context.Context, k *kubeClient) (integrations, error) {
		return readIntegrations(ctx, k, parseHints(hints))
	})
}

func readIntegrations(ctx context.Context, k *kubeClient, hints hintSet) (integrations, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return integrations{}, err
	}

	items := integrationList(func(s integrationSpec) (bool, string) {
		if len(s.Groups) == 0 {
			return hints[s.ID], ""
		}

		version, ok := groups[s.Groups[0]]
		if ok && s.Resource != "" {
			ok = servesResource(ctx, k, s.Groups[0], version, s.Resource)
		}

		if !ok {
			return false, ""
		}

		return true, version
	})

	return integrations{Checked: true, Items: items}, nil
}

// servesResource reports whether group/version serves resource, false when it cannot tell.
func servesResource(ctx context.Context, k *kubeClient, group, version, resource string) bool {
	var list struct {
		Resources []struct {
			Name string `json:"name"`
		} `json:"resources"`
	}

	if err := k.get(ctx, "/apis/"+group+"/"+version, &list); err != nil {
		return false
	}

	for _, r := range list.Resources {
		if r.Name == resource {
			return true
		}
	}

	return false
}

// integrationList builds the items, detect telling each one's status and API version.
func integrationList(detect func(integrationSpec) (bool, string)) []integration {
	catalog := loadAppCatalog()
	out := make([]integration, 0, len(integrationSpecs))

	for _, s := range integrationSpecs {
		icon := ""
		if app := catalog.byName[s.ID]; app != nil && app.ID == s.ID && app.hasIcon() {
			icon = app.ID
			if app.Icon != nil {
				icon = *app.Icon
			}
		}

		detected, version := detect(s)
		out = append(out, integration{
			ID: s.ID, Name: s.Name, Icon: icon, Website: s.Website,
			Groups: append([]string{}, s.Groups...), Detected: detected, Version: version,
		})
	}

	return out
}

// demoIntegrations reports what the demo cluster's other screens show.
func demoIntegrations() integrations {
	now := time.Now()
	ds := demoDataServices(now)
	running := map[string]bool{
		"argo-cd":        demoArgoCD(now).Installed,
		"flux":           demoFlux(now).Installed,
		"cloudnative-pg": ds.CNPG != nil,
		"mariadb":        ds.MariaDB != nil,
		"percona-xtradb": ds.Percona != nil,
		"dragonfly":      ds.Dragonfly != nil,
		"longhorn":       ds.Longhorn != nil,
		"rook":           ds.Ceph != nil,
		"garage":         ds.Garage != nil,
		"velero":         ds.Velero != nil,
		"cert-manager":   ds.CertManager != nil,
		"cilium":         demoCiliumStatus().Installed,
		"gateway-api":    true,
	}

	return integrations{Checked: true, Items: integrationList(func(s integrationSpec) (bool, string) { return running[s.ID], "" })}
}
