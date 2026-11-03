package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// reportApp is an inventory app running images (one container each) on node.
func reportApp(id, node string, images ...string) inventoryApp {
	pod := inventoryPod{Namespace: "apps", Pod: id + "-0", Node: node}
	app := inventoryApp{ID: id, Name: id, Namespaces: []string{"apps"}}

	for i, image := range images {
		pod.Containers = append(pod.Containers, inventoryContainer{Name: id + string(rune('a'+i)), Image: image})
		ref := parseImageRef(image)
		app.Images = append(app.Images, inventoryImage{Repo: ref.Repo(), Tag: ref.Tag, Digest: ref.Digest, Containers: 1})
	}

	app.Pods = []inventoryPod{pod}

	return app
}

func TestImageReportUnpinned(t *testing.T) {
	inv := inventory{Apps: []inventoryApp{
		reportApp("untagged", "n1", "ghcr.io/example-org/tool"),
		reportApp("latest", "n2", "ghcr.io/example-org/web:latest"),
		reportApp("digest", "n1", "ghcr.io/example-org/api@sha256:0123456789abcdef0123"),
		reportApp("latest-digest", "n1", "ghcr.io/example-org/job:latest@sha256:0123456789abcdef0123"),
		reportApp("pinned", "n1", "ghcr.io/example-org/db:16.4"),
	}}

	got := buildImageReport(inv, nil)

	var images []string
	for _, u := range got.Unpinned {
		images = append(images, u.Image)
	}

	want := []string{"ghcr.io/example-org/tool", "ghcr.io/example-org/web:latest"}
	if !slices.Equal(images, want) {
		t.Fatalf("unpinned = %v, want %v (a digest pins the image)", images, want)
	}

	if u := got.Unpinned[1]; !slices.Equal(u.Nodes, []string{"n2"}) || len(u.Apps) != 1 || u.Apps[0].ID != "latest" || u.Apps[0].Namespace != "apps" {
		t.Errorf("latest row = %+v, want app latest in apps on n2", u)
	}

	if got.Summary.Unpinned != 2 || got.Summary.Images != 5 {
		t.Errorf("summary = %+v, want 2 unpinned of 5 images", got.Summary)
	}
}

func TestImageReportMultiTag(t *testing.T) {
	inv := inventory{Apps: []inventoryApp{
		reportApp("cilium", "n1", "quay.io/cilium/cilium:v1.18.2"),
		reportApp("cilium-old", "n2", "quay.io/cilium/cilium:v1.18.1"),
		reportApp("one", "n1", "ghcr.io/example-org/one:1.0"),
		reportApp("one-again", "n2", "ghcr.io/example-org/one:1.0"),
	}}

	got := buildImageReport(inv, nil).MultiTag
	if len(got) != 1 || got[0].Repository != "quay.io/cilium/cilium" {
		t.Fatalf("multiTag = %+v, want only quay.io/cilium/cilium", got)
	}

	tags := got[0].Tags
	if len(tags) != 2 || tags[0].Tag != "v1.18.1" || !slices.Equal(tags[0].Nodes, []string{"n2"}) || tags[1].Apps[0].ID != "cilium" {
		t.Errorf("tags = %+v, want v1.18.1 on n2 and v1.18.2 of cilium", tags)
	}
}

func TestImageReportRegistriesFoldDockerHub(t *testing.T) {
	inv := inventory{Apps: []inventoryApp{
		reportApp("a", "n1", "docker.io/library/nginx:1.27"),
		reportApp("b", "n1", "nginx:1.27"),
		reportApp("c", "n1", "index.docker.io/library/nginx:1.27"),
		reportApp("d", "n1", "library/redis:8.2"),
		reportApp("e", "n1", "ghcr.io/example-org/web:2.0"),
		reportApp("f", "n1", "registry.example.net:5000/team/tool:1"),
	}}

	got := buildImageReport(inv, nil)

	want := []reportRegistry{
		{Host: "docker.io", Images: 2, Apps: 4, DockerHub: true},
		{Host: "ghcr.io", Images: 1, Apps: 1},
		{Host: "registry.example.net:5000", Images: 1, Apps: 1},
	}
	if !slices.Equal(got.Registries, want) {
		t.Errorf("registries = %+v, want %+v", got.Registries, want)
	}

	if got.Summary.DockerHub != 2 {
		t.Errorf("dockerHub = %d, want 2 (nginx under three names, and redis)", got.Summary.DockerHub)
	}

	if len(got.MultiTag) != 0 {
		t.Errorf("multiTag = %+v, want none: the three nginx names are one image", got.MultiTag)
	}
}

func TestImageReportUnusedOnNode(t *testing.T) {
	inv := inventory{Apps: []inventoryApp{
		reportApp("web", "n1", "nginx:1.27"),
		reportApp("api", "n1", "ghcr.io/example-org/api@sha256:aaa"),
		reportApp("tool", "n2", "ghcr.io/example-org/tool"),
	}}
	nodeImages := map[string][]imageInfo{
		"n1": {
			{Name: "docker.io/library/nginx:1.27", Digest: "sha256:img1", Size: 10},
			{Name: "ghcr.io/example-org/api@sha256:aaa", Digest: "sha256:img2", Size: 20},
			{Name: "ghcr.io/example-org/old@sha256:bbb", Digest: "sha256:img3", Size: 30},
			{Name: "ghcr.io/example-org/old:0.9", Digest: "sha256:img3", Size: 30},
			{Name: "registry.k8s.io/pause:3.10", Digest: "sha256:pause", Size: 1},
		},
		"n2": {
			{Name: "docker.io/library/nginx:1.27", Digest: "sha256:img1", Size: 10},
			{Name: "ghcr.io/example-org/tool:latest", Digest: "sha256:img4", Size: 40},
		},
	}

	got := buildImageReport(inv, nodeImages).UnusedOnNode

	want := []reportUnused{
		{Node: "n1", Image: "ghcr.io/example-org/old:0.9", SizeBytes: 30},
		{Node: "n2", Image: "docker.io/library/nginx:1.27", SizeBytes: 10},
	}
	if !slices.Equal(got, want) {
		t.Errorf("unusedOnNode = %+v, want %+v", got, want)
	}
}

func TestImageReportReadsWhatTheFacadeReturns(t *testing.T) {
	inv, err := inventoryJSON(demoInventory(contractNow.UnixMilli()))
	if err != nil {
		t.Fatal(err)
	}

	nodes, err := toJSON(map[string][]imageInfo{"n1": demoNodeImages()})
	if err != nil {
		t.Fatal(err)
	}

	out, err := ImageReportWithNodeImages(inv, nodes)
	if err != nil {
		t.Fatal(err)
	}

	var got imageReport
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	// n1 runs no container of the demo: both of its images are unused.
	if got.Summary.Unused != 2 || got.Summary.Unpinned == 0 {
		t.Errorf("summary = %+v, want 2 unused and the demo's unpinned images", got.Summary)
	}

	if out, err := ImageReport(inv); err != nil || !strings.Contains(out, `"unusedOnNode":[]`) {
		t.Errorf("ImageReport = %s, %v; want no unused images without node images", out, err)
	}

	if _, err := ImageReport("not json"); err == nil {
		t.Error("ImageReport(not json) = nil error")
	}
}

func TestDemoImageReport(t *testing.T) {
	got := demoImageReport(contractNow.UnixMilli())

	if len(got.Unpinned) != 2 {
		t.Errorf("demo unpinned = %+v, want two images", got.Unpinned)
	}

	if len(got.MultiTag) != 1 || got.MultiTag[0].Repository != "quay.io/cilium/cilium" {
		t.Errorf("demo multiTag = %+v, want cilium", got.MultiTag)
	}

	if got.Summary.Unused == 0 || got.Summary.DockerHub == 0 {
		t.Errorf("demo summary = %+v, want unused and Docker Hub images", got.Summary)
	}
}
