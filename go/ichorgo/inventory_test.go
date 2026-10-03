package ichorgo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		in   string
		want imageRef
	}{
		{"nginx", imageRef{Registry: "docker.io", Path: "library/nginx"}},
		{"nginx:1.27", imageRef{Registry: "docker.io", Path: "library/nginx", Tag: "1.27"}},
		{"grafana/grafana:12.2.0", imageRef{Registry: "docker.io", Path: "grafana/grafana", Tag: "12.2.0"}},
		{"quay.io/cilium/cilium:v1.18.2@sha256:abc", imageRef{Registry: "quay.io", Path: "cilium/cilium", Tag: "v1.18.2", Digest: "sha256:abc"}},
		{"registry.k8s.io/coredns/coredns:v1.12.0", imageRef{Registry: "registry.k8s.io", Path: "coredns/coredns", Tag: "v1.12.0"}},
		{"localhost:5000/app:dev", imageRef{Registry: "localhost:5000", Path: "app", Tag: "dev"}},
		{"ghcr.io/org/app@sha256:def", imageRef{Registry: "ghcr.io", Path: "org/app", Digest: "sha256:def"}},
	}

	for _, tt := range tests {
		if got := parseImageRef(tt.in); got != tt.want {
			t.Errorf("parseImageRef(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestIdentify(t *testing.T) {
	tests := []struct {
		image string
		app   string // catalog id, "slug:<slug>" for an upstream icon only, "" for unknown
	}{
		{"registry.k8s.io/kube-apiserver:v1.34.1", "kubernetes"},
		{"registry.k8s.io/coredns/coredns:v1.12.0", "coredns"},
		{"ghcr.io/siderolabs/flannel:v0.27.4", "flannel"},
		{"ghcr.io/siderolabs/talos-cloud-controller-manager:v1.10", "talos"},
		{"quay.io/cilium/operator-generic:v1.18.2", "cilium"},
		{"quay.io/cilium/hubble-relay:v1.18.2", "cilium"},
		{"quay.io/jetstack/cert-manager-cainjector:v1.18.2", "cert-manager"},
		{"docker.io/longhornio/longhorn-instance-manager:v1.9.1", "longhorn"},
		{"ghcr.io/home-assistant/home-assistant:2025.10", "home-assistant"},
		{"ghcr.io/home-operations/radarr:5.27", "radarr"},
		{"lscr.io/linuxserver/sonarr:latest", "sonarr"},
		{"ghcr.io/immich-app/immich-machine-learning:v2.0.1", "immich"},
		{"vaultwarden/server:1.34.3", "vaultwarden"},
		{"docker.io/bitnami/postgresql:16", "postgresql"},
		{"postgres:17-alpine", "postgresql"},
		{"quay.io/prometheus/prometheus:v3.6.0", "prometheus"},
		{"quay.io/prometheus/alertmanager:v0.28.1", "alertmanager"},
		{"quay.io/prometheus-operator/prometheus-config-reloader:v0.86.0", "prometheus-operator"},
		{"docker.io/grafana/loki:3.5.5", "loki"},
		{"ghcr.io/fluxcd/kustomize-controller:v1.7.0", "flux"},
		{"quay.io/argoproj/argocd:v3.1.0", "argo-cd"},
		{"registry.k8s.io/sig-storage/csi-provisioner:v5.3.0", "kubernetes-csi"},
		{"traefik:v3.5", "traefik"},
		{"registry.k8s.io/ingress-nginx/controller:v1.13.3", "ingress-nginx"},
		{"docker.io/louislam/uptime-kuma:2", "uptime-kuma"},
		{"ghcr.io/example/audiobookshelf:2.29", "audiobookshelf"},
		{"ghcr.io/someone/mealie-server:3", "mealie"},
		{"ghcr.io/example/stirling-pdf:1", "stirling-pdf"},
		{"ghcr.io/example/actual-server:25", "actual-budget"},
		{"ghcr.io/gethomepage/homepage:v1.5", "homepage"},
		{"docker.io/library/registry:2", "docker-registry"},
		{"ghcr.io/example/kimai:2", "slug:kimai"}, // not curated, but dashboard-icons has an icon
		{"registry.example.com/internal/nightly-backup:1", ""},
		{"sha256:0123456789abcdef", ""},
	}

	catalog := loadAppCatalog()

	for _, tt := range tests {
		id := catalog.identify(parseImageRef(tt.image))

		got := ""
		if id.app != nil {
			got = id.app.ID
		} else if id.slug != "" {
			got = "slug:" + id.slug
		}

		if got != tt.app {
			t.Errorf("identify(%q) = %q, want %q", tt.image, got, tt.app)
		}
	}
}

func TestHelperCandidates(t *testing.T) {
	for in, want := range map[string][]string{
		"longhorn-instance-manager": {"longhorn"},
		"cert-manager-controller":   {"cert-manager", "cert"},
		"immich-machine-learning":   {"immich"},
		"server":                    nil,
		"grafana":                   nil,
	} {
		if got := helperCandidates(in); !slices.Equal(got, want) {
			t.Errorf("helperCandidates(%q) = %q, want %q", in, got, want)
		}
	}
}

func appByID(t *testing.T, inv inventory, id string) inventoryApp {
	t.Helper()

	for _, app := range inv.Apps {
		if app.ID == id {
			return app
		}
	}

	t.Fatalf("no app %q in %+v", id, inv.Apps)

	return inventoryApp{}
}

func TestBuildInventoryGroupsAndFlags(t *testing.T) {
	running := "CONTAINER_RUNNING"
	inv := buildInventory(1, 3, []nodeContainers{
		{Node: "10.0.0.1", Containers: []containerInfo{
			{PodNamespace: "kube-system", Pod: "cilium-a", Name: "agent", Image: "quay.io/cilium/cilium:v1.18.2", Status: running, Memory: 100},
			{PodNamespace: "monitoring", Pod: "grafana-0", Name: "grafana", Image: "grafana/grafana:12.2.0", Status: running, Memory: 50},
			{PodNamespace: "monitoring", Pod: "grafana-0", Name: "sidecar", Image: "quay.io/kiwigrid/k8s-sidecar:1.30", Status: running, Memory: 5},
			{PodNamespace: "longhorn-system", Pod: "csi-0", Name: "registrar", Image: "registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.14.0", Status: running},
			{PodNamespace: "longhorn-system", Pod: "csi-0", Name: "plugin", Image: "longhornio/longhorn-manager:v1.9.1", Status: running},
			{PodNamespace: "kube-system", Pod: "snapshot-controller-0", Name: "snapshotter", Image: "registry.k8s.io/sig-storage/snapshot-controller:v8", Status: running},
		}},
		{Node: "10.0.0.2", Containers: []containerInfo{
			{PodNamespace: "kube-system", Pod: "cilium-b", Name: "agent", Image: "quay.io/cilium/cilium:v1.18.1", Status: "CONTAINER_CREATED", Memory: 90},
			{PodNamespace: "default", Pod: "kuma-0", Name: "app", Image: "louislam/uptime-kuma", Status: running},
			{PodNamespace: "immich", Pod: "ml-0", Name: "ml", Image: "registry.example.com/custom/ml:1", Status: running},
			{PodNamespace: "tools", Pod: "job-0", Name: "job", Image: "registry.example.com/internal/nightly-backup:1", Status: running},
		}},
	})

	if inv.Nodes != 3 || inv.Answered != 2 {
		t.Errorf("nodes = %d/%d, want 2/3", inv.Answered, inv.Nodes)
	}

	cilium := appByID(t, inv, "cilium")
	if cilium.Containers != 2 || cilium.Running != 1 || cilium.Memory != 190 || !cilium.Drift || len(cilium.Nodes) != 2 {
		t.Errorf("cilium = %+v", cilium)
	}

	if cilium.Icon != "cilium" || cilium.Category != "networking" {
		t.Errorf("cilium icon/category = %q/%q", cilium.Icon, cilium.Category)
	}

	// The k8s-sidecar joins grafana's pod; the CSI registrar joins longhorn's.
	if grafana := appByID(t, inv, "grafana"); grafana.Containers != 2 || grafana.Version != "12.2.0" || grafana.Drift {
		t.Errorf("grafana = %+v", grafana)
	}

	if longhorn := appByID(t, inv, "longhorn"); longhorn.Containers != 2 {
		t.Errorf("longhorn = %+v", longhorn)
	}

	// A CSI sidecar alone in its pod stays a (system) app of its own.
	if csi := appByID(t, inv, "kubernetes-csi"); !csi.System || csi.Containers != 1 {
		t.Errorf("kubernetes-csi = %+v", csi)
	}

	if kuma := appByID(t, inv, "uptime-kuma"); !kuma.Unpinned {
		t.Errorf("uptime-kuma without a tag should be unpinned: %+v", kuma)
	}

	// An unknown image in a namespace named after an app belongs to it.
	if immich := appByID(t, inv, "immich"); immich.Containers != 1 {
		t.Errorf("immich = %+v", immich)
	}

	unknown := appByID(t, inv, "image:registry.example.com/internal/nightly-backup")
	if unknown.Known || unknown.Icon != "" || unknown.Name != "Nightly Backup" || unknown.Category != "other" {
		t.Errorf("unknown = %+v", unknown)
	}
}

func TestBuildInventoryRemoteIcon(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		{PodNamespace: "apps", Pod: "kimai-0", Name: "kimai", Image: "kimai/kimai2:apache"},
	}}})

	// The repository is "kimai2"; stripping nothing, it is matched by the upstream slug only if equal.
	for _, app := range inv.Apps {
		if app.Icon != "" {
			t.Errorf("an uncurated app must not claim a bundled icon: %+v", app)
		}
	}

	inv = buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		{PodNamespace: "apps", Pod: "kimai-0", Name: "kimai", Image: "ghcr.io/x/kimai:2"},
	}}})

	if app := appByID(t, inv, "kimai"); app.RemoteIcon != "kimai" || !app.Known || app.Name != "Kimai" {
		t.Errorf("kimai = %+v", app)
	}
}

func TestBuildInventoryAnonymousImage(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		{PodNamespace: "x", Pod: "nightly-job-29384756-x7k2p", Name: "c", Image: "sha256:0123"},
		// Digest-pinned images: the pod's workload name says what runs.
		{PodNamespace: "cyril-home", Pod: "home-assistant-0", Name: "app", Image: "sha256:4567"},
		{PodNamespace: "cyril-home", Pod: "home-assistant-0", Name: "init", Image: "docker.io/library/busybox:1.38"},
		// …or the namespace does.
		{PodNamespace: "cyril-zigbee2mqtt", Pod: "main-6f7c9d8b4-h4p2z", Name: "app", Image: "sha256:89ab"},
	}}})

	app := appByID(t, inv, "pod:x/nightly-job")
	if app.Name != "Nightly Job" || len(app.Images) != 1 || app.Images[0].Repo != "" || app.Images[0].Digest != "sha256:0123" || app.Unpinned {
		t.Errorf("anonymous = %+v", app)
	}

	if ha := appByID(t, inv, "home-assistant"); ha.Containers != 2 {
		t.Errorf("home-assistant = %+v", ha)
	}

	if z := appByID(t, inv, "zigbee2mqtt"); z.Containers != 1 {
		t.Errorf("zigbee2mqtt = %+v", z)
	}
}

func TestBuildInventoryBaseImagesAndVersion(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		{PodNamespace: "mqtt", Pod: "mosquitto-0", Name: "init", Image: "busybox:1.38"},
		{PodNamespace: "mqtt", Pod: "mosquitto-0", Name: "app", Image: "eclipse-mosquitto:2.0.22"},
		{PodNamespace: "tools", Pod: "relay-5c8d7f9b6-rx2k9", Name: "socat", Image: "alpine/socat:1.8"},
		{PodNamespace: "storage", Pod: "csi-attacher-7d9f8c6b5-k2m8p", Name: "attacher", Image: "longhornio/csi-attacher:v4.9"},
		{PodNamespace: "a", Pod: "ente-0", Name: "web", Image: "ghcr.io/ente/web:latest"},
		{PodNamespace: "b", Pod: "shop-0", Name: "web", Image: "registry.example.com/shop/web:1"},
	}}})

	// The version comes from the image that names the app, not from its init container.
	if m := appByID(t, inv, "mosquitto"); m.Version != "2.0.22" || m.Containers != 2 || m.Drift {
		t.Errorf("mosquitto = %+v", m)
	}

	// A base image alone is named after its workload, not "Socat".
	if relay := appByID(t, inv, "pod:tools/relay"); relay.Name != "Relay" {
		t.Errorf("relay = %+v", relay)
	}

	// The vendor's path beats the generic CSI sidecar name.
	if l := appByID(t, inv, "longhorn"); l.Containers != 1 {
		t.Errorf("longhorn = %+v", l)
	}

	// Two unrelated "web" images stay apart, named with their parent path.
	if ente := appByID(t, inv, "image:ghcr.io/ente/web"); ente.Name != "Ente Web" {
		t.Errorf("ente = %+v", ente)
	}

	if shop := appByID(t, inv, "image:registry.example.com/shop/web"); shop.Name != "Shop Web" {
		t.Errorf("shop = %+v", shop)
	}
}

func TestBuildInventoryMergesWorkloadTwins(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		// A CronJob of base images next to the app, named like it, joins it.
		{PodNamespace: "rss", Pod: "miniflux-ai-29384756-x7k2p", Name: "cleanup", Image: "busybox:latest"},
		{PodNamespace: "rss", Pod: "miniflux-ai-7d9f8c6b5-k2m8p", Name: "app", Image: "ghcr.io/qetesh/miniflux-ai:0.9.5"},
		// An exporter is an app of its own, not the software it watches.
		{PodNamespace: "mastodon", Pod: "es-exporter-0", Name: "exporter", Image: "quay.io/prometheuscommunity/elasticsearch-exporter:v1.10.0"},
	}}})

	ai := appByID(t, inv, "image:ghcr.io/qetesh/miniflux-ai")
	if ai.Containers != 2 || ai.Version != "0.9.5" || ai.Unpinned || len(inv.Apps) != 2 {
		t.Errorf("miniflux-ai = %+v (apps %d)", ai, len(inv.Apps))
	}

	if es := appByID(t, inv, "image:quay.io/prometheuscommunity/elasticsearch-exporter"); es.Name != "Elasticsearch Exporter" || es.Category != "observability" {
		t.Errorf("exporter = %+v", es)
	}
}

func TestBuildInventoryIgnoresExitedForVersions(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		// Two finished runs of the old CronJob image, one live container of the new one.
		{PodNamespace: "a", Pod: "sonarr-29384756-x7k2p", Name: "app", Image: "ghcr.io/home-operations/sonarr:4.0.14", Status: "CONTAINER_EXITED"},
		{PodNamespace: "a", Pod: "sonarr-29384757-k2m8p", Name: "app", Image: "ghcr.io/home-operations/sonarr:4.0.14", Status: "CONTAINER_EXITED"},
		{PodNamespace: "a", Pod: "sonarr-0", Name: "app", Image: "ghcr.io/home-operations/sonarr:4.0.15", Status: "CONTAINER_RUNNING"},
		// Only exited containers: their images still version the app.
		{PodNamespace: "b", Pod: "radarr-0", Name: "app", Image: "ghcr.io/home-operations/radarr:5.27.5", Status: "CONTAINER_EXITED"},
	}}})

	if s := appByID(t, inv, "sonarr"); s.Version != "4.0.15" || s.Drift || s.Containers != 3 {
		t.Errorf("sonarr = %+v", s)
	}

	if r := appByID(t, inv, "radarr"); r.Version != "5.27.5" {
		t.Errorf("radarr = %+v", r)
	}
}

func TestBuildInventoryRuntimesAreNotApps(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		{PodNamespace: "a", Pod: "report-29384756-x7k2p", Name: "job", Image: "python:3.12-slim"},
		{PodNamespace: "b", Pod: "builder-0", Name: "build", Image: "golang:1.26"},
		{PodNamespace: "c", Pod: "grafana-0", Name: "grafana", Image: "grafana/grafana:12.2.0"},
		{PodNamespace: "c", Pod: "grafana-0", Name: "init", Image: "docker.io/library/python:3.11"},
	}}})

	for _, app := range inv.Apps {
		if app.Name == "Python" || app.Name == "Golang" {
			t.Errorf("a language runtime became an app: %+v", app)
		}
	}

	if g := appByID(t, inv, "grafana"); g.Containers != 2 || g.Version != "12.2.0" {
		t.Errorf("grafana = %+v", g)
	}

	if r := appByID(t, inv, "pod:a/report"); r.Name != "Report" || r.Known {
		t.Errorf("report = %+v", r)
	}
}

func TestPodBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"immich-server-7d9f8c6b5-k2m8p": "immich-server",
		"home-assistant-0":              "home-assistant",
		"cilium-8h2kd":                  "cilium",
		"backup-29384756-x7k2p":         "backup",
		"grafana":                       "grafana",
		"paperless-ngx-0":               "paperless-ngx",
		"0":                             "0",
	} {
		if got := podBaseName(in); got != want {
			t.Errorf("podBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDemoInventory(t *testing.T) {
	inv := demoInventory(1)
	if len(inv.Apps) < 15 || inv.Answered != 5 {
		t.Fatalf("demo inventory too small: %d apps", len(inv.Apps))
	}

	if cilium := appByID(t, inv, "cilium"); !cilium.Drift {
		t.Errorf("the demo shows version drift on cilium: %+v", cilium)
	}

	if _, err := json.Marshal(inv); err != nil {
		t.Fatal(err)
	}
}

// Screenshot mode keeps third-party logos (trademarks) out of screenshots: every app falls back to
// its monogram, while names stay.
func TestInventoryScreenshotModeDropsLogos(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	out, err := inventoryJSON(buildInventory(1, 1, []nodeContainers{{Node: "n", Containers: []containerInfo{
		{PodNamespace: "monitoring", Pod: "grafana-0", Name: "grafana", Image: "grafana/grafana:12.2.0"},
		{PodNamespace: "apps", Pod: "kimai-0", Name: "kimai", Image: "ghcr.io/x/kimai:2"},
	}}}))
	if err != nil {
		t.Fatal(err)
	}

	var inv inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}

	for _, app := range inv.Apps {
		if app.Icon != "" || app.RemoteIcon != "" {
			t.Errorf("screenshot mode leaked a logo: %+v", app)
		}
	}

	if app := appByID(t, inv, "grafana"); app.Name != "Grafana" || !app.Known {
		t.Errorf("names stay in screenshot mode: %+v", app)
	}
}

func TestInventoryKeepsLogosWithoutScreenshotMode(t *testing.T) {
	out, err := inventoryJSON(demoInventory(1))
	if err != nil {
		t.Fatal(err)
	}

	var inv inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}

	if app := appByID(t, inv, "grafana"); app.Icon != "grafana" {
		t.Errorf("grafana = %+v", app)
	}
}

// Every catalog app with an icon has it bundled (scripts/sync-app-icons.py), and names are unique.
func TestAppCatalogBundle(t *testing.T) {
	catalog := loadAppCatalog()
	assets := filepath.Join("..", "..", "app", "src", "main", "assets", "appicons")
	ids := map[string]bool{}

	for i := range catalog.apps {
		app := &catalog.apps[i]
		if ids[app.ID] {
			t.Errorf("duplicate id %q", app.ID)
		}

		ids[app.ID] = true

		if app.Name == "" || app.Category == "" {
			t.Errorf("%s: missing name or category", app.ID)
		}

		if app.hasIcon() {
			if _, err := os.Stat(filepath.Join(assets, app.ID+".webp")); err != nil {
				t.Errorf("%s: icon not bundled: %v", app.ID, err)
			}
		}
	}

	if len(catalog.slugs) < 1000 {
		t.Errorf("only %d upstream icon slugs", len(catalog.slugs))
	}
}
