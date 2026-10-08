package ichorgo

import (
	"context"
	"sync"
	"time"
)

// integrationSpec is a project the app reads through the Kubernetes API, and how to tell the
// cluster runs it: the custom resources (API groups) of its operator, else its pods (their
// images, matched with the app catalog) or its Services.
type integrationSpec struct {
	ID      string // catalog app id: the icon, the image match and the inventory hint
	Name    string // a proper noun: never translated
	Website string
	// Groups the readers ask; the first one tells whether the project is installed (any of
	// them when AnyGroup). Without any, the project is found by its running pods (catalog id ID).
	Groups   []string
	AnyGroup bool
	// Resource of Groups[0] that must be served too, when the group is shared with other
	// projects ("" when the group is enough).
	Resource string
	// ServiceKind also finds a project without API by its Services: a promMatch kind.
	ServiceKind string
}

// integrationSpecs lists every project the app integrates with, in the order Settings shows
// them. A reader that asks a new API group adds it here: TestIntegrationsCoverEveryGroup fails
// otherwise.
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
	{ID: "castai", Name: "CAST AI", Website: "https://cast.ai", Groups: []string{groupCastAI}},
	{ID: "cilium", Name: "Cilium", Website: "https://cilium.io", Groups: []string{groupCilium}},
	// Calico's CRDs come with the Kubernetes datastore; with the etcd one only its API server tells.
	{ID: "calico", Name: "Calico", Website: "https://docs.tigera.io/calico/latest/about/", Groups: []string{groupCalicoCRD, groupCalico}, AnyGroup: true},
	{ID: "external-secrets", Name: "External Secrets Operator", Website: "https://external-secrets.io", Groups: []string{groupExternalSecrets}},
	{ID: "gateway-api", Name: "Gateway API", Website: "https://gateway-api.sigs.k8s.io", Groups: []string{groupGatewayAPI}},
	{ID: "prometheus", Name: "Prometheus", Website: "https://prometheus.io", ServiceKind: "prometheus"},
	{ID: "thanos", Name: "Thanos", Website: "https://thanos.io", ServiceKind: "thanos"},
	{ID: "mimir", Name: "Mimir", Website: "https://grafana.com/oss/mimir/", ServiceKind: "mimir"},
	{ID: "victoriametrics", Name: "VictoriaMetrics", Website: "https://victoriametrics.com", ServiceKind: "victoriametrics"},
}

// groupGatewayAPI is the Gateway API's group: HTTPRoutes and Gateways.
const groupGatewayAPI = "gateway.networking.k8s.io"

// How an integration was found.
const (
	detectedByAPI       = "api"
	detectedByPods      = "pods"
	detectedByServices  = "services"
	detectedByInventory = "inventory" // the pods could not be listed: the inventory's hint
)

// supportedIntegration is one project in Settings: what it is, and whether the cluster runs it.
type supportedIntegration struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Icon    string   `json:"icon"` // bundled icon (assets/appicons/<icon>.webp), "" when none
	Website string   `json:"website"`
	Groups  []string `json:"groups"`
	// Detected on the cluster; always false when Checked is.
	Detected bool `json:"detected"`
	// Via is how it was found (detectedByXxx), "" when not detected.
	Via string `json:"via,omitempty"`
	// Version: the API version served for Groups[0], or the image tag of its pods; "" when unknown.
	Version string `json:"version,omitempty"`
	// Namespace it runs in, when found by its pods or Services.
	Namespace string `json:"namespace,omitempty"`
}

type supportedIntegrations struct {
	// Checked is false for the list alone (SupportedIntegrations), true once a cluster was asked.
	Checked bool                   `json:"checked"`
	Items   []supportedIntegration `json:"items"`
}

// detection is how one integration was detected; the zero value is "not detected".
type detection struct {
	via, version, namespace string
}

// SupportedIntegrations lists the projects the app integrates with, without asking any cluster:
// {"checked":false,"items":[{id,name,icon,website,groups,detected:false}]}.
func SupportedIntegrations() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(supportedIntegrations{Items: integrationList(func(integrationSpec) detection { return detection{} })})
}

// KubeSupportedIntegrations lists the projects the app integrates with and whether the cluster runs
// each one (os:admin); SupportedIntegrations' JSON with "checked":true and, per detected project, "via"
// (api, pods, services, inventory), "version" and "namespace". Operators are found by their API
// groups; projects without one by the images of the running pods (the app catalog) or, for
// metrics backends, their Services. hints (see KubeDataServices) stand in for the pods when
// they cannot be listed. kubeServer: see KubePods.
func KubeSupportedIntegrations(configYAML, contextName, kubeServer, hints string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoSupportedIntegrations, func(ctx context.Context, k *kubeClient) (supportedIntegrations, error) {
		return readSupportedIntegrations(ctx, k, parseHints(hints))
	})
}

func readSupportedIntegrations(ctx context.Context, k *kubeClient, hints hintSet) (supportedIntegrations, error) {
	var (
		groups      map[string]string
		groupsErr   error
		pods        map[string]detection
		podsErr     error
		services    map[string]detection
		resourceFor = map[string]bool{}
		wg          sync.WaitGroup
	)

	wg.Go(func() { groups, groupsErr = readAPIGroups(ctx, k) })
	wg.Go(func() { pods, podsErr = runningApps(ctx, k) })
	wg.Go(func() { services = queryServices(ctx, k) })
	wg.Wait()

	if groupsErr != nil {
		return supportedIntegrations{}, groupsErr
	}

	// Shared groups: ask whether the project's own resource is served, in parallel.
	var mu sync.Mutex

	for _, s := range integrationSpecs {
		if version, ok := groups[firstGroup(s)]; ok && s.Resource != "" {
			wg.Go(func() {
				served := servesResource(ctx, k, s.Groups[0], version, s.Resource)

				mu.Lock()
				resourceFor[s.ID] = served
				mu.Unlock()
			})
		}
	}

	wg.Wait()

	items := integrationList(func(s integrationSpec) detection {
		if len(s.Groups) > 0 {
			version, ok := servedGroup(s, groups)
			if !ok || s.Resource != "" && !resourceFor[s.ID] {
				return detection{}
			}

			return detection{via: detectedByAPI, version: version}
		}

		if f, ok := pods[s.ID]; ok {
			return f
		}

		if f, ok := services[s.ServiceKind]; ok && s.ServiceKind != "" {
			return f
		}

		if podsErr != nil && hints[s.ID] {
			return detection{via: detectedByInventory}
		}

		return detection{}
	})

	return supportedIntegrations{Checked: true, Items: items}, nil
}

func firstGroup(s integrationSpec) string {
	if len(s.Groups) == 0 {
		return ""
	}

	return s.Groups[0]
}

// servedGroup is the version of the group that tells s is installed: its first one, or with
// AnyGroup the first one the cluster serves.
func servedGroup(s integrationSpec, groups map[string]string) (string, bool) {
	candidates := s.Groups[:1]
	if s.AnyGroup {
		candidates = s.Groups
	}

	for _, g := range candidates {
		if version, ok := groups[g]; ok {
			return version, true
		}
	}

	return "", false
}

// runningApps maps the catalog ids of the running pods' images to the first pod found
// (namespace, image tag), from one listing of every running pod.
func runningApps(ctx context.Context, k *kubeClient) (map[string]detection, error) {
	pods, err := listDSPods(ctx, k, "")
	if err != nil {
		return nil, err
	}

	catalog := loadAppCatalog()
	out := map[string]detection{}

	for _, p := range pods {
		if p.Status.Phase != "Running" {
			continue
		}

		for _, c := range p.Spec.Containers {
			ref := parseImageRef(c.Image)
			if id := catalog.identify(ref); id.app != nil {
				if _, seen := out[id.app.ID]; !seen {
					out[id.app.ID] = detection{via: detectedByPods, version: ref.Tag, namespace: p.Metadata.Namespace}
				}
			}
		}
	}

	return out, nil
}

// queryServices maps the kinds of metrics query APIs (promMatch) to the likeliest Service;
// empty when the Services cannot be listed.
func queryServices(ctx context.Context, k *kubeClient) map[string]detection {
	out := map[string]detection{}

	d, err := discoverProm(ctx, k)
	if err != nil {
		return out
	}

	for _, src := range d.Sources {
		if _, seen := out[src.Kind]; !seen {
			out[src.Kind] = detection{via: detectedByServices, namespace: src.Namespace}
		}
	}

	return out
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

// integrationList builds the items, detect telling how each one was found.
func integrationList(detect func(integrationSpec) detection) []supportedIntegration {
	catalog := loadAppCatalog()
	out := make([]supportedIntegration, 0, len(integrationSpecs))

	for _, s := range integrationSpecs {
		// Bundled icons are named after the catalog id, as in the inventory.
		icon := ""
		if app := catalog.byName[s.ID]; app != nil && app.ID == s.ID && app.hasIcon() {
			icon = app.ID
		}

		f := detect(s)
		out = append(out, supportedIntegration{
			ID: s.ID, Name: s.Name, Icon: icon, Website: s.Website, Groups: append([]string{}, s.Groups...),
			Detected: f.via != "", Via: f.via, Version: f.version, Namespace: f.namespace,
		})
	}

	return out
}

// demoSupportedIntegrations reports what the demo cluster's other screens show.
func demoSupportedIntegrations() supportedIntegrations {
	now := time.Now()
	ds := demoDataServices(now)
	api := map[string]bool{
		"argo-cd":        demoArgoCD(now).Installed,
		"flux":           demoFlux(now).Installed,
		"cloudnative-pg": ds.CNPG != nil,
		"mariadb":        ds.MariaDB != nil,
		"percona-xtradb": ds.Percona != nil,
		"dragonfly":      ds.Dragonfly != nil,
		"longhorn":       ds.Longhorn != nil,
		"rook":           ds.Ceph != nil,
		"velero":         ds.Velero != nil,
		"cert-manager":   ds.CertManager != nil,
		"castai":         ds.CastAI != nil,
		"cilium":         demoCiliumStatus().Installed,
		"gateway-api":    true,
	}

	services := map[string]detection{}
	for _, src := range demoPromDiscovery().Sources {
		if _, seen := services[src.Kind]; !seen {
			services[src.Kind] = detection{via: detectedByServices, namespace: src.Namespace}
		}
	}

	return supportedIntegrations{Checked: true, Items: integrationList(func(s integrationSpec) detection {
		switch {
		case api[s.ID]:
			return detection{via: detectedByAPI}
		case s.ID == "garage" && ds.Garage != nil:
			return detection{via: detectedByPods}
		case s.ServiceKind != "":
			return services[s.ServiceKind]
		}

		return detection{}
	})}
}
