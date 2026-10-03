package talosmobile

import "time"

// demoWorkloads is a typical homelab, spread over the demo nodes: "pod/container image" per entry.
var demoWorkloads = []struct {
	node       int // index into demoNodes()
	namespace  string
	pod        string
	containers [][2]string // container name, image
}{
	{0, "kube-system", "kube-apiserver-demo-cp-1", [][2]string{{"kube-apiserver", "registry.k8s.io/kube-apiserver:v1.34.1"}}},
	{1, "kube-system", "kube-apiserver-demo-cp-2", [][2]string{{"kube-apiserver", "registry.k8s.io/kube-apiserver:v1.34.1"}}},
	{2, "kube-system", "kube-apiserver-demo-cp-3", [][2]string{{"kube-apiserver", "registry.k8s.io/kube-apiserver:v1.34.1"}}},
	{0, "kube-system", "kube-scheduler-demo-cp-1", [][2]string{{"kube-scheduler", "registry.k8s.io/kube-scheduler:v1.34.1"}}},
	{0, "kube-system", "coredns-7c9d-x2k4p", [][2]string{{"coredns", "registry.k8s.io/coredns/coredns:v1.12.3"}}},
	{1, "kube-system", "coredns-7c9d-q8m2n", [][2]string{{"coredns", "registry.k8s.io/coredns/coredns:v1.12.3"}}},
	{3, "kube-system", "cilium-8h2kd", [][2]string{{"cilium-agent", "quay.io/cilium/cilium:v1.18.2"}}},
	{4, "kube-system", "cilium-p9x7c", [][2]string{{"cilium-agent", "quay.io/cilium/cilium:v1.18.1"}}},
	{0, "kube-system", "cilium-operator-5d8f-l2v9q", [][2]string{{"cilium-operator", "quay.io/cilium/operator-generic:v1.18.2"}}},
	{1, "kube-system", "metrics-server-6f7c-h4p2z", [][2]string{{"metrics-server", "registry.k8s.io/metrics-server/metrics-server:v0.8.0"}}},
	{3, "cert-manager", "cert-manager-7b5d-9kq2w", [][2]string{{"cert-manager-controller", "quay.io/jetstack/cert-manager-controller:v1.18.2"}}},
	{4, "cert-manager", "cert-manager-cainjector-5c9-tt7x2", [][2]string{{"cert-manager-cainjector", "quay.io/jetstack/cert-manager-cainjector:v1.18.2"}}},
	{3, "longhorn-system", "longhorn-manager-r6k2p", [][2]string{{"longhorn-manager", "docker.io/longhornio/longhorn-manager:v1.9.1"}}},
	{4, "longhorn-system", "longhorn-manager-v3m8d", [][2]string{{"longhorn-manager", "docker.io/longhornio/longhorn-manager:v1.9.1"}}},
	{3, "longhorn-system", "longhorn-csi-plugin-b8w4k", [][2]string{
		{"node-driver-registrar", "registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.14.0"},
		{"longhorn-csi-plugin", "docker.io/longhornio/longhorn-manager:v1.9.1"},
	}},
	{3, "monitoring", "prometheus-kube-prometheus-0", [][2]string{
		{"prometheus", "quay.io/prometheus/prometheus:v3.6.0"},
		{"config-reloader", "quay.io/prometheus-operator/prometheus-config-reloader:v0.86.0"},
	}},
	{4, "monitoring", "grafana-6d8b-9fz2t", [][2]string{
		{"grafana", "docker.io/grafana/grafana:12.2.0"},
		{"grafana-sc-dashboard", "quay.io/kiwigrid/k8s-sidecar:1.30.10"},
	}},
	{3, "monitoring", "loki-0", [][2]string{{"loki", "docker.io/grafana/loki:3.5.5"}}},
	{4, "flux-system", "source-controller-7f9c-2xk8p", [][2]string{{"manager", "ghcr.io/fluxcd/source-controller:v1.7.0"}}},
	{4, "flux-system", "kustomize-controller-5b8d-n4q9z", [][2]string{{"manager", "ghcr.io/fluxcd/kustomize-controller:v1.7.0"}}},
	{3, "networking", "traefik-8c6d-w7r2m", [][2]string{{"traefik", "docker.io/traefik:v3.5.3"}}},
	{4, "home", "home-assistant-0", [][2]string{{"app", "ghcr.io/home-assistant/home-assistant:2025.10.1"}}},
	{3, "home", "zigbee2mqtt-0", [][2]string{{"app", "ghcr.io/koenkk/zigbee2mqtt:2.6.2"}}},
	{4, "home", "mosquitto-0", [][2]string{{"app", "docker.io/library/eclipse-mosquitto:2.0.22"}}},
	{3, "media", "jellyfin-5f8c-xq2wz", [][2]string{{"app", "ghcr.io/jellyfin/jellyfin:10.11.0"}}},
	{4, "media", "immich-server-7d9f-k2m8p", [][2]string{{"immich-server", "ghcr.io/immich-app/immich-server:v2.0.1"}}},
	{3, "media", "immich-machine-learning-6c4-p8x2n", [][2]string{{"immich-machine-learning", "ghcr.io/immich-app/immich-machine-learning:v2.0.1"}}},
	{3, "media", "immich-postgres-1", [][2]string{{"postgres", "ghcr.io/cloudnative-pg/postgresql:16.10"}}},
	{4, "media", "sonarr-0", [][2]string{{"app", "ghcr.io/home-operations/sonarr:4.0.15"}}},
	{4, "media", "radarr-0", [][2]string{{"app", "ghcr.io/home-operations/radarr:5.27.5"}}},
	{3, "default", "vaultwarden-0", [][2]string{{"vaultwarden", "docker.io/vaultwarden/server:1.34.3"}}},
	{4, "default", "paperless-0", [][2]string{
		{"paperless", "ghcr.io/paperless-ngx/paperless-ngx:2.18.4"},
		{"redis", "docker.io/library/redis:8.2"},
	}},
	{3, "default", "uptime-kuma-0", [][2]string{{"app", "docker.io/louislam/uptime-kuma:latest"}}},
	{4, "demo", "hello-ichor", [][2]string{{"web", "nginx:1.27"}}},
	{3, "tools", "it-backup-5c8d-rx2k9", [][2]string{{"backup", "registry.example.com/internal/nightly-backup:2025.10"}}},
}

func demoInventory(now int64) inventory {
	nodes := demoNodes()
	lists := make([]nodeContainers, len(nodes))

	for i, n := range nodes {
		lists[i].Node = n.Node
		lists[i].Containers = []containerInfo{}
	}

	uptime := time.Since(demoBoot).Seconds()

	for i, w := range demoWorkloads {
		for j, c := range w.containers {
			lists[w.node].Containers = append(lists[w.node].Containers, containerInfo{
				ID: w.pod + "/" + c[0], PodNamespace: w.namespace, Pod: w.pod, Name: c[0], Image: c[1],
				Status: "CONTAINER_RUNNING", Memory: uint64(24+(i*37+j*11)%380) << 20, CPUNanos: uint64(uptime * 1e7),
			})
		}
	}

	return buildInventory(now, len(nodes), lists)
}
