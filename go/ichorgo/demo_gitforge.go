package ichorgo

import "strings"

// The demo's Git repository: one app, podinfo, as a GitOps repository would hold it.

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
}

// demoGitForgeTree lists one directory of the demo repository.
func demoGitForgeTree(dir string) ([]gitForgeEntry, error) {
	seen := map[string]bool{}

	var entries []gitForgeEntry

	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}

	for path, content := range demoGitForgeFiles {
		rest, ok := strings.CutPrefix(path, prefix)
		if !ok {
			continue
		}

		if name, _, isDir := strings.Cut(rest, "/"); isDir {
			if !seen[prefix+name] {
				seen[prefix+name] = true
				entries = append(entries, gitForgeEntry{Path: prefix + name, Type: "dir"})
			}

			continue
		}

		entries = append(entries, gitForgeEntry{Path: path, Type: "file", Size: int64(len(content))})
	}

	if len(entries) == 0 {
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
