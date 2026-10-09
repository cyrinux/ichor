package ichorgo

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// "Can I?": which of the app's Kubernetes actions the cluster's credentials may run, asked
// with SelfSubjectAccessReviews before the tap rather than learnt from a 403 after it. An
// OIDC, EKS or AKS identity often has a narrow role. The answers are kept accessCacheTTL
// per cluster and namespace. A review that cannot be asked (an old or odd API server)
// never blocks: the action is offered, marked unknown, and the API server still decides.

const (
	ssarPath              = "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews"
	selfSubjectReviewPath = "/apis/authentication.k8s.io/v1/selfsubjectreviews"
	accessCacheTTL        = 3 * time.Minute
)

// ssarAttributes is a SelfSubjectAccessReview's resourceAttributes. Namespace "" is every
// namespace (or a cluster-scoped resource).
type ssarAttributes struct {
	Namespace   string `json:"namespace,omitempty"`
	Verb        string `json:"verb"`
	Group       string `json:"group,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Name        string `json:"name,omitempty"`
}

type ssarRequest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		ResourceAttributes ssarAttributes `json:"resourceAttributes"`
	} `json:"spec"`
}

type ssarStatus struct {
	Status struct {
		Allowed         bool   `json:"allowed"`
		Denied          bool   `json:"denied"`
		Reason          string `json:"reason"`
		EvaluationError string `json:"evaluationError"`
	} `json:"status"`
}

// kubeAccess is the answer for one action: when denied, what was refused (verb, resource as
// "resource/subresource", namespace "" for every namespace or cluster-wide) and the API
// server's reason. Unknown: the review could not be asked, so the action stays offered.
type kubeAccess struct {
	Allowed   bool   `json:"allowed"`
	Unknown   bool   `json:"unknown,omitempty"`
	Verb      string `json:"verb,omitempty"`
	Group     string `json:"group,omitempty"`
	Resource  string `json:"resource,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// kubeActionAccess is the access to every action of kubeActionChecks in Namespace.
type kubeActionAccess struct {
	Namespace string                `json:"namespace"`
	Actions   map[string]kubeAccess `json:"actions"`
}

// kubeActionChecks: each app action and the permissions it needs, all of them. namespaced
// attributes take the namespace asked about; the others keep theirs ("" = everywhere). An
// action on several kinds is asked per kind, on that kind's own resource: restartWorkload and
// scale are a Deployment's, fluxReconcile a Kustomization's (its suspend follows it), and
// each other kind has its own name.
var kubeActionChecks = map[string][]accessNeed{
	"restartWorkload":    {{ssarAttributes{Verb: "patch", Group: "apps", Resource: "deployments"}, true}},
	"restartStatefulSet": {{ssarAttributes{Verb: "patch", Group: "apps", Resource: "statefulsets"}, true}},
	"restartDaemonSet":   {{ssarAttributes{Verb: "patch", Group: "apps", Resource: "daemonsets"}, true}},
	"scale":              {{ssarAttributes{Verb: "patch", Group: "apps", Resource: "deployments", Subresource: "scale"}, true}},
	"scaleStatefulSet":   {{ssarAttributes{Verb: "patch", Group: "apps", Resource: "statefulsets", Subresource: "scale"}, true}},
	"deletePod":          {{ssarAttributes{Verb: "delete", Resource: "pods"}, true}},
	"execPod":            {{ssarAttributes{Verb: "create", Resource: "pods", Subresource: "exec"}, true}},
	"suspendCronJob":     {{ssarAttributes{Verb: "patch", Group: "batch", Resource: "cronjobs"}, true}},
	"triggerCronJob":     {{ssarAttributes{Verb: "create", Group: "batch", Resource: "jobs"}, true}},
	// Helm keeps its releases in Secrets: a rollback writes a new one.
	"helmRollback":                {{ssarAttributes{Verb: "create", Resource: "secrets"}, true}},
	"argoSync":                    {{ssarAttributes{Verb: "patch", Group: "argoproj.io", Resource: "applications"}, true}},
	"fluxReconcile":               {{ssarAttributes{Verb: "patch", Group: groupFluxKustomize, Resource: "kustomizations"}, true}},
	"fluxReconcileHelmRelease":    {{ssarAttributes{Verb: "patch", Group: groupFluxHelm, Resource: "helmreleases"}, true}},
	"fluxReconcileGitRepository":  {{ssarAttributes{Verb: "patch", Group: groupFluxSource, Resource: "gitrepositories"}, true}},
	"fluxReconcileOCIRepository":  {{ssarAttributes{Verb: "patch", Group: groupFluxSource, Resource: "ocirepositories"}, true}},
	"fluxReconcileHelmRepository": {{ssarAttributes{Verb: "patch", Group: groupFluxSource, Resource: "helmrepositories"}, true}},
	"fluxReconcileBucket":         {{ssarAttributes{Verb: "patch", Group: groupFluxSource, Resource: "buckets"}, true}},
	"cordonNode":                  {{ssarAttributes{Verb: "patch", Resource: "nodes"}, false}},
	// A drain cordons, then evicts the node's pods whatever their namespace.
	"drainNode": {
		{ssarAttributes{Verb: "patch", Resource: "nodes"}, false},
		{ssarAttributes{Verb: "create", Resource: "pods", Subresource: "eviction"}, false},
	},
}

type accessNeed struct {
	attrs      ssarAttributes
	namespaced bool
}

// KubeActionAccess says which of the app's Kubernetes actions the credentials may run in
// namespace ("" for cluster-wide): {"namespace", "actions": {name: {allowed, unknown, verb,
// group, resource, namespace, reason}}}, names restartWorkload (restartStatefulSet,
// restartDaemonSet), scale (scaleStatefulSet), deletePod, execPod, suspendCronJob,
// triggerCronJob, helmRollback, argoSync, fluxReconcile (fluxReconcileHelmRelease,
// fluxReconcileGitRepository, fluxReconcileOCIRepository, fluxReconcileHelmRepository,
// fluxReconcileBucket), cordonNode, drainNode. Answers are cached a few minutes.
// kubeServer: see KubePods.
func KubeActionAccess(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))
	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, func() kubeActionAccess { return allowAll(namespace) }, func(ctx context.Context, k *kubeClient) (kubeActionAccess, error) {
		return actionAccess.get(target.key()+"\x00"+namespace, func() (kubeActionAccess, error) {
			return readActionAccess(ctx, k, namespace), nil
		})
	})
}

// KubeCan asks whether the credentials may run verb on resource ("resource" or
// "resource/subresource") of group, in namespace ("" for every namespace or a
// cluster-scoped resource), on the object name ("" for any): {allowed, unknown, reason...}.
// For an action kubeActionChecks does not cover, e.g. saving an edited object.
func KubeCan(configYAML, contextName, kubeServer, verb, group, resource, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	resource, subresource, _ := strings.Cut(strings.TrimSpace(resource), "/")
	attrs := ssarAttributes{Namespace: namespace, Verb: strings.TrimSpace(verb), Group: strings.TrimSpace(group), Resource: resource, Subresource: subresource, Name: name}

	if attrs.Verb == "" || attrs.Resource == "" {
		return "", errors.New("no verb or resource given")
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() kubeAccess { return kubeAccess{Allowed: true} },
		func(ctx context.Context, k *kubeClient) (kubeAccess, error) {
			a, err := canDo(ctx, k, attrs)
			if err != nil {
				return kubeAccess{Allowed: true, Unknown: true}, nil
			}

			return a, nil
		})
}

// kubeWhoAmI is who the API server takes the credentials for (SelfSubjectReview, Kubernetes
// 1.28+). Unknown: the API server cannot say.
type kubeWhoAmI struct {
	User    string   `json:"user,omitempty"`
	Groups  []string `json:"groups,omitempty"`
	Unknown bool     `json:"unknown,omitempty"`
}

// KubeWhoAmI is who the cluster's credentials are to the API server: {user, groups, unknown}.
func KubeWhoAmI(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, demoKubeWhoAmI(target), func(ctx context.Context, k *kubeClient) (kubeWhoAmI, error) {
		return readWhoAmI(ctx, k), nil
	})
}

func readWhoAmI(ctx context.Context, k *kubeClient) kubeWhoAmI {
	body := map[string]any{"apiVersion": "authentication.k8s.io/v1", "kind": "SelfSubjectReview"}

	var review struct {
		Status struct {
			UserInfo struct {
				Username string   `json:"username"`
				Groups   []string `json:"groups"`
			} `json:"userInfo"`
		} `json:"status"`
	}

	if err := k.post(ctx, selfSubjectReviewPath, body, &review); err != nil || review.Status.UserInfo.Username == "" {
		return kubeWhoAmI{Unknown: true}
	}

	return kubeWhoAmI{User: review.Status.UserInfo.Username, Groups: review.Status.UserInfo.Groups}
}

// readActionAccess asks every review of kubeActionChecks for namespace, at once.
func readActionAccess(ctx context.Context, k *kubeClient, namespace string) kubeActionAccess {
	type answer struct {
		name   string
		access kubeAccess
	}

	answers := make(chan answer, len(kubeActionChecks))

	var wg sync.WaitGroup

	for name, needs := range kubeActionChecks {
		wg.Go(func() {
			answers <- answer{name, needsAccess(ctx, k, namespace, needs)}
		})
	}

	wg.Wait()
	close(answers)

	out := kubeActionAccess{Namespace: namespace, Actions: make(map[string]kubeAccess, len(kubeActionChecks))}
	for a := range answers {
		out.Actions[a.name] = a.access
	}

	return out
}

// needsAccess is allowed when every need is; else the first refused, with what it was.
func needsAccess(ctx context.Context, k *kubeClient, namespace string, needs []accessNeed) kubeAccess {
	unknown := false

	for _, n := range needs {
		attrs := n.attrs
		if n.namespaced {
			attrs.Namespace = namespace
		}

		a, err := canDo(ctx, k, attrs)
		if err != nil {
			unknown = true

			continue
		}

		if !a.Allowed {
			return a
		}
	}

	return kubeAccess{Allowed: true, Unknown: unknown}
}

// canDo asks one SelfSubjectAccessReview; an error means it could not be asked.
func canDo(ctx context.Context, k *kubeClient, attrs ssarAttributes) (kubeAccess, error) {
	review := ssarRequest{APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"}
	review.Spec.ResourceAttributes = attrs

	var status ssarStatus
	if err := k.post(ctx, ssarPath, review, &status); err != nil {
		return kubeAccess{}, err
	}

	if status.Status.Allowed && !status.Status.Denied {
		return kubeAccess{Allowed: true}, nil
	}

	resource := attrs.Resource
	if attrs.Subresource != "" {
		resource += "/" + attrs.Subresource
	}

	reason := status.Status.Reason
	if reason == "" {
		reason = status.Status.EvaluationError
	}

	return kubeAccess{Verb: attrs.Verb, Group: attrs.Group, Resource: resource, Namespace: attrs.Namespace, Reason: reason}, nil
}

// allowAll is the demo's answer: everything offered (the demo refuses the actions itself).
func allowAll(namespace string) kubeActionAccess {
	out := kubeActionAccess{Namespace: namespace, Actions: make(map[string]kubeAccess, len(kubeActionChecks))}
	for name := range kubeActionChecks {
		out.Actions[name] = kubeAccess{Allowed: true}
	}

	return out
}

func (a kubeActionAccess) hasUnknown() bool {
	for _, access := range a.Actions {
		if access.Unknown {
			return true
		}
	}

	return false
}

// accessCache keeps each complete answer for ttl.
type accessCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]accessEntry
}

type accessEntry struct {
	value kubeActionAccess
	at    time.Time
}

var actionAccess = newAccessCache(accessCacheTTL)

func newAccessCache(ttl time.Duration) *accessCache {
	return &accessCache{ttl: ttl, now: time.Now, entries: map[string]accessEntry{}}
}

func (c *accessCache) get(key string, fetch func() (kubeActionAccess, error)) (kubeActionAccess, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && c.now().Sub(e.at) < c.ttl {
		c.mu.Unlock()

		return e.value, nil
	}
	c.mu.Unlock()

	value, err := fetch()
	if err != nil {
		return kubeActionAccess{}, err
	}

	// A review that could not be asked may answer next time.
	if value.hasUnknown() {
		return value, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for k, e := range c.entries {
		if c.now().Sub(e.at) >= c.ttl {
			delete(c.entries, k)
		}
	}

	c.entries[key] = accessEntry{value, c.now()}

	return value, nil
}
