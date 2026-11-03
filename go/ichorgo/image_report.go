package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The images report (D8): from an inventory the apps already hold, which images are not
// pinned, which repositories run several tags at once, where the images come from, and which
// images sit on a Talos node's disk with no container using them. Pure: no cluster call.

type imageReport struct {
	Unpinned     []reportUnpinned `json:"unpinned"`
	MultiTag     []reportRepo     `json:"multiTag"`
	Registries   []reportRegistry `json:"registries"`
	UnusedOnNode []reportUnused   `json:"unusedOnNode"`
	Summary      reportSummary    `json:"summary"`
}

// reportAppRef is an app of the inventory (its id opens its sheet), with a namespace it runs in.
type reportAppRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// reportUnpinned is an image run as :latest or with no tag, and no digest.
type reportUnpinned struct {
	Image string         `json:"image"`
	Apps  []reportAppRef `json:"apps"`
	Nodes []string       `json:"nodes"`
}

// reportRepo is a repository running with several tags (or digests) at once.
type reportRepo struct {
	Repository string      `json:"repository"`
	Tags       []reportTag `json:"tags"`
}

type reportTag struct {
	Tag   string         `json:"tag"`
	Nodes []string       `json:"nodes"`
	Apps  []reportAppRef `json:"apps"`
}

// reportRegistry counts the distinct images and the apps pulling from one registry host.
type reportRegistry struct {
	Host      string `json:"host"`
	Images    int    `json:"images"`
	Apps      int    `json:"apps"`
	DockerHub bool   `json:"dockerHub"`
}

// reportUnused is an image in a node's CRI store that no container of the node runs.
type reportUnused struct {
	Node      string `json:"node"`
	Image     string `json:"image"`
	SizeBytes int64  `json:"sizeBytes"`
}

type reportSummary struct {
	Images    int `json:"images"`
	Unpinned  int `json:"unpinned"`
	MultiTag  int `json:"multiTag"`
	DockerHub int `json:"dockerHub"` // distinct images pulled from Docker Hub (rate limits)
	Unused    int `json:"unused"`
}

// ImageReport is the images report of inventoryJSON, what ClusterInventory or KubeInventory
// returned (the apps pass back what they hold). No node images: unusedOnNode stays empty.
func ImageReport(inventoryJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	return ImageReportWithNodeImages(inventoryJSON, "")
}

// ImageReportWithNodeImages is ImageReport for a Talos cluster: nodeImagesJSON maps each node
// to what NodeImages returned for it ({"node": [...]}), for the images no container uses; ""
// or "{}" leaves them out.
func ImageReportWithNodeImages(inventoryJSON, nodeImagesJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var inv inventory
	if err := json.Unmarshal([]byte(inventoryJSON), &inv); err != nil {
		return "", fmt.Errorf("read the inventory: %w", err)
	}

	nodeImages := map[string][]imageInfo{}
	if strings.TrimSpace(nodeImagesJSON) != "" {
		if err := json.Unmarshal([]byte(nodeImagesJSON), &nodeImages); err != nil {
			return "", fmt.Errorf("read the node images: %w", err)
		}
	}

	return toJSON(buildImageReport(inv, nodeImages))
}

// reportKey is one image: a canonical repository with its tag and digest, or only a digest.
type reportKey struct{ repo, tag, digest string }

type reportUse struct {
	apps  map[string]reportAppRef
	nodes map[string]bool
}

// Docker Hub's other names; parseImageRef already turns a bare name into docker.io/library/….
var dockerHubAliases = map[string]bool{"index.docker.io": true, "registry-1.docker.io": true, "registry.hub.docker.com": true}

// reportKeyOf reads a container's image reference.
func reportKeyOf(image string) reportKey {
	ref := parseImageRef(image)
	if ref.anonymous() {
		return reportKey{digest: image}
	}

	if dockerHubAliases[ref.Registry] {
		ref.Registry = defaultRegistry
	}

	return reportKey{repo: ref.Repo(), tag: ref.Tag, digest: ref.Digest}
}

// inventoryKey is reportKeyOf for an image the inventory already split.
func inventoryKey(img inventoryImage) reportKey {
	if img.Repo == "" {
		return reportKey{digest: img.Digest}
	}

	key := reportKeyOf(img.Repo)

	return reportKey{repo: key.repo, tag: img.Tag, digest: img.Digest}
}

func (k reportKey) unpinned() bool {
	return k.repo != "" && k.digest == "" && (k.tag == "" || k.tag == "latest")
}

func (k reportKey) host() string { return k.repo[:strings.Index(k.repo, "/")] }

// label names the version: the tag, else the short digest; no tag and no digest is latest.
func (k reportKey) label() string {
	switch {
	case k.tag != "":
		return k.tag
	case k.digest != "":
		return "@" + shortImageDigest(k.digest)
	default:
		return "latest"
	}
}

func (k reportKey) display() string {
	if k.repo == "" {
		return k.digest
	}

	if k.tag == "" {
		return k.repo
	}

	return k.repo + ":" + k.tag
}

// shortImageDigest is "sha256:" and the first 12 hex digits, like docker.
func shortImageDigest(digest string) string {
	algo, hex, ok := strings.Cut(digest, ":")
	if !ok || len(hex) <= 12 {
		return digest
	}

	return algo + ":" + hex[:12]
}

func buildImageReport(inv inventory, nodeImages map[string][]imageInfo) imageReport {
	uses := collectImageUses(inv)

	report := imageReport{
		Unpinned:     reportUnpinnedImages(uses),
		MultiTag:     reportMultiTag(uses),
		Registries:   reportRegistries(uses),
		UnusedOnNode: reportUnusedImages(inv, nodeImages),
	}

	report.Summary = reportSummary{
		Images: len(uses), Unpinned: len(report.Unpinned), MultiTag: len(report.MultiTag), Unused: len(report.UnusedOnNode),
	}

	for _, r := range report.Registries {
		if r.DockerHub {
			report.Summary.DockerHub += r.Images
		}
	}

	return report
}

// collectImageUses maps every image to the apps running it and the nodes it runs on.
func collectImageUses(inv inventory) map[reportKey]*reportUse {
	uses := map[reportKey]*reportUse{}

	use := func(key reportKey, app inventoryApp, namespace, node string) {
		u := uses[key]
		if u == nil {
			u = &reportUse{apps: map[string]reportAppRef{}, nodes: map[string]bool{}}
			uses[key] = u
		}

		if _, ok := u.apps[app.ID]; !ok {
			u.apps[app.ID] = reportAppRef{ID: app.ID, Name: app.Name, Namespace: namespace}
		}

		if node != "" {
			u.nodes[node] = true
		}
	}

	for _, app := range inv.Apps {
		for _, pod := range app.Pods {
			for _, c := range pod.Containers {
				use(reportKeyOf(c.Image), app, pod.Namespace, pod.Node)
			}
		}

		// The images of workloads with no pod (stopped or not starting) count without a node.
		namespace := ""
		if len(app.Namespaces) > 0 {
			namespace = app.Namespaces[0]
		}

		for _, img := range app.Images {
			use(inventoryKey(img), app, namespace, "")
		}
	}

	return uses
}

func reportUnpinnedImages(uses map[reportKey]*reportUse) []reportUnpinned {
	byImage := map[string]*reportUse{}

	for key, u := range uses {
		if key.unpinned() {
			byImage[key.display()] = mergeUse(byImage[key.display()], u)
		}
	}

	out := []reportUnpinned{}
	for _, image := range slices.Sorted(maps.Keys(byImage)) {
		out = append(out, reportUnpinned{Image: image, Apps: sortedApps(byImage[image]), Nodes: sortedNodes(byImage[image])})
	}

	return out
}

func reportMultiTag(uses map[reportKey]*reportUse) []reportRepo {
	repos := map[string]map[string]*reportUse{}

	for key, u := range uses {
		if key.repo == "" {
			continue
		}

		if repos[key.repo] == nil {
			repos[key.repo] = map[string]*reportUse{}
		}

		repos[key.repo][key.label()] = mergeUse(repos[key.repo][key.label()], u)
	}

	out := []reportRepo{}

	for _, repo := range slices.Sorted(maps.Keys(repos)) {
		tags := repos[repo]
		if len(tags) < 2 {
			continue
		}

		entry := reportRepo{Repository: repo}
		for _, tag := range slices.Sorted(maps.Keys(tags)) {
			entry.Tags = append(entry.Tags, reportTag{Tag: tag, Nodes: sortedNodes(tags[tag]), Apps: sortedApps(tags[tag])})
		}

		out = append(out, entry)
	}

	return out
}

func reportRegistries(uses map[reportKey]*reportUse) []reportRegistry {
	images := map[string]int{}
	apps := map[string]map[string]bool{}

	for key, u := range uses {
		if key.repo == "" {
			continue
		}

		host := key.host()
		images[host]++

		if apps[host] == nil {
			apps[host] = map[string]bool{}
		}

		for id := range u.apps {
			apps[host][id] = true
		}
	}

	out := []reportRegistry{}
	for host, n := range images {
		out = append(out, reportRegistry{Host: host, Images: n, Apps: len(apps[host]), DockerHub: host == defaultRegistry})
	}

	slices.SortFunc(out, func(a, b reportRegistry) int {
		return cmp.Or(cmp.Compare(b.Images, a.Images), cmp.Compare(a.Host, b.Host))
	})

	return out
}

func mergeUse(into, from *reportUse) *reportUse {
	if into == nil {
		into = &reportUse{apps: map[string]reportAppRef{}, nodes: map[string]bool{}}
	}

	for id, app := range from.apps {
		if _, ok := into.apps[id]; !ok {
			into.apps[id] = app
		}
	}

	for node := range from.nodes {
		into.nodes[node] = true
	}

	return into
}

func sortedApps(u *reportUse) []reportAppRef {
	apps := slices.Collect(maps.Values(u.apps))
	slices.SortFunc(apps, func(a, b reportAppRef) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })

	return apps
}

func sortedNodes(u *reportUse) []string { return orEmpty(slices.Sorted(maps.Keys(u.nodes))) }
