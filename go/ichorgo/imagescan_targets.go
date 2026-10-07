package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// scanPod is the part of a pod an image scan reads: its images, as asked and as pulled, and
// the secrets the kubelet pulled them with.
type scanPod struct {
	Metadata struct {
		OwnerReferences []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Controller bool   `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Containers       []scanContainer         `json:"containers"`
		InitContainers   []scanContainer         `json:"initContainers"`
		ImagePullSecrets []struct{ Name string } `json:"imagePullSecrets"`
	} `json:"spec"`
	Status struct {
		ContainerStatuses     []scanContainerStatus `json:"containerStatuses"`
		InitContainerStatuses []scanContainerStatus `json:"initContainerStatuses"`
	} `json:"status"`
}

type scanContainer struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type scanContainerStatus struct {
	Name    string `json:"name"`
	ImageID string `json:"imageID"`
}

// scanTarget is one image to scan, and what the scan needs to pull it.
type scanTarget struct {
	image   string   // as the pods name it
	ref     string   // what Trivy pulls: repo@digest when the kubelet recorded it
	digest  string   // sha256:…, "" when unknown
	pods    []string // namespace/pod
	secrets []string // namespace/name of the pull secrets of those pods
	owners  []string // namespace/kind/name of the pods' controllers (the operator's report keys)
}

// readScanTargets reads the pods and returns their distinct images (by what is pulled), in
// the pods' order. A pod gone since is skipped; none left is a refusal.
func readScanTargets(ctx context.Context, k *kubeClient, refs []routePod) ([]scanTarget, error) {
	var targets []scanTarget

	index := map[string]int{}

	for _, r := range refs {
		var pod scanPod

		err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(r.Namespace)+"/pods/"+url.PathEscape(r.Pod), &pod)
		if isNotFound(err) {
			continue
		}

		if err != nil {
			return nil, err
		}

		secrets := make([]string, 0, len(pod.Spec.ImagePullSecrets))
		for _, s := range pod.Spec.ImagePullSecrets {
			secrets = append(secrets, r.Namespace+"/"+s.Name)
		}

		owner := r.Namespace + "/Pod/" + r.Pod
		for _, o := range pod.Metadata.OwnerReferences {
			if o.Controller {
				owner = r.Namespace + "/" + o.Kind + "/" + o.Name
			}
		}

		for _, t := range podScanTargets(pod) {
			i, ok := index[t.ref]
			if !ok {
				i = len(targets)
				index[t.ref] = i
				targets = append(targets, t)
			}

			targets[i].pods = appendNew(targets[i].pods, r.Namespace+"/"+r.Pod)
			targets[i].owners = appendNew(targets[i].owners, owner)
			for _, s := range secrets {
				targets[i].secrets = appendNew(targets[i].secrets, s)
			}
		}
	}

	if len(targets) == 0 {
		return nil, netPerfRefused("none of these pods runs any more: reload the app and try again")
	}

	return targets, nil
}

func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}

	return append(list, s)
}

// podScanTargets lists a pod's images: each container's as pulled (its status' imageID,
// repo@digest) when it has one, else as the spec names it.
func podScanTargets(pod scanPod) []scanTarget {
	pulled := map[string]string{} // container name -> repo@digest

	for _, s := range append(slices.Clone(pod.Status.InitContainerStatuses), pod.Status.ContainerStatuses...) {
		if ref := pulledRef(s.ImageID); ref != "" {
			pulled[s.Name] = ref
		}
	}

	var out []scanTarget

	for _, c := range append(slices.Clone(pod.Spec.InitContainers), pod.Spec.Containers...) {
		t := scanTarget{image: c.Image, ref: cmpOr(pulled[c.Name], c.Image)}

		_, t.digest, _ = strings.Cut(t.ref, "@")
		if t.ref != "" && !slices.ContainsFunc(out, func(o scanTarget) bool { return o.ref == t.ref }) {
			out = append(out, t)
		}
	}

	return out
}

// pulledRef is the repo@sha256:… a container status' imageID names, "" when it only names
// a local image ID (sha256:… alone: the image was built or loaded on the node).
func pulledRef(imageID string) string {
	ref := strings.TrimPrefix(imageID, "docker-pullable://")
	if repo, digest, ok := strings.Cut(ref, "@"); ok && repo != "" && strings.HasPrefix(digest, "sha256:") {
		return ref
	}

	return ""
}

// scanSecret is a pull secret's data.
type scanSecret struct {
	Type string            `json:"type"`
	Data map[string]string `json:"data"` // base64
}

// dockerConfig is a Docker config.json: registry -> credentials.
type dockerConfig struct {
	Auths map[string]json.RawMessage `json:"auths"`
}

// readPullCredentials merges the given pull secrets (namespace/name) into one Docker
// config.json holding only the registries in want (the scanned images'), nil when none
// has credentials for them. A secret gone or of another type is skipped, as the kubelet
// does; the first secret naming a registry wins.
func readPullCredentials(ctx context.Context, k *kubeClient, secrets []string, want map[string]bool) ([]byte, []string, error) {
	merged := dockerConfig{Auths: map[string]json.RawMessage{}}

	var used []string

	for _, s := range secrets {
		ns, name, _ := strings.Cut(s, "/")

		var secret scanSecret

		err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(ns)+"/secrets/"+url.PathEscape(name), &secret)
		if isNotFound(err) {
			continue
		}

		if err != nil {
			return nil, nil, fmt.Errorf("read the pull secret %s: %w", s, err)
		}

		auths, err := secretAuths(secret)
		if err != nil {
			return nil, nil, fmt.Errorf("pull secret %s: %w", s, err)
		}

		for registry, auth := range auths {
			if _, ok := merged.Auths[registry]; !ok && want[registryHost(registry)] {
				merged.Auths[registry] = auth
				used = appendNew(used, s)
			}
		}
	}

	if len(merged.Auths) == 0 {
		return nil, nil, nil
	}

	data, err := json.Marshal(merged)

	return data, used, err
}

// secretAuths reads the registries of a kubernetes.io/dockerconfigjson secret, or of a
// legacy kubernetes.io/dockercfg one (the auths map alone).
func secretAuths(s scanSecret) (map[string]json.RawMessage, error) {
	var key string

	switch s.Type {
	case "kubernetes.io/dockerconfigjson":
		key = ".dockerconfigjson"
	case "kubernetes.io/dockercfg":
		key = ".dockercfg"
	default:
		return nil, nil
	}

	raw, err := base64.StdEncoding.DecodeString(s.Data[key])
	if err != nil {
		return nil, errors.New("unreadable data")
	}

	if key == ".dockercfg" {
		var auths map[string]json.RawMessage

		return auths, json.Unmarshal(raw, &auths)
	}

	var cfg dockerConfig

	return cfg.Auths, json.Unmarshal(raw, &cfg)
}

// registryHost is the registry a Docker config key names ("https://index.docker.io/v1/",
// "ghcr.io"), as image references name it (docker.io for Docker Hub).
func registryHost(key string) string {
	host := key
	if _, rest, ok := strings.Cut(host, "://"); ok {
		host = rest
	}

	host, _, _ = strings.Cut(host, "/")

	return operatorRegistry(strings.ToLower(host))
}

// targetRegistries are the registries the targets are pulled from.
func targetRegistries(targets []scanTarget) map[string]bool {
	out := map[string]bool{}

	for _, t := range targets {
		out[parseImageRef(t.ref).Registry] = true
	}

	return out
}
