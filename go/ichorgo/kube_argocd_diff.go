package ichorgo

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The Argo CD diff: what syncing an Application now would change, like the Diff tab of the
// Argo CD UI, without an Argo CD token (plans/roadmap/devops/05-argocd-diff.md, phase 2).
// The application controller already computes it on every reconciliation and keeps it in
// Argo CD's Redis: per resource, the live object as Argo CD normalises it and the object as
// it would be after a sync (ignoreDifferences and Argo CD's own normalisation applied, Secret
// values hidden by the controller). The app reads that cache through a port-forward to the
// Redis pod (a SCAN and a GET, read only: nothing runs in the cluster, nothing is written),
// and renders each pair with the diff engine the Flux diff uses. No exec, no Git credentials.

const (
	argoDiffTimeout = 60 * time.Second
	// argoRedisPort is Redis' port in the argocd-redis and argocd-redis-ha-haproxy pods.
	argoRedisPort = 6379
	// argoRedisSecret holds Redis' password (key auth) when Argo CD enables it (its default
	// install does since 2.8).
	argoRedisSecret = "argocd-redis"
	// argoRedisKeyPrefix starts the cache key of an app's compared resources:
	// "app|managed-resources|<app>|<cache version>" (Argo CD's appstatecache).
	argoRedisKeyPrefix = "app|managed-resources|"
	// argoRedisMaxValue caps what the app reads for one key.
	argoRedisMaxValue = 64 << 20
	// argoRedisScanCount is how many keys one SCAN step looks at.
	argoRedisScanCount = 1000
)

// argoRedisSelectors find the Redis to read, in order: the HA proxy (it knows the primary),
// then the single Redis.
var argoRedisSelectors = []string{"app.kubernetes.io/name=argocd-redis-ha-haproxy", "app.kubernetes.io/name=argocd-redis"}

var (
	errArgoNoRedis   = errors.New("no Argo CD Redis pod to read the compared state from (argocd-redis or argocd-redis-ha-haproxy)")
	errArgoNotCached = errors.New("not compared by Argo CD yet, or its cache expired: refresh the app and try again")
)

// argoManagedResource is one entry of the controller's cache: the states as JSON text.
type argoManagedResource struct {
	Group               string `json:"group"`
	Kind                string `json:"kind"`
	Namespace           string `json:"namespace"`
	Name                string `json:"name"`
	TargetState         string `json:"targetState"`
	LiveState           string `json:"liveState"`
	NormalizedLiveState string `json:"normalizedLiveState"`
	PredictedLiveState  string `json:"predictedLiveState"`
	Hook                bool   `json:"hook"`
	Modified            bool   `json:"modified"`
}

// KubeArgoDiff compares the Argo CD Application namespace/name with what a sync would apply
// now (os:admin), as Argo CD itself compared it last: per object created, changed (with a
// unified diff), deleted (pruned), unchanged, ignored (a hook) or error. Secret values never
// show. kubeServer: see KubePods.
func KubeArgoDiff(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("application", namespace, name); err != nil {
		return "", err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return toJSON(demoArgoDiff(namespace, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), argoDiffTimeout)
	defer cancel()

	res, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (gitOpsDiff, error) {
		return diffArgoApp(ctx, k, k.dialPodPort, namespace, name)
	})
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// podDialer opens a stream to a pod's port (a port-forward); tests give a fake.
type podDialer func(ctx context.Context, namespace, pod string, port int) (io.ReadWriteCloser, error)

func diffArgoApp(ctx context.Context, k *kubeClient, dial podDialer, namespace, name string) (gitOpsDiff, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return gitOpsDiff{}, err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return gitOpsDiff{}, errors.New("the cluster runs no Argo CD")
	}

	var app argoObject
	if err := k.get(ctx, "/apis/"+groupArgo+"/"+version+"/namespaces/"+url.PathEscape(namespace)+"/applications/"+url.PathEscape(name), &app); err != nil {
		return gitOpsDiff{}, err
	}

	redis, err := findArgoRedis(ctx, k)
	if err != nil {
		return gitOpsDiff{}, err
	}

	password, err := argoRedisPassword(ctx, k, redis.Metadata.Namespace)
	if err != nil {
		return gitOpsDiff{}, err
	}

	stream, err := dial(ctx, redis.Metadata.Namespace, redis.Metadata.Name, argoRedisPort)
	if err != nil {
		return gitOpsDiff{}, err
	}
	defer stream.Close() //nolint:errcheck

	client := newRespClient(stream, argoRedisMaxValue)

	if err := client.auth(password); err != nil {
		return gitOpsDiff{}, fmt.Errorf("argocd-redis: %w", err)
	}

	raw, err := client.findValue(argoRedisKeyPrefix + argoInstanceName(app, redis.Metadata.Namespace) + "|")
	if err != nil {
		return gitOpsDiff{}, fmt.Errorf("argocd-redis: %w", err)
	}

	if raw == nil {
		return gitOpsDiff{}, errArgoNotCached
	}

	managed, err := decodeArgoManagedResources(raw)
	if err != nil {
		return gitOpsDiff{}, fmt.Errorf("argocd-redis: %w", err)
	}

	out := gitOpsDiff{
		Kind: "Application", Namespace: namespace, Name: name,
		Revision: argoDiffRevision(app), Applied: argoAppliedRevision(app),
		Warnings: []string{},
	}

	if !argoAutoPrune(app) && slices.ContainsFunc(managed, func(m argoManagedResource) bool {
		return !m.Hook && argoStateEmpty(m.TargetState) && !argoStateEmpty(m.LiveState)
	}) {
		out.Warnings = append(out.Warnings, "Objects marked deleted go only with a sync that prunes.")
	}

	masker := newKubeDiffMasker()
	for _, m := range managed {
		out.Resources = append(out.Resources, diffArgoManaged(m, masker))
	}

	sortDiffResources(out.Resources)

	return out, nil
}

// findArgoRedis is a running Redis pod of Argo CD; its namespace is the control plane's.
func findArgoRedis(ctx context.Context, k *kubeClient) (dsPod, error) {
	for _, selector := range argoRedisSelectors {
		pods, err := listDSPods(ctx, k, selector)
		if err != nil {
			return dsPod{}, err
		}

		if i := slices.IndexFunc(pods, func(p dsPod) bool { return p.Status.Phase == "Running" }); i >= 0 {
			return pods[i], nil
		}
	}

	return dsPod{}, errArgoNoRedis
}

// argoRedisPassword is Redis' password from the argocd-redis Secret, "" when Argo CD runs it
// without one.
func argoRedisPassword(ctx context.Context, k *kubeClient, namespace string) (string, error) {
	var secret struct {
		Data map[string]string `json:"data"`
	}

	err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/secrets/"+argoRedisSecret, &secret)
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}

		return "", fmt.Errorf("read the Redis password: %w", err)
	}

	raw, err := base64.StdEncoding.DecodeString(secret.Data["auth"])
	if err != nil {
		return "", fmt.Errorf("read the Redis password: %w", err)
	}

	return string(raw), nil
}

// argoInstanceName names the app in Argo CD's caches: its name in the control plane's
// namespace, "namespace_name" elsewhere (apps in any namespace).
func argoInstanceName(app argoObject, controlPlane string) string {
	if app.Metadata.Namespace != "" && app.Metadata.Namespace != controlPlane {
		return app.Metadata.Namespace + "_" + app.Metadata.Name
	}

	return app.Metadata.Name
}

// decodeArgoManagedResources reads the cached value: JSON, gzipped when Argo CD compresses
// its cache (its default).
func decodeArgoManagedResources(raw []byte) ([]argoManagedResource, error) {
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}

		raw, err = io.ReadAll(io.LimitReader(zr, argoRedisMaxValue))
		if err != nil {
			return nil, err
		}
	}

	var out []argoManagedResource
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unexpected cached state (%v)", err)
	}

	return out, nil
}

func argoStateEmpty(s string) bool {
	s = strings.TrimSpace(s)

	return s == "" || s == "null" || s == "{}"
}

// diffArgoManaged renders one cached entry: Argo CD's normalised live object against the
// one a sync would produce. Argo CD's own verdict (modified) decides changed or unchanged.
func diffArgoManaged(m argoManagedResource, masker *kubeDiffMasker) kubeDiffResource {
	r := kubeDiffResource{Group: m.Group, Kind: m.Kind, Namespace: m.Namespace, Name: m.Name}

	if m.Hook {
		r.Change = diffChangeIgnored

		return r
	}

	live, err := argoState(m.NormalizedLiveState, m.LiveState)
	if err != nil {
		r.Change, r.Error = diffChangeError, "unreadable live state: "+err.Error()

		return r
	}

	wanted, err := argoState(m.PredictedLiveState, m.TargetState)
	if err != nil {
		r.Change, r.Error = diffChangeError, "unreadable target state: "+err.Error()

		return r
	}

	if v, _ := firstObject(wanted, live)["apiVersion"].(string); v != "" {
		_, r.Version = splitAPIVersion(v)
	}

	switch {
	case live == nil && wanted == nil:
		r.Change = diffChangeUnchanged
	case live == nil:
		diffObjects(&r, nil, wanted, masker)
		r.Change = diffChangeCreated
	case wanted == nil:
		diffObjects(&r, live, nil, masker)
		r.Change = diffChangeDeleted
	case !m.Modified:
		r.Change = diffChangeUnchanged
	default:
		diffObjects(&r, live, wanted, masker)
		if r.Change == diffChangeUnchanged {
			// Argo CD sees a change the normalisation hides (a field it strips differently): show
			// the two states as they are.
			r.Change = diffChangeChanged
			r.Diff = unifiedDiff(diffRenderRaw(live), diffRenderRaw(wanted), "live", "wanted")
		}
	}

	return r
}

// argoState decodes the preferred JSON state, else the fallback; nil when both are empty.
func argoState(preferred, fallback string) (map[string]any, error) {
	for _, s := range []string{preferred, fallback} {
		if argoStateEmpty(s) {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return nil, err
		}

		return obj, nil
	}

	return nil, nil
}

func firstObject(a, b map[string]any) map[string]any {
	if a != nil {
		return a
	}

	return b
}

// argoDiffRevision is what Argo CD last compared: the revision(s) of status.sync, else the
// target revision(s).
func argoDiffRevision(app argoObject) string {
	if len(app.Status.Sync.Revisions) > 0 {
		return strings.Join(app.Status.Sync.Revisions, ", ")
	}

	if app.Status.Sync.Revision != "" {
		return app.Status.Sync.Revision
	}

	var targets []string
	for _, s := range argoSourcesOf(app.Spec) {
		targets = append(targets, s.TargetRevision)
	}

	return strings.Join(targets, ", ")
}

// argoAppliedRevision is the revision of the last deployment, "" before the first sync.
func argoAppliedRevision(app argoObject) string {
	if n := len(app.Status.History); n > 0 {
		h := app.Status.History[n-1]
		if h.Revision != "" {
			return h.Revision
		}

		return strings.Join(h.Revisions, ", ")
	}

	return ""
}

func argoAutoPrune(app argoObject) bool {
	p := app.Spec.SyncPolicy

	return p != nil && p.Automated != nil && p.Automated.Prune && (p.Automated.Enabled == nil || *p.Automated.Enabled)
}
