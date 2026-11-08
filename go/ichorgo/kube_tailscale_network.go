package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/kube/netmap"
)

// The network map of one address the Tailscale operator serves, from the tailnet name through
// each proxy to the pods and nodes (ingress),
//
//	tailnet name → ProxyGroup → Proxy → (Ingress) → Service → Pod → node
//
// or from the Service through the proxies to the tailnet target (egress),
//
//	Service → ProxyGroup → Proxy → tailnet target
//
// One map per address, not one for the cluster: a few addresses × a few replicas is already
// more boxes than a phone shows. The ProxyGroup column is left out for an object with a proxy
// of its own.

// The kinds the Tailscale map adds to netmap's.
const (
	netKindProxyGroup = "ProxyGroup"
	netKindProxy      = "Proxy"
)

// The labels the operator puts on its proxy pods, in its own namespace.
const (
	tsLabelManaged    = "tailscale.com/managed"
	tsLabelParent     = "tailscale.com/parent-resource"
	tsLabelParentNS   = "tailscale.com/parent-resource-ns"
	tsLabelParentType = "tailscale.com/parent-resource-type"
	tsParentGroup     = "proxygroup"
	tsZoneLabel       = "topology.kubernetes.io/zone"
)

// KubeTailscaleNetwork draws how traffic crosses the tailnet for the Service or Ingress (kind)
// namespace/name the operator serves (os:admin): a netmap, as KubeArgoNetwork's. An object the
// operator does not serve has an empty map. kubeServer: see KubePods.
func KubeTailscaleNetwork(configYAML, contextName, kubeServer, kind, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if kind != tsKindService && kind != tsKindIngress {
		return "", fmt.Errorf("kind %q is neither Service nor Ingress", kind)
	}

	if err := validateKubeName(strings.ToLower(kind), namespace, name); err != nil {
		return "", err
	}

	demo := func() netmap.Map { return demoTailscaleNetwork(kind, namespace, name) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (netmap.Map, error) {
		return readTailscaleNetwork(ctx, k, kind, namespace, name)
	})
}

// tsNetInput is what one map is built from.
type tsNetInput struct {
	kind     string
	object   json.RawMessage  // the Service or Ingress
	services []netmap.Service // the object's namespace's
	pods     []netmap.Pod     // the object's namespace's
	proxies  []netmap.Pod     // every proxy pod of the operator
	groups   []tsProxyGroupObject
	nodes    []netmap.NodeObject
	zones    map[string]string // node name -> zone
}

func readTailscaleNetwork(ctx context.Context, k *kubeClient, kind, namespace, name string) (netmap.Map, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return netmap.Map{}, err
	}

	version, ok := groups[groupTailscale]
	if !ok {
		return netmap.Map{}, &kubeAPIError{Code: 404, Reason: "NotFound", Message: "the Tailscale operator is not installed"}
	}

	in := tsNetInput{kind: kind}
	base := "/api/v1/namespaces/" + url.PathEscape(namespace)

	path := base + "/services/" + url.PathEscape(name)
	if kind == tsKindIngress {
		path = "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(namespace) + "/ingresses/" + url.PathEscape(name)
	}

	if err := k.get(ctx, path, &in.object); err != nil {
		return netmap.Map{}, err
	}

	var (
		wg   sync.WaitGroup
		errs [3]error
	)

	wg.Go(func() { in.services, errs[0] = listObjects[netmap.Service](ctx, k, base+"/services") })
	wg.Go(func() { in.pods, errs[1] = readNetPods(ctx, k, base+"/pods") })
	// The proxies run in the operator's namespace: one cluster-wide list by its label.
	wg.Go(func() {
		in.proxies, errs[2] = readNetPods(ctx, k, "/api/v1/pods?labelSelector="+url.QueryEscape(tsLabelManaged+"=true"))
	})
	// Without the ProxyGroups the boxes lose their type and devices, not the map.
	wg.Go(func() {
		in.groups, _ = listObjects[tsProxyGroupObject](ctx, k, "/apis/"+groupTailscale+"/"+version+"/proxygroups")
	})
	// Node readiness and zones only colour and label: without them the map still shows.
	wg.Go(func() { in.nodes, in.zones = readNetNodes(ctx, k) })
	wg.Wait()

	if err := cmp.Or(errs[:]...); err != nil {
		return netmap.Map{}, err
	}

	return buildTailscaleNetwork(in), nil
}

// readNetNodes is the cluster's nodes and the zone of each; nothing when the list fails.
func readNetNodes(ctx context.Context, k *kubeClient) ([]netmap.NodeObject, map[string]string) {
	raws, err := listObjects[json.RawMessage](ctx, k, "/api/v1/nodes")
	if err != nil {
		return nil, nil
	}

	nodes := make([]netmap.NodeObject, 0, len(raws))
	zones := map[string]string{}

	for _, raw := range raws {
		var (
			n      netmap.NodeObject
			labels labeledObject
		)

		if json.Unmarshal(raw, &n) != nil || json.Unmarshal(raw, &labels) != nil {
			continue
		}

		nodes = append(nodes, n)
		if z := labels.Metadata.Labels[tsZoneLabel]; z != "" {
			zones[n.Metadata.Name] = z
		}
	}

	return nodes, zones
}

// buildTailscaleNetwork draws the object's map; one the operator does not serve is empty.
func buildTailscaleNetwork(in tsNetInput) netmap.Map {
	row, ok := tsExposedOf(in.kind, in.object)
	if !ok {
		return netmap.Finish(netmap.NewGraph(0), nil)
	}

	var group *tsProxyGroupObject
	if i := slices.IndexFunc(in.groups, func(g tsProxyGroupObject) bool { return g.Metadata.Name == row.ProxyGroup }); i >= 0 {
		group = &in.groups[i]
	}

	proxies := tsProxiesOf(row, in.proxies)

	var m netmap.Map
	if row.Direction == tsDirectionEgress {
		m = tsEgressMap(in, row, group, proxies)
	} else {
		m = tsIngressMap(in, row, group, proxies)
	}

	m.Problem = tsRootCause(m)

	return m
}

// tsProxiesOf are the proxy pods serving row: its ProxyGroup's, or its own (labelled with the
// object's kind, namespace and name).
func tsProxiesOf(row tsExposed, pods []netmap.Pod) []netmap.Pod {
	var out []netmap.Pod

	for _, p := range pods {
		l := p.Labels

		switch {
		case row.ProxyGroup != "":
			if l[tsLabelParentType] == tsParentGroup && l[tsLabelParent] == row.ProxyGroup {
				out = append(out, p)
			}
		case l[tsLabelParent] == row.Name && l[tsLabelParentNS] == row.Namespace && tsParentKind(l[tsLabelParentType]) == row.Kind:
			out = append(out, p)
		}
	}

	slices.SortFunc(out, func(a, b netmap.Pod) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// tsParentKind is the kind a proxy pod's parent-resource-type names.
func tsParentKind(t string) string {
	switch t {
	case "svc":
		return tsKindService
	case "ingress":
		return tsKindIngress
	}

	return ""
}

// tsLayers numbers the columns in front of netmap's Service: the ProxyGroup only when the
// object is on one, the Ingress only for an Ingress.
type tsLayers struct{ group, proxy, ingress, front int }

func tsLayersOf(onGroup, ingress bool, first int) tsLayers {
	l := tsLayers{group: -1, ingress: -1}
	next := first

	if onGroup {
		l.group, next = next, next+1
	}

	l.proxy, next = next, next+1

	if ingress {
		l.ingress, next = next, next+1
	}

	l.front = next

	return l
}

// tsIngressMap: tailnet name → ProxyGroup → proxies → (Ingress) → Service → pods → nodes.
func tsIngressMap(in tsNetInput, row tsExposed, group *tsProxyGroupObject, pods []netmap.Pod) netmap.Map {
	layers := tsLayersOf(row.ProxyGroup != "", row.Kind == tsKindIngress, 1)
	g := netmap.NewGraph(layers.front)

	entry := g.Add(externalNode(tsEntryOf(row), 0))
	proxies := tsAddProxies(g, in, row, group, pods, layers, entry)

	// What the proxies send traffic to: the Ingress and the Services behind it, or the Service.
	var next string
	if row.Kind == tsKindIngress {
		var ing ingressObject
		_ = json.Unmarshal(in.object, &ing)

		next = g.Add(netmap.Node{
			ID: "ing/" + row.Namespace + "/" + row.Name, Layer: layers.ingress, Kind: tsKindIngress, Namespace: row.Namespace,
			Name: row.Name, Detail: strings.Join(row.TailnetNames, ", "), Managed: true,
		})
		for _, svc := range ingressBackends(ing) {
			g.Link(next, tsAddService(g, in, row.Namespace, svc))
		}
	} else {
		next = tsAddService(g, in, row.Namespace, row.Name)
		g.Node(next).Managed = true
	}

	for _, pid := range proxies {
		g.Link(pid, next)
	}

	return netmap.Finish(g, in.nodes)
}

// tsEgressMap: Service → ProxyGroup → proxies → tailnet target. The Service is the entry, the
// target the end: Ichor does not join the tailnet, the operator's condition is all it knows of
// it.
func tsEgressMap(in tsNetInput, row tsExposed, group *tsProxyGroupObject, pods []netmap.Pod) netmap.Map {
	layers := tsLayersOf(row.ProxyGroup != "", false, 1)
	g := netmap.NewGraph(0)

	svc := netmap.Node{ID: netmap.ServiceID(row.Namespace, row.Name), Kind: netmap.KindService, Namespace: row.Namespace, Name: row.Name, Managed: true}
	if i := slices.IndexFunc(in.services, func(s netmap.Service) bool { return s.Metadata.Name == row.Name }); i >= 0 {
		svc.Detail = netmap.ServiceDetail(in.services[i])
	}

	proxies := tsAddProxies(g, in, row, group, pods, layers, g.Add(svc))

	// The end of the map: its own health, the operator's condition, as nothing is behind it.
	entry := tsEntryOf(row)
	target := externalNode(entry, layers.front)
	target.Health = entry.Health

	tid := g.Add(target)
	for _, pid := range proxies {
		g.Link(pid, tid)
	}

	return netmap.Finish(g, in.nodes)
}

// tsAddProxies adds the ProxyGroup box (when the object is on one) after from, then a box per
// proxy pod, and a box per ProxyGroup device no pod could be paired with; it returns the proxy
// boxes' ids.
func tsAddProxies(g *netmap.Graph, in tsNetInput, row tsExposed, group *tsProxyGroupObject, proxies []netmap.Pod, layers tsLayers, from string) []string {
	var (
		devices   map[string]tsDeviceObject
		unmatched []tsDeviceObject
	)

	if row.ProxyGroup != "" {
		gid := g.Add(tsGroupNode(row.ProxyGroup, group, proxies, layers.group))
		g.Link(from, gid)
		from = gid

		if group != nil {
			devices, unmatched = tsMatchDevices(*group, proxies)
		}
	}

	var ids []string

	// "Running · ingress-0 · 100.64.0.31 · node-a/zone-1": the status first, as on a pod box.
	for _, p := range proxies {
		detail := []string{netmap.PodDetail(p.Pod)}
		if d, ok := devices[p.Name]; ok {
			detail = append(detail, d.Hostname)
			detail = append(detail, d.TailnetIPs...)
		}

		if where := tsWhere(p.Node, in.zones); where != "" {
			detail = append(detail, where)
		}

		pid := g.Add(netmap.Node{
			ID: "proxy/" + p.Namespace + "/" + p.Name, Layer: layers.proxy, Kind: netKindProxy, Namespace: p.Namespace, Name: p.Name,
			Detail: strings.Join(detail, " · "), Health: netmap.PodHealth(p.Pod),
		})
		g.Link(from, pid)
		ids = append(ids, pid)
	}

	// A device without a pod is shown, not guessed: it may be a pod this list missed.
	for _, d := range unmatched {
		did := g.Add(netmap.Node{
			ID: "device/" + d.Hostname, Layer: layers.proxy, Kind: netKindProxy, Name: d.Hostname,
			Detail: strings.Join(d.TailnetIPs, " · "), Health: healthIdle,
		})
		g.Link(from, did)
	}

	return ids
}

// tsGroupNode is the ProxyGroup box: its type and ready proxies; as good as its best proxy,
// critical with none, and a warning when two or more run and all on one node (one node down
// takes the address with it).
func tsGroupNode(name string, group *tsProxyGroupObject, proxies []netmap.Pod, layer int) netmap.Node {
	n := netmap.Node{ID: "pg/" + name, Layer: layer, Kind: netKindProxyGroup, Name: name, Health: healthCritical, Detail: "not found"}
	if group == nil {
		return n
	}

	row := tsProxyGroupRow(*group)
	ready, running, nodes := 0, 0, map[string]bool{}

	for _, p := range proxies {
		if p.Healthy {
			ready++
		}

		if h := netmap.PodHealth(p.Pod); tsBetter(h, n.Health) {
			n.Health = h
		}

		if p.Status == "Running" && p.Node != "" {
			running++
			nodes[p.Node] = true
		}
	}

	if n.Health == healthIdle {
		n.Health = healthOK
	}

	detail := []string{row.Type, fmt.Sprintf("%d/%d ready", ready, row.Replicas)}

	if running >= 2 && len(nodes) == 1 && n.Health == healthOK {
		n.Health = healthWarning

		for node := range nodes {
			detail = append(detail, "all on "+node)
		}
	}

	n.Detail = strings.Join(slices.DeleteFunc(detail, func(s string) bool { return s == "" }), " · ")

	return n
}

// tsBetter tells whether health a serves better than b: ok (or idle), then warning, then
// critical.
func tsBetter(a, b string) bool {
	rank := func(h string) int {
		switch h {
		case healthOK, healthIdle:
			return 2
		case healthWarning:
			return 1
		default:
			return 0
		}
	}

	return rank(a) > rank(b)
}

// tsMatchDevices pairs a ProxyGroup's proxy pods with its devices: pod <group>-<n> runs the
// device <hostnamePrefix>-<n> (the prefix defaults to the group's name), or one named like the
// pod. What does not pair is left unpaired on both sides.
func tsMatchDevices(group tsProxyGroupObject, pods []netmap.Pod) (map[string]tsDeviceObject, []tsDeviceObject) {
	prefix := group.Spec.HostnamePrefix
	if prefix == "" {
		prefix = group.Metadata.Name
	}

	byHost := map[string]tsDeviceObject{}
	for _, d := range group.Status.Devices {
		byHost[d.Hostname] = d
	}

	paired := map[string]tsDeviceObject{}

	for _, p := range pods {
		index, ok := strings.CutPrefix(p.Name, group.Metadata.Name+"-")
		if _, err := strconv.Atoi(index); !ok || err != nil {
			index = ""
		}

		for _, host := range []string{prefix + "-" + index, p.Name} {
			if d, found := byHost[host]; found && (index != "" || host == p.Name) {
				paired[p.Name] = d
				delete(byHost, host)

				break
			}
		}
	}

	var unmatched []tsDeviceObject
	for _, d := range group.Status.Devices {
		if _, left := byHost[d.Hostname]; left {
			unmatched = append(unmatched, d)
		}
	}

	return paired, unmatched
}

// tsWhere is "node-a/zone-1", the node alone without a zone.
func tsWhere(node string, zones map[string]string) string {
	if z := zones[node]; node != "" && z != "" {
		return node + "/" + z
	}

	return node
}

// tsAddService adds the Service namespace/name with its pods and nodes; one that is not there
// is a critical box.
func tsAddService(g *netmap.Graph, in tsNetInput, namespace, name string) string {
	i := slices.IndexFunc(in.services, func(s netmap.Service) bool { return s.Metadata.Name == name })
	if i < 0 {
		return g.Add(netmap.Node{
			ID: netmap.ServiceID(namespace, name), Layer: g.ServiceLayer(), Kind: netmap.KindService, Namespace: namespace, Name: name,
			Detail: "not found", Health: healthCritical,
		})
	}

	return netmap.AddBackend(g, in.services[i], in.pods)
}

// ingressBackends are the Services an Ingress sends traffic to, each once, in order.
func ingressBackends(ing ingressObject) []string {
	var out []string

	add := func(b *ingressBackend) {
		if b != nil && b.Service != nil && b.Service.Name != "" && !slices.Contains(out, b.Service.Name) {
			out = append(out, b.Service.Name)
		}
	}

	add(ing.Spec.DefaultBackend)

	for _, r := range ing.Spec.Rules {
		if r.HTTP == nil {
			continue
		}

		for i := range r.HTTP.Paths {
			add(&r.HTTP.Paths[i].Backend)
		}
	}

	return out
}

// tsRootCause is netmap's root cause with the tailnet name last: the deepest critical box,
// else the deepest warning one, among the cluster's boxes first; the name (the operator's
// condition) only when nothing in the cluster explains it.
func tsRootCause(m netmap.Map) *netmap.Problem {
	for _, external := range []bool{false, true} {
		for _, level := range []string{healthCritical, healthWarning} {
			var found *netmap.Node

			for i := range m.Nodes {
				n := &m.Nodes[i]
				if n.Health == level && (n.Kind == netKindExternalName) == external && (found == nil || n.Layer > found.Layer) {
					found = n
				}
			}

			if found != nil {
				return &netmap.Problem{Kind: found.Kind, Namespace: found.Namespace, Name: found.Name, Detail: found.Detail}
			}
		}
	}

	return nil
}
