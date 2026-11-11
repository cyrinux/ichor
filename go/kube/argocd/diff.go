package argocd

import (
	"bytes"
	"cmp"
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

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubediff"
)

// The Argo CD diff: what syncing an Application now would change, like the Diff tab of the
// Argo CD UI, without an Argo CD token.
// The application controller already computes it on every reconciliation and keeps it in
// Argo CD's Redis: per resource, the live object as Argo CD normalises it and the object as
// it would be after a sync (ignoreDifferences and Argo CD's own normalisation applied, Secret
// values hidden by the controller). The app reads that cache through a port-forward to the
// Redis pod (a SCAN and a GET, read only: nothing runs in the cluster, nothing is written),
// and renders each pair with the diff engine the Flux diff uses. No exec, no Git credentials.

const (
	DiffTimeout = 60 * time.Second
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

// Diff compares the Application namespace/name with what a sync would apply now, as Argo CD
// itself compared it last (env.Dial reaches its Redis): per object created, changed (with a
// unified diff), deleted (pruned), unchanged, ignored (a hook) or error. Secret values never
// show.
func Diff(ctx context.Context, env Env, namespace, name string) (kubediff.Result, error) {
	k, dial := env.Client, env.Dial
	groups, err := kube.APIGroups(ctx, k)
	if err != nil {
		return kubediff.Result{}, err
	}

	version, ok := groups[Group]
	if !ok {
		return kubediff.Result{}, errors.New("the cluster runs no Argo CD")
	}

	var app Object
	if err := k.Get(ctx, "/apis/"+Group+"/"+version+"/namespaces/"+url.PathEscape(namespace)+"/applications/"+url.PathEscape(name), &app); err != nil {
		return kubediff.Result{}, err
	}

	redis, err := findArgoRedis(ctx, k)
	if err != nil {
		return kubediff.Result{}, err
	}

	password, err := argoRedisPassword(ctx, k, redis.Metadata.Namespace)
	if err != nil {
		return kubediff.Result{}, err
	}

	stream, err := dial(ctx, redis.Metadata.Namespace, redis.Metadata.Name, argoRedisPort)
	if err != nil {
		return kubediff.Result{}, err
	}
	defer stream.Close() //nolint:errcheck

	client := newRespClient(stream, argoRedisMaxValue)

	if err := client.auth(password); err != nil {
		return kubediff.Result{}, fmt.Errorf("argocd-redis: %w", err)
	}

	raw, err := client.findValue(argoRedisKeyPrefix + argoInstanceName(app, redis.Metadata.Namespace) + "|")
	if err != nil {
		return kubediff.Result{}, fmt.Errorf("argocd-redis: %w", err)
	}

	if raw == nil {
		return kubediff.Result{}, errArgoNotCached
	}

	managed, err := decodeArgoManagedResources(raw)
	if err != nil {
		return kubediff.Result{}, fmt.Errorf("argocd-redis: %w", err)
	}

	out := kubediff.Result{
		Kind: "Application", Namespace: namespace, Name: name,
		Revision: argoDiffRevision(app), Applied: argoAppliedRevision(app),
		Warnings: []string{},
	}

	if !argoAutoPrune(app) && slices.ContainsFunc(managed, func(m argoManagedResource) bool {
		return !m.Hook && argoStateEmpty(m.TargetState) && !argoStateEmpty(m.LiveState)
	}) {
		out.Warnings = append(out.Warnings, "Objects marked deleted go only with a sync that prunes.")
	}

	masker := kubediff.NewMasker()
	for _, m := range managed {
		out.Resources = append(out.Resources, diffArgoManaged(m, masker))
	}

	kubediff.Sort(out.Resources)

	return out, nil
}

// redisPod is the part of a pod findArgoRedis reads.
type redisPod struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// findArgoRedis is a running Redis pod of Argo CD, the first by namespace then name; its
// namespace is the control plane's.
func findArgoRedis(ctx context.Context, k kube.Client) (redisPod, error) {
	for _, selector := range argoRedisSelectors {
		var pods kube.List[redisPod]
		if err := kube.GetList(ctx, k, "/api/v1/pods?labelSelector="+url.QueryEscape(selector), &pods); err != nil {
			return redisPod{}, err
		}

		slices.SortFunc(pods.Items, func(a, b redisPod) int {
			return cmp.Or(strings.Compare(a.Metadata.Namespace, b.Metadata.Namespace), strings.Compare(a.Metadata.Name, b.Metadata.Name))
		})

		if i := slices.IndexFunc(pods.Items, func(p redisPod) bool { return p.Status.Phase == "Running" }); i >= 0 {
			return pods.Items[i], nil
		}
	}

	return redisPod{}, errArgoNoRedis
}

// argoRedisPassword is Redis' password from the argocd-redis Secret, "" when Argo CD runs it
// without one.
func argoRedisPassword(ctx context.Context, k kube.Client, namespace string) (string, error) {
	var secret struct {
		Data map[string]string `json:"data"`
	}

	err := k.Get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/secrets/"+argoRedisSecret, &secret)
	if err != nil {
		if kube.IsNotFound(err) {
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
func argoInstanceName(app Object, controlPlane string) string {
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
func diffArgoManaged(m argoManagedResource, masker *kubediff.Masker) kubediff.Resource {
	r := kubediff.Resource{Group: m.Group, Kind: m.Kind, Namespace: m.Namespace, Name: m.Name}

	if m.Hook {
		r.Change = kubediff.ChangeIgnored

		return r
	}

	live, err := argoState(m.NormalizedLiveState, m.LiveState)
	if err != nil {
		r.Change, r.Error = kubediff.ChangeError, "unreadable live state: "+err.Error()

		return r
	}

	wanted, err := argoState(m.PredictedLiveState, m.TargetState)
	if err != nil {
		r.Change, r.Error = kubediff.ChangeError, "unreadable target state: "+err.Error()

		return r
	}

	if v, _ := firstObject(wanted, live)["apiVersion"].(string); v != "" {
		_, r.Version = kubediff.SplitAPIVersion(v)
	}

	switch {
	case live == nil && wanted == nil:
		r.Change = kubediff.ChangeUnchanged
	case live == nil:
		kubediff.Objects(&r, nil, wanted, masker)
		r.Change = kubediff.ChangeCreated
	case wanted == nil:
		kubediff.Objects(&r, live, nil, masker)
		r.Change = kubediff.ChangeDeleted
	case !m.Modified:
		r.Change = kubediff.ChangeUnchanged
	default:
		kubediff.Objects(&r, live, wanted, masker)
		if r.Change == kubediff.ChangeUnchanged {
			// Argo CD sees a change the normalisation hides (a field it strips differently): show
			// the two states as they are.
			r.Change = kubediff.ChangeChanged
			r.Diff = kubediff.Unified(kubediff.RenderRaw(live), kubediff.RenderRaw(wanted), "live", "wanted")
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
func argoDiffRevision(app Object) string {
	if len(app.Status.Sync.Revisions) > 0 {
		return strings.Join(app.Status.Sync.Revisions, ", ")
	}

	if app.Status.Sync.Revision != "" {
		return app.Status.Sync.Revision
	}

	var targets []string
	for _, s := range SourcesOf(app.Spec) {
		targets = append(targets, s.TargetRevision)
	}

	return strings.Join(targets, ", ")
}

// argoAppliedRevision is the revision of the last deployment, "" before the first sync.
func argoAppliedRevision(app Object) string {
	if n := len(app.Status.History); n > 0 {
		h := app.Status.History[n-1]
		if h.Revision != "" {
			return h.Revision
		}

		return strings.Join(h.Revisions, ", ")
	}

	return ""
}

func argoAutoPrune(app Object) bool {
	p := app.Spec.SyncPolicy

	return p != nil && p.Automated != nil && p.Automated.Prune && (p.Automated.Enabled == nil || *p.Automated.Enabled)
}
