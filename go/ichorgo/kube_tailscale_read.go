package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// tsCondition is a metav1.Condition as the operator writes it; ObservedGeneration behind the
// object's generation means the operator has not looked at the latest spec yet.
type tsCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	ObservedGeneration int64  `json:"observedGeneration"`
}

type tsMeta struct {
	Name       string `json:"name"`
	Generation int64  `json:"generation"`
}

type tsDeviceObject struct {
	Hostname        string   `json:"hostname"`
	TailnetIPs      []string `json:"tailnetIPs"`
	StaticEndpoints []string `json:"staticEndpoints"`
}

// tsConnectorObject is a Connector (tailscale.com/v1alpha1).
type tsConnectorObject struct {
	Metadata tsMeta `json:"metadata"`
	Spec     struct {
		Tags         []string `json:"tags"`
		Hostname     string   `json:"hostname"`
		ProxyClass   string   `json:"proxyClass"`
		Replicas     *int     `json:"replicas"`
		ExitNode     bool     `json:"exitNode"`
		SubnetRouter *struct {
			AdvertiseRoutes []string `json:"advertiseRoutes"`
		} `json:"subnetRouter"`
		AppConnector *struct {
			Routes []string `json:"routes"`
		} `json:"appConnector"`
	} `json:"spec"`
	Status struct {
		Conditions     []tsCondition    `json:"conditions"`
		SubnetRoutes   string           `json:"subnetRoutes"`
		IsExitNode     bool             `json:"isExitNode"`
		IsAppConnector bool             `json:"isAppConnector"`
		TailnetIPs     []string         `json:"tailnetIPs"`
		Hostname       string           `json:"hostname"`
		Devices        []tsDeviceObject `json:"devices"`
	} `json:"status"`
}

// tsProxyGroupObject is a ProxyGroup.
type tsProxyGroupObject struct {
	Metadata tsMeta `json:"metadata"`
	Spec     struct {
		Type       string   `json:"type"`
		Tags       []string `json:"tags"`
		Replicas   *int     `json:"replicas"`
		ProxyClass string   `json:"proxyClass"`
	} `json:"spec"`
	Status struct {
		Conditions []tsCondition    `json:"conditions"`
		Devices    []tsDeviceObject `json:"devices"`
		URL        string           `json:"url"`
	} `json:"status"`
}

// tsProxyClassObject is a ProxyClass: only its readiness matters here.
type tsProxyClassObject struct {
	Metadata tsMeta `json:"metadata"`
	Status   struct {
		Conditions []tsCondition `json:"conditions"`
	} `json:"status"`
}

// tsDNSConfigObject is a DNSConfig.
type tsDNSConfigObject struct {
	Metadata tsMeta `json:"metadata"`
	Status   struct {
		Conditions []tsCondition `json:"conditions"`
		Nameserver *struct {
			IP string `json:"ip"`
		} `json:"nameserver"`
	} `json:"status"`
}

// tsDeploymentObject is a Deployment, enough to tell the operator and its readiness.
type tsDeploymentObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int `json:"replicas"`
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

// tsRaw is what the reads brought back; each goroutine writes its own fields only.
// classesRead is false when the ProxyClass list failed: no missing-ProxyClass check then.
type tsRaw struct {
	connectors   []tsConnectorObject
	proxyGroups  []tsProxyGroupObject
	proxyClasses []tsProxyClassObject
	dnsConfigs   []tsDNSConfigObject
	operator     *tsDeploymentObject
	classesRead  bool
	errs         [5]error
}

// readTailscale lists the operator's four kinds and finds its Deployment, in parallel. A kind
// the operator does not serve yet (404 on an older release) is "none"; any other failure lands
// in the report's error and the rest still shows.
func readTailscale(ctx context.Context, k *kubeClient) (tsReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return tsReport{}, err
	}

	return readTailscaleWithin(ctx, k, groups), nil
}

// readTailscaleWithin reads the operator through the API groups already read (the checkup's):
// not installed without tailscale.com. A list that fails is the report's Error.
func readTailscaleWithin(ctx context.Context, k *kubeClient, groups map[string]string) tsReport {
	version := groups[groupTailscale]
	if version == "" {
		return emptyTailscale()
	}

	base := "/apis/" + groupTailscale + "/" + version

	var (
		raw tsRaw
		wg  sync.WaitGroup
	)

	wg.Go(func() {
		var list kubeList[tsConnectorObject]
		raw.errs[0] = ignoreNotFound(getList(ctx, k, base+"/connectors", &list))
		raw.connectors = list.Items
	})
	wg.Go(func() {
		var list kubeList[tsProxyGroupObject]
		raw.errs[1] = ignoreNotFound(getList(ctx, k, base+"/proxygroups", &list))
		raw.proxyGroups = list.Items
	})
	wg.Go(func() {
		var list kubeList[tsProxyClassObject]
		raw.errs[2] = ignoreNotFound(getList(ctx, k, base+"/proxyclasses", &list))
		raw.proxyClasses, raw.classesRead = list.Items, raw.errs[2] == nil
	})
	wg.Go(func() {
		var list kubeList[tsDNSConfigObject]
		raw.errs[3] = ignoreNotFound(getList(ctx, k, base+"/dnsconfigs", &list))
		raw.dnsConfigs = list.Items
	})
	wg.Go(func() { raw.operator, raw.errs[4] = findTailscaleOperator(ctx, k) })

	wg.Wait()

	r := buildTailscale(raw)
	r.Version = version

	return r
}

func emptyTailscale() tsReport {
	return tsReport{
		Connectors: []tsConnector{}, ProxyGroups: []tsProxyGroup{}, ProxyClasses: []tsProxyClass{},
	}
}

// findTailscaleOperator is the operator Deployment: the Helm chart's label first, else a
// Deployment named like an operator that runs the catalog's tailscale image.
func findTailscaleOperator(ctx context.Context, k *kubeClient) (*tsDeploymentObject, error) {
	var labeled kubeList[tsDeploymentObject]
	if err := getList(ctx, k, "/apis/apps/v1/deployments?labelSelector="+url.QueryEscape(tsOperatorSelector), &labeled); err != nil {
		return nil, err
	}

	if len(labeled.Items) > 0 {
		return pickTailscaleOperator(labeled.Items, false), nil
	}

	var all kubeList[tsDeploymentObject]
	if err := getList(ctx, k, "/apis/apps/v1/deployments", &all); err != nil {
		return nil, err
	}

	return pickTailscaleOperator(all.Items, true), nil
}

// pickTailscaleOperator is the first Deployment (by namespace and name) that is the operator;
// match asks the image and name test, for Deployments not selected by the chart's label.
func pickTailscaleOperator(deps []tsDeploymentObject, match bool) *tsDeploymentObject {
	sorted := slices.Clone(deps)
	slices.SortFunc(sorted, func(a, b tsDeploymentObject) int {
		return cmp.Or(strings.Compare(a.Metadata.Namespace, b.Metadata.Namespace), strings.Compare(a.Metadata.Name, b.Metadata.Name))
	})

	for i := range sorted {
		if !match || (strings.Contains(sorted[i].Metadata.Name, "operator") && tsOperatorImage(sorted[i]) != "") {
			return &sorted[i]
		}
	}

	return nil
}

// tsOperatorImage is the Deployment's first container image the catalog names tailscale, "".
func tsOperatorImage(d tsDeploymentObject) string {
	catalog := loadAppCatalog()

	for _, c := range d.Spec.Template.Spec.Containers {
		if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "tailscale" {
			return c.Image
		}
	}

	return ""
}

// buildTailscale maps the objects to rows, flags the ProxyClasses named but missing, sorts
// (problems first), caps each list and counts.
func buildTailscale(raw tsRaw) tsReport {
	r := emptyTailscale()
	r.Installed = true
	r.Error = sectionError(raw.errs[:]...)
	r.Operator = tsOperatorRow(raw.operator)

	for _, c := range raw.connectors {
		r.Connectors = append(r.Connectors, tsConnectorRow(c))
	}
	for _, g := range raw.proxyGroups {
		r.ProxyGroups = append(r.ProxyGroups, tsProxyGroupRow(g))
	}
	for _, c := range raw.proxyClasses {
		r.ProxyClasses = append(r.ProxyClasses, tsProxyClass{
			Name:    c.Metadata.Name,
			tsState: tsStateOf(c.Status.Conditions, "ProxyClassReady", c.Metadata.Generation, tsHealthWarning),
		})
	}

	if len(raw.dnsConfigs) > 0 {
		d := slices.MinFunc(raw.dnsConfigs, func(a, b tsDNSConfigObject) int { return strings.Compare(a.Metadata.Name, b.Metadata.Name) })
		r.DNSConfig = &tsDNSConfig{
			Name:    d.Metadata.Name,
			tsState: tsStateOf(d.Status.Conditions, "NameserverReady", d.Metadata.Generation, tsHealthWarning),
		}
		if d.Status.Nameserver != nil {
			r.DNSConfig.NameserverIP = d.Status.Nameserver.IP
		}
	}

	linkProxyClasses(&r, raw.classesRead)
	finishTailscale(&r)

	return r
}

// finishTailscale sorts every list problems first, caps it at tsListMax and counts the rows.
// The demo report goes through it too.
func finishTailscale(r *tsReport) {
	sortTS(r.Connectors, func(c tsConnector) (string, string) { return c.Health, c.Name })
	sortTS(r.ProxyGroups, func(g tsProxyGroup) (string, string) { return g.Health, g.Name })
	sortTS(r.ProxyClasses, func(c tsProxyClass) (string, string) { return c.Health, c.Name })

	r.Truncated = len(r.Connectors) > tsListMax || len(r.ProxyGroups) > tsListMax || len(r.ProxyClasses) > tsListMax
	r.Connectors = r.Connectors[:min(len(r.Connectors), tsListMax)]
	r.ProxyGroups = r.ProxyGroups[:min(len(r.ProxyGroups), tsListMax)]
	r.ProxyClasses = r.ProxyClasses[:min(len(r.ProxyClasses), tsListMax)]

	var healths []string
	if r.Operator != nil {
		healths = append(healths, r.Operator.Health)
	}
	for _, c := range r.Connectors {
		healths = append(healths, c.Health)
	}
	for _, g := range r.ProxyGroups {
		healths = append(healths, g.Health)
	}
	for _, c := range r.ProxyClasses {
		healths = append(healths, c.Health)
	}
	if r.DNSConfig != nil {
		healths = append(healths, r.DNSConfig.Health)
	}

	r.Counts = tsCounts{}
	for _, h := range healths {
		switch h {
		case tsHealthCritical:
			r.Counts.Critical++
		case tsHealthWarning:
			r.Counts.Warning++
		default:
			r.Counts.OK++
		}
	}
}

// linkProxyClasses counts each ProxyClass's users and, when the ProxyClasses could be read,
// raises a Connector or ProxyGroup that names a missing one to a warning.
func linkProxyClasses(r *tsReport, classesRead bool) {
	used := map[string]int{}
	for _, c := range r.Connectors {
		used[c.ProxyClass]++
	}
	for _, g := range r.ProxyGroups {
		used[g.ProxyClass]++
	}

	known := map[string]bool{}
	for i := range r.ProxyClasses {
		r.ProxyClasses[i].UsedBy = used[r.ProxyClasses[i].Name]
		known[r.ProxyClasses[i].Name] = true
	}

	if !classesRead {
		return
	}

	missing := func(class string, s *tsState) {
		if class != "" && !known[class] && s.Health == tsHealthOK {
			s.Health, s.Reason = tsHealthWarning, tsReasonProxyClassMissing
		}
	}
	for i := range r.Connectors {
		missing(r.Connectors[i].ProxyClass, &r.Connectors[i].tsState)
	}
	for i := range r.ProxyGroups {
		missing(r.ProxyGroups[i].ProxyClass, &r.ProxyGroups[i].tsState)
	}
}

func tsOperatorRow(d *tsDeploymentObject) *tsOperator {
	if d == nil {
		return nil
	}

	op := &tsOperator{
		Namespace: d.Metadata.Namespace,
		Name:      d.Metadata.Name,
		Ready:     d.Status.ReadyReplicas,
		Desired:   1,
		Image:     tsOperatorImage(*d),
		Health:    tsHealthOK,
	}
	if d.Spec.Replicas != nil {
		op.Desired = *d.Spec.Replicas
	}
	if op.Image == "" && len(d.Spec.Template.Spec.Containers) > 0 {
		op.Image = d.Spec.Template.Spec.Containers[0].Image
	}

	switch {
	case op.Ready < op.Desired:
		op.Health = tsHealthCritical
	case op.Desired == 0:
		op.Health = tsHealthWarning // scaled to zero: nothing is reconciled
	}

	return op
}

func tsConnectorRow(c tsConnectorObject) tsConnector {
	s := c.Status
	row := tsConnector{
		Name:             c.Metadata.Name,
		Hostname:         cmp.Or(s.Hostname, c.Spec.Hostname),
		TailnetIPs:       nonNil(s.TailnetIPs),
		SubnetRoutes:     s.SubnetRoutes,
		AdvertisedRoutes: []string{},
		// The spec's role while the status has none yet (a connector that is not ready).
		ExitNode:           s.IsExitNode || c.Spec.ExitNode,
		AppConnector:       s.IsAppConnector || c.Spec.AppConnector != nil,
		AppConnectorRoutes: []string{},
		Tags:               nonNil(c.Spec.Tags),
		ProxyClass:         c.Spec.ProxyClass,
		Replicas:           1,
		Devices:            tsDevices(s.Devices),
		tsState:            tsStateOf(s.Conditions, "ConnectorReady", c.Metadata.Generation, tsHealthCritical),
	}

	if c.Spec.Replicas != nil {
		row.Replicas = *c.Spec.Replicas
	}
	if c.Spec.SubnetRouter != nil {
		row.AdvertisedRoutes = nonNil(c.Spec.SubnetRouter.AdvertiseRoutes)
	}
	if c.Spec.AppConnector != nil {
		row.AppConnectorRoutes = nonNil(c.Spec.AppConnector.Routes)
	}

	return row
}

// tsProxyGroupRow maps a ProxyGroup: all proxies up is ok, some is a warning, none is
// critical. An operator without ProxyGroupAvailable (older releases) says it through
// ProxyGroupReady alone.
func tsProxyGroupRow(g tsProxyGroupObject) tsProxyGroup {
	row := tsProxyGroup{
		Name:       g.Metadata.Name,
		Type:       g.Spec.Type,
		Replicas:   2,
		ProxyClass: g.Spec.ProxyClass,
		Tags:       nonNil(g.Spec.Tags),
		URL:        g.Status.URL,
		Devices:    tsDevices(g.Status.Devices),
		tsState:    tsStateOf(g.Status.Conditions, "ProxyGroupReady", g.Metadata.Generation, tsHealthCritical),
	}

	if g.Spec.Replicas != nil {
		row.Replicas = *g.Spec.Replicas
	}

	avail, hasAvail := tsConditionOf(g.Status.Conditions, "ProxyGroupAvailable")
	row.Available = row.Ready || (hasAvail && avail.Status == "True" && avail.ObservedGeneration >= g.Metadata.Generation)

	if !row.Ready && row.Available {
		row.Health = tsHealthWarning
	}

	return row
}

func tsConditionOf(conds []tsCondition, typ string) (tsCondition, bool) {
	for _, c := range conds {
		if c.Type == typ {
			return c, true
		}
	}

	return tsCondition{}, false
}

// tsStateOf is a row's readiness from its condition typ: True is ok, False is falseHealth,
// absent, Unknown or stale (observed before the latest spec) a warning.
func tsStateOf(conds []tsCondition, typ string, generation int64, falseHealth string) tsState {
	c, ok := tsConditionOf(conds, typ)

	switch {
	case !ok:
		return tsState{Reason: tsReasonNotReconciled, Health: tsHealthWarning}
	case c.ObservedGeneration != 0 && c.ObservedGeneration < generation:
		return tsState{Reason: tsReasonReconciling, Message: c.Message, Health: tsHealthWarning}
	case c.Status == "True":
		return tsState{Ready: true, Reason: c.Reason, Message: c.Message, Health: tsHealthOK}
	case c.Status == "False":
		return tsState{Reason: c.Reason, Message: c.Message, Health: falseHealth}
	default:
		return tsState{Reason: c.Reason, Message: c.Message, Health: tsHealthWarning}
	}
}

func tsDevices(devs []tsDeviceObject) []tsDevice {
	out := []tsDevice{}
	for _, d := range devs {
		out = append(out, tsDevice{Hostname: d.Hostname, TailnetIPs: nonNil(d.TailnetIPs), StaticEndpoints: nonNil(d.StaticEndpoints)})
	}

	return out
}

// tsRank orders the healths: critical, warning, then ok.
func tsRank(health string) int {
	switch health {
	case tsHealthCritical:
		return 0
	case tsHealthWarning:
		return 1
	default:
		return 2
	}
}

func sortTS[T any](rows []T, key func(T) (health, name string)) {
	slices.SortFunc(rows, func(a, b T) int {
		ha, na := key(a)
		hb, nb := key(b)

		return cmp.Or(cmp.Compare(tsRank(ha), tsRank(hb)), strings.Compare(na, nb))
	})
}
