package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// Listing containers is heavier than a stats sample; a node slower than this is left out.
const (
	inventoryTimeout     = 15 * time.Second
	inventoryNodeTimeout = 10 * time.Second
)

// inventory is the software running in the cluster, grouped into applications.
type inventory struct {
	At       int64          `json:"at"`       // unix ms
	Nodes    int            `json:"nodes"`    // nodes of the context
	Answered int            `json:"answered"` // nodes whose containers are counted
	Apps     []inventoryApp `json:"apps"`
}

type inventoryApp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"` // a catalog category, or "other"
	// Icon names the bundled icon (appicons/<icon>.webp); RemoteIcon an upstream dashboard-icons
	// slug the app may fetch when the user allowed it. Neither: the app draws a monogram.
	Icon       string           `json:"icon,omitempty"`
	RemoteIcon string           `json:"remoteIcon,omitempty"`
	Known      bool             `json:"known"`  // identified by the catalog or an icon slug
	System     bool             `json:"system"` // Kubernetes and Talos plumbing
	Version    string           `json:"version"`
	Drift      bool             `json:"drift"`    // one repository runs with several tags
	Unpinned   bool             `json:"unpinned"` // :latest or untagged
	Namespaces []string         `json:"namespaces"`
	Nodes      []string         `json:"nodes"`
	Containers int              `json:"containers"`
	Running    int              `json:"running"`
	Memory     uint64           `json:"memory"` // bytes
	Images     []inventoryImage `json:"images"`
	Pods       []inventoryPod   `json:"pods"`
}

type inventoryImage struct {
	Repo       string `json:"repo"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest,omitempty"`
	Containers int    `json:"containers"`
}

type inventoryPod struct {
	Namespace  string               `json:"namespace"`
	Pod        string               `json:"pod"`
	Node       string               `json:"node"`
	Containers []inventoryContainer `json:"containers"`
}

type inventoryContainer struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	Memory uint64 `json:"memory"` // bytes
}

// nodeContainers are the Kubernetes containers one node reported.
type nodeContainers struct {
	Node       string
	Containers []containerInfo
}

// ClusterInventory lists the applications running in the cluster: every node's Kubernetes
// containers, identified from their image (and pod, and namespace) against the bundled catalog
// (os:reader). Nodes that do not answer in time are left out rather than failing the inventory.
func ClusterInventory(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ClusterInventory", configYAML, contextName, "")
	}

	return withSession(configYAML, contextName, inventoryTimeout, func(ctx context.Context, s *session) (string, error) {
		nodes := targetNodes(s.context)
		lists := make([]*nodeContainers, len(nodes))

		forEachNode(nodes, func(i int, node string) { lists[i] = listNodeContainers(ctx, s.client, node) })

		answered := make([]nodeContainers, 0, len(lists))

		for _, list := range lists {
			if list != nil {
				answered = append(answered, *list)
			}
		}

		return inventoryJSON(buildInventory(time.Now().UnixMilli(), len(nodes), answered))
	})
}

// inventoryJSON encodes the inventory. In screenshot mode it drops every logo, so store and website
// screenshots never show third-party trademarks: each app falls back to its monogram, names stay.
func inventoryJSON(inv inventory) (string, error) {
	if privacy.isEnabled() {
		apps := make([]inventoryApp, len(inv.Apps))
		for i, app := range inv.Apps {
			app.Icon, app.RemoteIcon = "", ""
			apps[i] = app
		}

		inv.Apps = apps
	}

	return toJSON(inv)
}

// listNodeContainers returns nil when the node did not list its containers.
func listNodeContainers(ctx context.Context, c *client.Client, node string) *nodeContainers {
	ctx, cancel := context.WithTimeout(ctx, inventoryNodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)

	list, err := c.Containers(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI)
	if err != nil {
		return nil
	}

	// Best effort: memory makes the inventory nicer, but the apps are worth listing without it.
	stats, _ := c.Stats(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI) //nolint:errcheck

	return &nodeContainers{
		Node:       node,
		Containers: mergeContainers(first(list.GetMessages()).GetContainers(), first(stats.GetMessages()).GetStats()),
	}
}

// placedContainer is one container with where it runs and what it was identified as.
type placedContainer struct {
	node    string
	info    containerInfo
	ref     imageRef
	id      identification
	adopted bool // identified by its pod, not by its own image
}

func buildInventory(at int64, total int, nodes []nodeContainers) inventory {
	catalog := loadAppCatalog()
	placed := identifyContainers(catalog, nodes)
	keys := groupKeys(placed)

	groups := map[string]*appBuilder{}

	var order []string

	// An app takes its identity from a container that defines it, not from one that joined it.
	for i, p := range placed {
		if app := appIdentity(p); app.ID == keys[i] && groups[app.ID] == nil {
			groups[app.ID] = newAppBuilder(app)
			order = append(order, app.ID)
		}
	}

	for i, p := range placed {
		groups[keys[i]].add(p)
	}

	apps := make([]inventoryApp, 0, len(order))
	for _, key := range order {
		apps = append(apps, groups[key].build())
	}

	slices.SortFunc(apps, func(a, b inventoryApp) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})

	return inventory{At: at, Nodes: total, Answered: len(nodes), Apps: apps}
}

// groupKeys is each container's app id. A workload named only by its pod (a CronJob of base images
// next to the app) joins the image-named app of the same name in its namespace.
func groupKeys(placed []placedContainer) []string {
	keys := make([]string, len(placed))
	named := map[string]string{} // namespace + name -> app id

	for i, p := range placed {
		app := appIdentity(p)
		keys[i] = app.ID

		if !strings.HasPrefix(app.ID, podKeyPrefix) {
			named[p.info.PodNamespace+"\x00"+strings.ToLower(app.Name)] = app.ID
		}
	}

	for i, p := range placed {
		if strings.HasPrefix(keys[i], podKeyPrefix) {
			if id, ok := named[p.info.PodNamespace+"\x00"+strings.ToLower(appIdentity(p).Name)]; ok {
				keys[i] = id
			}
		}
	}

	return keys
}

// identifyContainers names each container's app from its image. A helper or unknown container then
// joins the main app of its pod (grafana's k8s-sidecar, a CSI driver's registrar); a pod whose images
// said nothing (pinned by digest, base images) is named from its workload name or namespace.
func identifyContainers(catalog *appCatalog, nodes []nodeContainers) []placedContainer {
	var placed []placedContainer

	pods := map[string][]int{}

	for _, n := range nodes {
		for _, c := range n.Containers {
			ref := parseImageRef(c.Image)
			key := c.PodNamespace + "/" + c.Pod
			pods[key] = append(pods[key], len(placed))
			placed = append(placed, placedContainer{node: n.Node, info: c, ref: ref, id: catalog.identify(ref)})
		}
	}

	for _, members := range pods {
		main, sibling := podMain(catalog, placed, members)
		if main == (identification{}) {
			continue
		}

		for _, i := range members {
			if !isMain(placed[i].id) {
				// Named by its pod rather than by a sibling, a container's own image still versions the app.
				placed[i].id, placed[i].adopted = main, sibling
			}
		}
	}

	return placed
}

// podMain is the app a pod runs: its first identified container's (sibling), else its name's or
// namespace's.
func podMain(catalog *appCatalog, placed []placedContainer, members []int) (id identification, sibling bool) {
	if k := slices.IndexFunc(members, func(i int) bool { return isMain(placed[i].id) }); k >= 0 {
		return placed[members[k]].id, true
	}

	info := placed[members[0]].info

	return catalog.byPod(info.PodNamespace, info.Pod), false
}

func isMain(id identification) bool {
	return (id.app != nil && !id.app.Generic) || id.slug != ""
}

const podKeyPrefix = "pod:"

// CRI container states, as Talos reports them.
const (
	containerRunning = "CONTAINER_RUNNING"
	containerExited  = "CONTAINER_EXITED"
)

// namesNothing reports an image that says nothing about its app: a bare digest, or a base image.
func (p placedContainer) namesNothing() bool { return p.ref.anonymous() || baseImages[p.ref.Name()] }

// appIdentity is the app a container belongs to, before any count: a catalog app, an app named by
// an icon slug, else an unknown app per image repository, or per workload when the image names nothing.
func appIdentity(p placedContainer) inventoryApp {
	switch {
	case p.id.app != nil:
		app := inventoryApp{ID: p.id.app.ID, Name: p.id.app.Name, Category: p.id.app.Category, Known: true, System: p.id.app.System}
		if p.id.app.hasIcon() {
			app.Icon = p.id.app.ID
		}

		return app
	case p.id.slug != "":
		return inventoryApp{ID: p.id.slug, Name: displayName(p.id.slug), Category: "other", Known: true, RemoteIcon: p.id.slug}
	case p.namesNothing():
		base := podBaseName(p.info.Pod)

		return inventoryApp{ID: podKeyPrefix + p.info.PodNamespace + "/" + base, Name: displayName(base), Category: "other"}
	default:
		return inventoryApp{ID: "image:" + p.ref.Repo(), Name: unknownName(p.ref), Category: unknownCategory(p.ref)}
	}
}

// unknownCategory files Prometheus exporters (smartctl_exporter, domain-exporter) under observability.
func unknownCategory(ref imageRef) string {
	if name := ref.Name(); strings.HasSuffix(name, "-exporter") || strings.HasSuffix(name, "_exporter") {
		return "observability"
	}

	return "other"
}

type appBuilder struct {
	app        inventoryApp
	namespaces map[string]bool
	nodes      map[string]bool
	images     map[string]*inventoryImage
	primary    map[string]bool // images that identified the app themselves; the version comes from them
	live       map[string]bool // images with a container that has not exited
	pods       map[string]*inventoryPod
}

func newAppBuilder(app inventoryApp) *appBuilder {
	return &appBuilder{
		app:        app,
		namespaces: map[string]bool{}, nodes: map[string]bool{},
		images: map[string]*inventoryImage{}, primary: map[string]bool{}, live: map[string]bool{}, pods: map[string]*inventoryPod{},
	}
}

func (b *appBuilder) add(p placedContainer) {
	b.app.Containers++
	b.app.Memory += p.info.Memory

	if p.info.Status == containerRunning {
		b.app.Running++
	}

	if p.info.PodNamespace != "" {
		b.namespaces[p.info.PodNamespace] = true
	}

	b.nodes[p.node] = true

	image := inventoryImage{Repo: p.ref.Repo(), Tag: p.ref.Tag, Digest: p.ref.Digest}
	if p.ref.anonymous() {
		image = inventoryImage{Digest: p.info.Image}
	}

	imageKey := image.Repo + ":" + image.Tag + "@" + image.Digest
	if b.images[imageKey] == nil {
		b.images[imageKey] = &image
	}

	b.images[imageKey].Containers++

	if !p.adopted && !p.namesNothing() {
		b.primary[imageKey] = true
	}

	if p.info.Status != containerExited {
		b.live[imageKey] = true
	}

	podKey := p.info.PodNamespace + "/" + p.info.Pod
	if b.pods[podKey] == nil {
		b.pods[podKey] = &inventoryPod{Namespace: p.info.PodNamespace, Pod: p.info.Pod, Node: p.node}
	}

	b.pods[podKey].Containers = append(b.pods[podKey].Containers, inventoryContainer{
		Name: p.info.Name, Image: p.info.Image, Status: p.info.Status, Memory: p.info.Memory,
	})
}

func (b *appBuilder) build() inventoryApp {
	app := b.app
	app.Namespaces = sortedKeys(b.namespaces)
	app.Nodes = sortedKeys(b.nodes)

	keys := make([]string, 0, len(b.images))
	for key := range b.images {
		keys = append(keys, key)
	}

	// The most used image that identified the app first: its tag is the app's version.
	slices.SortFunc(keys, func(x, y string) int {
		ix, iy := b.images[x], b.images[y]

		return cmp.Or(compareBool(b.primary[y], b.primary[x]), compareBool(b.live[y], b.live[x]), cmp.Compare(iy.Containers, ix.Containers), cmp.Compare(x, y))
	})

	for _, key := range keys {
		app.Images = append(app.Images, *b.images[key])
	}

	// Only the app's own images version it and raise flags: a busybox:latest init container does not,
	// nor the old tag of finished CronJob runs, unless nothing of the app runs any more.
	own := b.ownImages(keys, app.Images, true)
	if len(own) == 0 {
		own = b.ownImages(keys, app.Images, false)
	}

	if len(own) > 0 {
		app.Version = own[0].Tag
	}

	app.Drift, app.Unpinned = imageFlags(own)

	for _, pod := range b.pods {
		app.Pods = append(app.Pods, *pod)
	}

	slices.SortFunc(app.Pods, func(x, y inventoryPod) int {
		return cmp.Or(cmp.Compare(x.Namespace, y.Namespace), cmp.Compare(x.Pod, y.Pod))
	})

	return app
}

// ownImages are the images that identified the app themselves, only those of live containers if asked.
func (b *appBuilder) ownImages(keys []string, images []inventoryImage, liveOnly bool) []inventoryImage {
	var own []inventoryImage

	for i, key := range keys {
		if b.primary[key] && (!liveOnly || b.live[key]) {
			own = append(own, images[i])
		}
	}

	return own
}

// imageFlags reports a repository running with several tags, and an image without a pinned version.
func imageFlags(images []inventoryImage) (drift, unpinned bool) {
	tags := map[string]string{}

	for _, img := range images {
		if seen, ok := tags[img.Repo]; ok && seen != img.Tag {
			drift = true
		}

		tags[img.Repo] = img.Tag

		if img.Digest == "" && (img.Tag == "" || img.Tag == "latest") {
			unpinned = true
		}
	}

	return drift, unpinned
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
