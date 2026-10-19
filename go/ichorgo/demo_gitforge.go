package ichorgo

import (
	"cmp"
	"strings"
)

// The demo's Git repository: one app, podinfo, as a GitOps repository would hold it, and the
// demo cluster's own configuration (Talos patches per node, a Kubernetes tree, SOPS files).

// demoClusterRepoURL is the demo cluster's saved repository; its head is demoClusterRepoHead.
const (
	demoClusterRepoURL  = "https://git.home.example/example-org/cluster-config"
	demoClusterRepoHead = "c1u5t3rc0nf1g" + "000000000000000000000000000"
)

var demoGitForgeFiles = map[string]string{
	"apps/podinfo/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: demo
resources:
  - deployment.yaml
  - hpa.yaml
`,
	"apps/podinfo/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: podinfo
spec:
  replicas: 2
  selector:
    matchLabels:
      app: podinfo
  template:
    metadata:
      labels:
        app: podinfo
    spec:
      containers:
        - name: podinfo
          image: ghcr.io/stefanprodan/podinfo:6.7.0
          ports:
            - containerPort: 9898
`,
	"apps/podinfo/hpa.yaml": `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: podinfo
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: podinfo
  minReplicas: 2
  maxReplicas: 4
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 80
`,
	"README.md": `# cluster-config

Configuration of the lab cluster: Talos machine config patches under talos/, generated
with talhelper, and the Kubernetes tree Flux applies under kubernetes/.

Secrets are encrypted with SOPS and age (see .sops.yaml).
`,
	".sops.yaml": `creation_rules:
  - path_regex: talos/.*\.sops\.ya?ml
    age: age1examplerecipient0000000000000000000000000000000000000000
  - path_regex: kubernetes/.*\.sops\.ya?ml
    encrypted_regex: ^(data|stringData)$
    age: age1examplerecipient0000000000000000000000000000000000000000
`,
	"talos/talconfig.yaml": `clusterName: lab
talosVersion: v1.11.2
kubernetesVersion: v1.34.1
endpoint: https://10.0.0.10:6443
patches:
  - "@./patches/network.yaml"
nodes:
  - hostname: cp-1
    ipAddress: 10.0.0.11
    controlPlane: true
    installDisk: /dev/sda
    patches:
      - "@./nodes/cp-1.yaml"
  - hostname: worker-a
    ipAddress: 10.0.0.21
    controlPlane: false
    installDisk: /dev/nvme0n1
    patches:
      - "@./nodes/worker-a.yaml"
`,
	"talos/patches/network.yaml": `machine:
  network:
    nameservers:
      - 10.0.0.1
  time:
    servers:
      - time.cloudflare.com
`,
	"talos/nodes/cp-1.yaml": `machine:
  network:
    hostname: cp-1
    interfaces:
      - interface: eth0
        dhcp: false
        addresses:
          - 10.0.0.11/24
        vip:
          ip: 10.0.0.10
`,
	"talos/nodes/worker-a.yaml": `machine:
  network:
    hostname: worker-a
  kubelet:
    extraArgs:
      max-pods: "150"
  nodeLabels:
    node.kubernetes.io/pool: general
`,
	"talos/talsecret.sops.yaml": `cluster:
    id: ENC[AES256_GCM,data:ZXhhbXBsZQ==,iv:aXY=,tag:dGFn,type:str]
    secret: ENC[AES256_GCM,data:ZXhhbXBsZQ==,iv:aXY=,tag:dGFn,type:str]
secrets:
    bootstraptoken: ENC[AES256_GCM,data:ZXhhbXBsZQ==,iv:aXY=,tag:dGFn,type:str]
sops:
    age:
        - recipient: age1examplerecipient0000000000000000000000000000000000000000
          enc: |
            -----BEGIN AGE ENCRYPTED FILE-----
            cGxhY2Vob2xkZXIgY2lwaGVydGV4dCwgbm90IGEgcmVhbCBrZXk=
            -----END AGE ENCRYPTED FILE-----
    lastmodified: "2026-10-01T08:00:00Z"
    mac: ENC[AES256_GCM,data:bWFj,iv:aXY=,tag:dGFn,type:str]
    version: 3.10.2
`,
	"kubernetes/apps/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - web/deployment.yaml
`,
	"kubernetes/apps/web/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: web
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: ghcr.io/example-org/web:1.4.0
          ports:
            - containerPort: 8080
`,
}

// demoGitForgeRepo is the demo cluster repository as GitForgeRepo answers it.
func demoGitForgeRepo(branch string) gitForgeRepoInfo {
	return gitForgeRepoInfo{
		DefaultBranch: "main", Branch: cmp.Or(branch, "main"), HeadSha: demoClusterRepoHead,
		WebURL: demoClusterRepoURL, Private: true, CanPush: true, Forge: gitForgeDemo,
	}
}

// demoGitForgeTree lists one directory of the demo repository.
func demoGitForgeTree(dir string) ([]gitForgeEntry, error) {
	sizes := make(map[string]int64, len(demoGitForgeFiles))
	for p, c := range demoGitForgeFiles {
		sizes[p] = int64(len(c))
	}

	entries := listTreeDir(sizes, dir)
	if entries == nil {
		return nil, errGitForgeNotFound
	}

	return entries, nil
}

// demoGitForgeFile reads one file of the demo repository.
func demoGitForgeFile(path string) (gitForgeFile, error) {
	content, ok := demoGitForgeFiles[path]
	if !ok {
		return gitForgeFile{}, errGitForgeNotFound
	}

	return gitForgeFile{Path: path, SHA: "0d1e2m3o4" + strings.Repeat("0", 31), Size: int64(len(content)), Content: content}, nil
}
