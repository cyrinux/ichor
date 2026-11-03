package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Ollama plugin's two Go readers (go/plugin/builtin/ollama.yaml). Ollama has no CRD: it
// runs as a Deployment (or a StatefulSet) whose image the app catalog identifies as ollama,
// behind a Service on port 11434, and answers an unauthenticated HTTP API. The readers reach
// that API through the API server's Service proxy (the pod proxy of the first ready pod when
// no Service selects the pods), so the API server's RBAC on services/proxy (pods/proxy) is the
// only gate. Both return synthetic objects (ichor.dev/v1alpha1, a group no cluster serves)
// whose source names the workload a row opens (ref: $.source):
//   - native:ollama-servers: one OllamaServer per workload, with its version and the counts
//     and sizes of its models on disk (/api/tags) and loaded (/api/ps);
//   - native:ollama-models: one OllamaModel per model on disk per server, joined with the
//     loaded ones; a loaded model no longer on disk (deleted while loaded) is an orphan row.

const (
	// ollamaPort is the API port Ollama listens on (OLLAMA_HOST's default).
	ollamaPort = 11434
	// ollamaCallTimeout bounds one call to a server's API.
	ollamaCallTimeout = 10 * time.Second
	// ollamaMaxBody caps an answer: /api/tags of a large library is tens of KB.
	ollamaMaxBody = 1 << 20
	// ollamaPluginVersion is the synthetic objects' API version.
	ollamaPluginVersion = "ichor.dev/v1alpha1"
)

// ollamaGPUResources are the extended resources a container asks a GPU with.
var ollamaGPUResources = []string{"nvidia.com/gpu", "amd.com/gpu", "gpu.intel.com/i915", "gpu.intel.com/xe"}

// ollamaWorkload is a Deployment or StatefulSet as the readers need it.
type ollamaWorkload struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int `json:"replicas"`
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
		Template struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Containers []ollamaContainer `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

type ollamaContainer struct {
	Image string `json:"image"`
	Ports []struct {
		Name          string `json:"name"`
		ContainerPort int    `json:"containerPort"`
	} `json:"ports"`
	Resources struct {
		Limits map[string]any `json:"limits"`
	} `json:"resources"`
}

// ollamaServiceObject is a Service as the readers need it.
type ollamaServiceObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Selector map[string]string `json:"selector"`
		Ports    []struct {
			Name       string `json:"name"`
			Port       int    `json:"port"`
			TargetPort any    `json:"targetPort"`
		} `json:"ports"`
	} `json:"spec"`
}

// ollamaPodObject is a pod as the pod-proxy fallback needs it.
type ollamaPodObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Phase      string               `json:"phase"`
		Conditions []ollamaPodCondition `json:"conditions"`
	} `json:"status"`
}

type ollamaPodCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// ollamaTag is one /api/tags entry: a model on disk.
type ollamaTag struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	Details    struct {
		Format            string `json:"format"`
		Family            string `json:"family"`
		ParameterSize     string `json:"parameter_size"`
		QuantizationLevel string `json:"quantization_level"`
	} `json:"details"`
}

// ollamaRunning is one /api/ps entry: a loaded model. Size is the loaded footprint (weights
// and KV cache), not the size on disk.
type ollamaRunning struct {
	ollamaTag

	ExpiresAt string `json:"expires_at"`
	SizeVRAM  int64  `json:"size_vram"`
}

// ollamaTarget is how a server's API is reached: a proxy path prefix, and the RBAC resource
// that guards it (services/proxy or pods/proxy).
type ollamaTarget struct {
	prefix   string
	resource string
}

// ollamaServiceRef is the Service in front of a server.
type ollamaServiceRef struct {
	Name string
	Port int
}

// ollamaServer is one Ollama workload and what its API said.
type ollamaServer struct {
	kind      string // Deployment or StatefulSet
	resource  string // deployments or statefulsets
	namespace string
	name      string
	replicas  int
	ready     int
	gpu       int
	labels    map[string]string // the pod template's, which a Service selects
	selector  map[string]string // the workload's matchLabels, to list its pods
	podPort   int               // the container port the pod proxy asks
	service   *ollamaServiceRef
	target    ollamaTarget

	version   string
	reachable bool
	forbidden bool
	err       string
	tags      []ollamaTag
	running   []ollamaRunning
}

// source is the workload a row of the server (or of its models) opens.
func (s ollamaServer) source() map[string]any {
	return map[string]any{
		"group": "apps", "version": "v1", "resource": s.resource, "kind": s.kind,
		"namespace": s.namespace, "name": s.name,
	}
}

// ollamaGet reads path (such as /api/tags) from the server behind target and decodes the
// JSON answer into out (nil to drop it).
func ollamaGet(ctx context.Context, k *kubeClient, target, path string, out any) error {
	return ollamaCall(ctx, k, ollamaCallTimeout, http.MethodGet, target, path, nil, out)
}

// ollamaCall makes one request to an Ollama API through the API server's proxy: target is the
// proxy prefix (".../services/<svc>:<port>/proxy" or ".../pods/<pod>:<port>/proxy"), body is
// sent as JSON when not nil. Each call is bounded by timeout and its answer by ollamaMaxBody;
// a non-2xx answer is a *kubeAPIError: the API server's (a 403 the proxy's RBAC refusal), or
// Ollama's own ({"error": "…"}), whose Reason is ollamaReason.
func ollamaCall(ctx context.Context, k *kubeClient, timeout time.Duration, method, target, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		data        []byte
		contentType string
	)

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Ollama request: %w", err)
		}

		data, contentType = raw, "application/json"
	}

	resp, err := k.request(ctx, method, target+path, "application/json", contentType, data, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	answer, err := io.ReadAll(io.LimitReader(resp.Body, ollamaMaxBody+1))
	if err != nil {
		return err
	}

	if len(answer) > ollamaMaxBody {
		return fmt.Errorf("the Ollama answer is larger than %d bytes", ollamaMaxBody)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ollamaStatusError(resp.StatusCode, answer)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("decode Ollama answer: %w", err)
	}

	return nil
}

// ollamaError is the reason a call failed, as a server row shows it, and whether it was
// refused: a 403 is the RBAC refusal of resource, anything else the API server's or the
// backend's message, with the proxy path prefix (when one was asked) cut out of it: the
// apps never show a proxy URL.
// ollamaReason is the Reason of a *kubeAPIError Ollama answered itself, not the API server.
const ollamaReason = "Ollama"

// ollamaStatusError is a non-2xx answer: the API server's Status, or Ollama's {"error"} (its
// message kept, the body's first 200 bytes when it has none).
func ollamaStatusError(code int, answer []byte) error {
	if isKubeStatus(answer) {
		return kubeStatusError(code, answer)
	}

	var body struct {
		Error string `json:"error"`
	}

	msg := strings.TrimSpace(string(answer))
	if json.Unmarshal(answer, &body) == nil && body.Error != "" {
		msg = body.Error
	}

	if len(msg) > 200 {
		msg = msg[:200]
	}

	return &kubeAPIError{Code: code, Reason: ollamaReason, Message: msg}
}

func ollamaError(err error, resource, prefix string) (string, bool) {
	if isForbidden(err) {
		return "the credentials may not read " + resource, true
	}

	msg := kubeError(err).Error()
	if prefix != "" {
		msg = strings.ReplaceAll(msg, prefix, "")
	}

	return msg, false
}

// readOllamaServers is native:ollama-servers.
func readOllamaServers(ctx context.Context, k *kubeClient, _ *pluginProbe, _ plugin.Resource, scope string) ([]map[string]any, error) {
	servers, err := listOllamaServers(ctx, k, scope)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(servers))
	for _, s := range servers {
		out = append(out, ollamaServerObject(s))
	}

	return out, nil
}

// readOllamaModels is native:ollama-models.
func readOllamaModels(ctx context.Context, k *kubeClient, _ *pluginProbe, _ plugin.Resource, scope string) ([]map[string]any, error) {
	servers, err := listOllamaServers(ctx, k, scope)
	if err != nil {
		return nil, err
	}

	out := []map[string]any{}
	for _, s := range servers {
		out = append(out, ollamaModelObjects(s)...)
	}

	return out, nil
}

// ollamaServerObject is the OllamaServer of s.
func ollamaServerObject(s ollamaServer) map[string]any {
	var disk, vram int64
	for _, t := range s.tags {
		disk += t.Size
	}

	for _, r := range s.running {
		vram += r.SizeVRAM
	}

	spec := map[string]any{"gpu": s.gpu, "service": nil}
	if s.service != nil {
		spec["service"] = map[string]any{"name": s.service.Name, "port": s.service.Port}
	}

	status := map[string]any{
		"replicas": s.replicas, "readyReplicas": s.ready, "reachable": s.reachable, "forbidden": s.forbidden,
		"version": s.version, "loaded": len(s.running), "models": len(s.tags), "diskBytes": disk, "vramBytes": vram,
	}
	if s.err != "" {
		status["error"] = s.err
	}

	return map[string]any{
		"apiVersion": ollamaPluginVersion, "kind": "OllamaServer",
		"metadata": map[string]any{"namespace": s.namespace, "name": s.name},
		"spec":     spec, "status": status, "source": s.source(),
	}
}

// ollamaModelObjects are the OllamaModels of s: one per model on disk, joined with the
// loaded ones on digest (else name), then the loaded ones no longer on disk.
func ollamaModelObjects(s ollamaServer) []map[string]any {
	used := make([]bool, len(s.running))
	out := make([]map[string]any, 0, len(s.tags)+len(s.running))

	for _, t := range s.tags {
		status := map[string]any{"loaded": false, "orphan": false}

		if i := ollamaRunningFor(s.running, used, t); i >= 0 {
			used[i] = true
			status = ollamaLoadedStatus(s.running[i], false)
		}

		out = append(out, ollamaModelObject(s, t, status))
	}

	for i, r := range s.running {
		if !used[i] {
			out = append(out, ollamaModelObject(s, r.ollamaTag, ollamaLoadedStatus(r, true)))
		}
	}

	return out
}

// ollamaRunningFor is the index of the loaded entry of t not yet joined: the same digest,
// else the same name; -1 when t is not loaded.
func ollamaRunningFor(running []ollamaRunning, used []bool, t ollamaTag) int {
	if t.Digest != "" {
		for i, r := range running {
			if !used[i] && r.Digest == t.Digest {
				return i
			}
		}
	}

	for i, r := range running {
		if !used[i] && (r.Name == t.Name || r.Model == t.Name) {
			return i
		}
	}

	return -1
}

func ollamaModelObject(s ollamaServer, t ollamaTag, status map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": ollamaPluginVersion, "kind": "OllamaModel",
		"metadata": map[string]any{"namespace": s.namespace, "name": cmp.Or(t.Name, t.Model)},
		"spec": map[string]any{
			"server": s.name, "serverGpu": s.gpu, "size": t.Size, "digest": ollamaShortDigest(t.Digest),
			"modifiedAt": t.ModifiedAt, "family": t.Details.Family, "parameterSize": t.Details.ParameterSize,
			"quantization": t.Details.QuantizationLevel, "format": t.Details.Format,
		},
		"status": status, "source": s.source(),
	}
}

// ollamaLoadedStatus is a loaded model's status: its footprint, VRAM share and expiry.
func ollamaLoadedStatus(r ollamaRunning, orphan bool) map[string]any {
	share, processor := ollamaProcessor(r.Size, r.SizeVRAM)

	return map[string]any{
		"loaded": true, "orphan": orphan, "size": r.Size, "sizeVRAM": r.SizeVRAM,
		"expiresAt": r.ExpiresAt, "gpuShare": share, "processor": processor,
	}
}

// ollamaProcessor is the GPU share (0–100) of a loaded model and the CLI's Processor column
// for it ("100% GPU", "100% CPU", "48%/52% CPU/GPU", "unknown").
func ollamaProcessor(size, vram int64) (int, string) {
	switch {
	case vram == 0:
		return 0, "100% CPU"
	case vram == size:
		return 100, "100% GPU"
	case size <= 0 || vram > size:
		return 0, "unknown"
	}

	cpu := int(math.Round(float64(size-vram) / float64(size) * 100))

	return 100 - cpu, fmt.Sprintf("%d%%/%d%% CPU/GPU", cpu, 100-cpu)
}

// ollamaShortDigest is a digest's first 12 hex characters, as `ollama list` shows it.
func ollamaShortDigest(d string) string {
	if _, hex, ok := strings.Cut(d, ":"); ok {
		d = hex
	}

	return d[:min(12, len(d))]
}

// listOllamaServers are the Ollama workloads in scope by namespace then name, each with its
// API read. Only the Deployment list failing is an error: a Service, pod or proxy call that
// fails is the server's own error.
func listOllamaServers(ctx context.Context, k *kubeClient, scope string) ([]ollamaServer, error) {
	servers, err := listOllamaWorkloads(ctx, k, scope)
	if err != nil || len(servers) == 0 {
		return servers, err
	}

	byNamespace := map[string][]int{}
	for i, s := range servers {
		byNamespace[s.namespace] = append(byNamespace[s.namespace], i)
	}

	var wg sync.WaitGroup

	for ns, idx := range byNamespace {
		wg.Go(func() {
			var services kubeList[ollamaServiceObject]
			servicesErr := getList(ctx, k, scopedPath("/api/v1", ns, "services"), &services)

			for _, i := range idx {
				locateOllama(ctx, k, &servers[i], services.Items, servicesErr)
			}
		})
	}

	wg.Wait()

	for i := range servers {
		wg.Go(func() { readOllamaAPI(ctx, k, &servers[i]) })
	}

	wg.Wait()

	return servers, nil
}

// listOllamaWorkloads are the Deployments and StatefulSets in scope running the catalog's
// Ollama image. A refused StatefulSet list is not an error when the Deployments were read.
func listOllamaWorkloads(ctx context.Context, k *kubeClient, scope string) ([]ollamaServer, error) {
	var deps, sets kubeList[ollamaWorkload]

	if err := getList(ctx, k, resourceRef{"apps", "v1", "deployments"}.path(scope), &deps); err != nil {
		return nil, err
	}

	if err := getList(ctx, k, resourceRef{"apps", "v1", "statefulsets"}.path(scope), &sets); err != nil {
		sets.Items = nil
	}

	catalog := loadAppCatalog()
	servers := []ollamaServer{}

	add := func(items []ollamaWorkload, kind, resource string) {
		for _, w := range items {
			gpu, port, ok := ollamaRunsIn(catalog, w)
			if !ok {
				continue
			}

			replicas := 1
			if w.Spec.Replicas != nil {
				replicas = *w.Spec.Replicas
			}

			servers = append(servers, ollamaServer{
				kind: kind, resource: resource, namespace: w.Metadata.Namespace, name: w.Metadata.Name,
				replicas: replicas, ready: w.Status.ReadyReplicas, gpu: gpu, podPort: port,
				labels: w.Spec.Template.Metadata.Labels, selector: w.Spec.Selector.MatchLabels,
			})
		}
	}

	add(deps.Items, "Deployment", "deployments")
	add(sets.Items, "StatefulSet", "statefulsets")

	slices.SortFunc(servers, func(a, b ollamaServer) int {
		return cmp.Or(strings.Compare(a.namespace, b.namespace), strings.Compare(a.name, b.name), strings.Compare(a.kind, b.kind))
	})

	return servers, nil
}

// ollamaRunsIn tells whether a container of w runs the catalog's Ollama image, with the GPUs
// its containers ask for (their limits summed) and the port its Ollama container listens on
// (the one named http, else 11434).
func ollamaRunsIn(catalog *appCatalog, w ollamaWorkload) (gpu, port int, found bool) {
	port = ollamaPort

	for _, c := range w.Spec.Template.Spec.Containers {
		if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "ollama" {
			found = true

			for _, p := range c.Ports {
				if p.Name == "http" && p.ContainerPort != 0 {
					port = p.ContainerPort
				}
			}
		}

		for _, res := range ollamaGPUResources {
			gpu += ollamaCount(c.Resources.Limits[res])
		}
	}

	return gpu, port, found
}

// ollamaCount reads a whole number written as a number or a string (a GPU limit, a port).
func ollamaCount(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}

	return 0
}

// locateOllama sets how s's API is reached: the port named http (or 11434) of the first
// Service selecting its pods, else the first ready pod's port through the pod proxy.
func locateOllama(ctx context.Context, k *kubeClient, s *ollamaServer, services []ollamaServiceObject, servicesErr error) {
	for _, svc := range services {
		if !selects(svc.Spec.Selector, s.labels) {
			continue
		}

		if port := ollamaServicePort(svc); port != 0 {
			s.service = &ollamaServiceRef{Name: svc.Metadata.Name, Port: port}
			s.target = ollamaTarget{
				prefix:   serviceProxyPath(s.namespace, url.PathEscape(svc.Metadata.Name)+":"+strconv.Itoa(port), ""),
				resource: "services/proxy",
			}

			return
		}
	}

	pod, err := ollamaReadyPod(ctx, k, s)

	switch {
	case err != nil && servicesErr != nil:
		s.err, s.forbidden = ollamaError(servicesErr, "services", "")
	case err != nil:
		s.err, s.forbidden = ollamaError(err, "pods", "")
	case pod == "":
		s.err = "no Service selects its pods and no pod is ready"
	default:
		s.target = ollamaTarget{
			prefix:   scopedPath("/api/v1", s.namespace, "pods") + "/" + url.PathEscape(pod) + ":" + strconv.Itoa(s.podPort) + "/proxy",
			resource: "pods/proxy",
		}
	}
}

// ollamaServicePort is the Service port Ollama answers on: the one named http, else the one
// numbered (or targeting) 11434; 0 when none.
func ollamaServicePort(svc ollamaServiceObject) int {
	for _, p := range svc.Spec.Ports {
		if p.Name == "http" || p.Port == ollamaPort || ollamaCount(p.TargetPort) == ollamaPort {
			return p.Port
		}
	}

	return 0
}

// ollamaReadyPod is the name of the first ready pod of s (by name), "" when none is ready.
func ollamaReadyPod(ctx context.Context, k *kubeClient, s *ollamaServer) (string, error) {
	if len(s.selector) == 0 {
		return "", nil
	}

	parts := make([]string, 0, len(s.selector))
	for _, key := range slices.Sorted(maps.Keys(s.selector)) {
		parts = append(parts, key+"="+s.selector[key])
	}

	var pods kubeList[ollamaPodObject]

	path := scopedPath("/api/v1", s.namespace, "pods") + "?labelSelector=" + url.QueryEscape(strings.Join(parts, ","))
	if err := getList(ctx, k, path, &pods); err != nil {
		return "", err
	}

	slices.SortFunc(pods.Items, func(a, b ollamaPodObject) int { return strings.Compare(a.Metadata.Name, b.Metadata.Name) })

	for _, p := range pods.Items {
		ready := slices.ContainsFunc(p.Status.Conditions, func(c ollamaPodCondition) bool {
			return c.Type == "Ready" && c.Status == "True"
		})
		if p.Status.Phase == "Running" && ready {
			return p.Metadata.Name, nil
		}
	}

	return "", nil
}

// readOllamaAPI reads s's version, models on disk and loaded models. The version answering
// makes the server reachable; a later call failing is its error, the rest still shows.
func readOllamaAPI(ctx context.Context, k *kubeClient, s *ollamaServer) {
	if s.target.prefix == "" {
		return
	}

	var version struct {
		Version string `json:"version"`
	}

	if err := ollamaGet(ctx, k, s.target.prefix, "/api/version", &version); err != nil {
		s.err, s.forbidden = ollamaError(err, s.target.resource, s.target.prefix)

		return
	}

	s.reachable, s.version = true, version.Version

	var (
		tags struct {
			Models []ollamaTag `json:"models"`
		}
		running struct {
			Models []ollamaRunning `json:"models"`
		}
		errs [2]error
		wg   sync.WaitGroup
	)

	wg.Go(func() { errs[0] = ollamaGet(ctx, k, s.target.prefix, "/api/tags", &tags) })
	wg.Go(func() { errs[1] = ollamaGet(ctx, k, s.target.prefix, "/api/ps", &running) })
	wg.Wait()

	s.tags, s.running = tags.Models, running.Models

	for _, err := range errs {
		if err != nil && s.err == "" {
			s.err, s.forbidden = ollamaError(err, s.target.resource, s.target.prefix)
		}
	}
}
