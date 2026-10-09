package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// `helm rollback` without the Helm SDK. Like Helm, it records revision N+1 as
// pending-rollback (the lock), moves every object from the current revision's manifest to the
// target's (created, three-way patched, deleted unless kept), then marks N+1 deployed and the
// current one superseded, so the helm CLI agrees afterwards. Unlike the CLI it shows the plan
// first, dry-run on the API server, and refuses what it would get wrong: a chart with
// rollback hooks, another operation in progress, an object some other release owns. A release
// the Flux helm-controller manages is suspended first, or Flux would upgrade it straight back.

const (
	helmStatusDeployed        = "deployed"
	helmStatusSuperseded      = "superseded"
	helmStatusFailed          = "failed"
	helmStatusPendingRollback = "pending-rollback"

	helmChangeCreate = "create"
	helmChangeUpdate = "update"
	helmChangeDelete = "delete"
	helmChangeKeep   = "keep" // helm.sh/resource-policy: keep, left in place as Helm does

	helmLiveParallel = 4
	helmFieldManager = "ichor"

	// helmRollbackTimeout bounds a plan or a rollback: discovery, a read per object and a dry
	// run (then the change) per change, one after the other.
	helmRollbackTimeout = 3 * time.Minute
	// helmRecordTimeout is left for recording how a rollback ended, even past the first.
	helmRecordTimeout = 30 * time.Second
)

// helmRollbackPlan is what a rollback would do, as JSON for the confirmation.
type helmRollbackPlan struct {
	Namespace string `json:"namespace"` // where the release's Secrets are
	// ReleaseNamespace is where its objects go (a Flux HelmRelease's targetNamespace may differ
	// from where it stores the release).
	ReleaseNamespace string       `json:"releaseNamespace"`
	Name             string       `json:"name"`
	From             int          `json:"from"` // the current revision
	To               int          `json:"to"`   // the revision rolled back to
	FromChart        string       `json:"fromChart"`
	ToChart          string       `json:"toChart"`
	FromAppVersion   string       `json:"fromAppVersion"`
	ToAppVersion     string       `json:"toAppVersion"`
	Changes          []helmChange `json:"changes"`
	Unchanged        int          `json:"unchanged"` // objects already as the target renders them
	// FluxOwner is the HelmRelease ("namespace/name") whose helm-controller manages the
	// release: the rollback suspends it. "" for a release installed by hand.
	FluxOwner string `json:"fluxOwner"`
	// Blockers say why the rollback cannot run; empty when it can.
	Blockers []string `json:"blockers"`
}

// helmChange is one object the rollback creates, updates, deletes or keeps. Error is why the
// API server refused its dry run.
type helmChange struct {
	Action    string `json:"action"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Error     string `json:"error"`
}

// helmStored is a revision as stored: its Secret, the release as a generic map (rewritten for
// the new revision, numbers kept exact) and the fields read here.
type helmStored struct {
	ref     helmSecretRef
	payload map[string]any
	rel     helmRelease
}

// helmStep is a change ready to run.
type helmStep struct {
	action   string
	obj      helmObject
	original map[string]any // the current revision's rendering, nil for an object it lacks
	patch    map[string]any // update only
}

type helmRollback struct {
	plan    helmRollbackPlan
	refs    []helmSecretRef // every revision, newest first
	current helmStored
	target  helmStored
	steps   []helmStep
}

// KubeHelmRollbackPlan says what rolling the Helm release namespace/name back to revision
// would do, without doing it (os:admin): every change is dry-run on the API server. revision
// 0 is the previous one, as for `helm rollback`. JSON helmRollbackPlan. kubeServer: see
// KubePods.
func KubeHelmRollbackPlan(configYAML, contextName, kubeServer, namespace, name string, revision int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateHelmRollback(namespace, name, revision); err != nil {
		return "", err
	}

	demo := func() helmRollbackPlan { return demoHelmRollbackPlan(namespace, name, revision) }

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return toJSON(demo())
	}

	ctx, cancel := context.WithTimeout(context.Background(), helmRollbackTimeout)
	defer cancel()

	plan, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (helmRollbackPlan, error) {
		rb, err := prepareHelmRollback(ctx, k, namespace, name, revision)
		if err != nil {
			return helmRollbackPlan{}, err
		}

		return rb.plan, nil
	})
	if err != nil {
		return "", err
	}

	return toJSON(plan)
}

// KubeHelmRollback rolls the Helm release namespace/name back to revision (0: the previous
// one), after the same checks and dry run as KubeHelmRollbackPlan (os:admin). A HelmRelease
// managing it is suspended first. On failure the new revision is recorded as failed, as
// Helm does, and the error names the object that stopped it.
func KubeHelmRollback(configYAML, contextName, kubeServer, namespace, name string, revision int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "helm-rollback", Namespace: namespace, Object: "HelmRelease/" + name, Params: fmt.Sprintf("revision=%d", revision)})

	if err := validateHelmRollback(namespace, name, revision); err != nil {
		return err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return errDemoUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), helmRollbackTimeout)
	defer cancel()

	return kubeMutationError(kubeDo(ctx, target, func(ctx context.Context, k *kubeClient) error {
		rb, err := prepareHelmRollback(ctx, k, namespace, name, revision)
		if err != nil {
			return err
		}

		return runHelmRollback(ctx, k, rb, time.Now())
	}))
}

func validateHelmRollback(namespace, name string, revision int) error {
	if revision < 0 {
		return fmt.Errorf("invalid revision %d", revision)
	}

	return validateKubeName("Helm release", namespace, name)
}

// prepareHelmRollback reads both revisions and the live objects, and dry-runs every change.
func prepareHelmRollback(ctx context.Context, k *kubeClient, namespace, name string, revision int) (*helmRollback, error) {
	refs, err := helmSecretRefs(ctx, k, namespace, name)
	if err != nil {
		return nil, err
	}

	refs = helmHistoryRefs(refs, name)
	if len(refs) == 0 {
		return nil, fmt.Errorf("no Helm release %s/%s", namespace, name)
	}

	targetRef, err := helmTargetRef(refs, revision)
	if err != nil {
		return nil, err
	}

	rb := &helmRollback{refs: refs}

	if rb.current, err = readHelmStored(ctx, k, refs[0]); err != nil {
		return nil, fmt.Errorf("revision %d: %w", refs[0].revision, err)
	}

	if rb.target, err = readHelmStored(ctx, k, targetRef); err != nil {
		return nil, fmt.Errorf("revision %d: %w", targetRef.revision, err)
	}

	cur, tgt := rb.current.rel, rb.target.rel
	releaseNS := cmp.Or(cur.Namespace, namespace)
	rb.plan = helmRollbackPlan{
		Namespace: namespace, ReleaseNamespace: releaseNS, Name: name, From: refs[0].revision, To: targetRef.revision,
		FromChart: chartLabel(cur), ToChart: chartLabel(tgt),
		FromAppVersion: cur.Chart.Metadata.AppVersion, ToAppVersion: tgt.Chart.Metadata.AppVersion,
		Changes: []helmChange{}, Blockers: helmRollbackBlockers(rb.current, rb.target),
	}

	kinds := newKubeKinds(k)

	curObjs, err := helmObjects(ctx, kinds, cur.Manifest, releaseNS)
	if err == nil {
		var tgtObjs []helmObject

		if tgtObjs, err = helmObjects(ctx, kinds, tgt.Manifest, releaseNS); err == nil {
			rb.plan.FluxOwner = helmFluxOwner(namespace, curObjs, tgtObjs)
			err = rb.planSteps(ctx, k, curObjs, tgtObjs)
		}
	}

	if err != nil {
		rb.plan.Blockers = append(rb.plan.Blockers, err.Error())
	}

	return rb, nil
}

// helmHistoryRefs keeps one Secret per revision of release name (the one Helm named, if a
// copy exists), newest first.
func helmHistoryRefs(refs []helmSecretRef, name string) []helmSecretRef {
	byRevision := map[int]helmSecretRef{}

	for _, ref := range refs {
		if ref.name != name {
			continue
		}

		if _, seen := byRevision[ref.revision]; seen && ref.secret != helmSecretName(name, ref.revision) {
			continue
		}

		byRevision[ref.revision] = ref
	}

	out := make([]helmSecretRef, 0, len(byRevision))
	for _, ref := range byRevision {
		out = append(out, ref)
	}

	slices.SortFunc(out, func(a, b helmSecretRef) int { return cmp.Compare(b.revision, a.revision) })

	return out
}

// helmSecretName is the Secret Helm 3 stores a revision in.
func helmSecretName(name string, revision int) string {
	return "sh.helm.release.v1." + name + ".v" + strconv.Itoa(revision)
}

// helmTargetRef is the revision to roll back to; 0 means the one before the current.
func helmTargetRef(refs []helmSecretRef, revision int) (helmSecretRef, error) {
	if revision == 0 {
		if revision = refs[0].revision - 1; revision < 1 {
			return helmSecretRef{}, errors.New("there is no earlier revision to roll back to")
		}
	}

	if revision == refs[0].revision {
		return helmSecretRef{}, fmt.Errorf("revision %d is the current one", revision)
	}

	for _, ref := range refs {
		if ref.revision == revision {
			return ref, nil
		}
	}

	return helmSecretRef{}, fmt.Errorf("revision %d is not kept any more (Helm keeps the last ones only)", revision)
}

func chartLabel(rel helmRelease) string {
	return strings.TrimSuffix(rel.Chart.Metadata.Name+"-"+rel.Chart.Metadata.Version, "-")
}

// helmRollbackBlockers are the reasons a rollback must not run whatever its objects.
func helmRollbackBlockers(current, target helmStored) []string {
	blockers := []string{}

	if status := cmp.Or(current.rel.Info.Status, current.ref.status); strings.HasPrefix(status, "pending-") {
		blockers = append(blockers, fmt.Sprintf("another operation is in progress (revision %d is %s)", current.ref.revision, status))
	}

	hooks := []string{}

	for _, h := range target.rel.Hooks {
		if slices.Contains(h.Events, "pre-rollback") || slices.Contains(h.Events, "post-rollback") {
			hooks = append(hooks, h.Kind+" "+h.Name)
		}
	}

	if len(hooks) > 0 {
		blockers = append(blockers, "the chart runs hooks on rollback ("+strings.Join(hooks, ", ")+"): use helm rollback, which runs them")
	}

	return blockers
}

// helmFluxOwner is the HelmRelease whose helm-controller rendered these objects (it labels
// each one), "namespace/name", or "".
func helmFluxOwner(namespace string, lists ...[]helmObject) string {
	for _, list := range lists {
		for _, o := range list {
			if name := o.objectLabel(fluxHelmLabelName); name != "" {
				return cmp.Or(o.objectLabel(fluxHelmLabelNamespace), namespace) + "/" + name
			}
		}
	}

	return ""
}

// planSteps compares both renderings with the live objects, keeps the changes that change
// something and dry-runs them, in Helm's order: creations and updates by kind, deletions in
// reverse.
func (rb *helmRollback) planSteps(ctx context.Context, k *kubeClient, current, target []helmObject) error {
	slices.SortStableFunc(target, func(a, b helmObject) int { return helmKindRank(a.Kind) - helmKindRank(b.Kind) })
	slices.SortStableFunc(current, func(a, b helmObject) int { return helmKindRank(b.Kind) - helmKindRank(a.Kind) })

	for _, list := range [][]helmObject{current, target} {
		for _, o := range list {
			withHelmOwnership(o.Body, rb.plan.Name, rb.plan.ReleaseNamespace)
		}
	}

	byKey := map[string]helmObject{}
	for _, o := range current {
		byKey[o.key()] = o
	}

	inTarget := map[string]bool{}
	steps := []helmStep{}

	for _, o := range target {
		inTarget[o.key()] = true

		if was, ok := byKey[o.key()]; ok {
			steps = append(steps, helmStep{action: helmChangeUpdate, obj: o, original: was.Body})
		} else {
			steps = append(steps, helmStep{action: helmChangeCreate, obj: o})
		}
	}

	for _, o := range current {
		if inTarget[o.key()] {
			continue
		}

		if o.annotation(helmAnnotationPolicy) == "keep" {
			rb.plan.Changes = append(rb.plan.Changes, helmChange{Action: helmChangeKeep, Kind: o.Kind, Namespace: o.Namespace, Name: o.Name})

			continue
		}

		steps = append(steps, helmStep{action: helmChangeDelete, obj: o})
	}

	steps, err := rb.withLive(ctx, k, steps)
	if err != nil {
		return err
	}

	failed := 0

	for _, s := range steps {
		change := helmChange{Action: s.action, Kind: s.obj.Kind, Namespace: s.obj.Namespace, Name: s.obj.Name}
		rb.plan.Changes = append(rb.plan.Changes, change)

		if s.action == helmChangeKeep {
			continue
		}

		if err := applyHelmStep(ctx, k, s, true); err != nil {
			rb.plan.Changes[len(rb.plan.Changes)-1].Error = kubeError(err).Error()
			failed++
		}

		rb.steps = append(rb.steps, s)
	}

	if failed > 0 {
		return fmt.Errorf("the API server refuses %d of the changes (see each one)", failed)
	}

	return nil
}

// withLive reads each object as it is now and settles its step: an update whose object is
// gone becomes a creation, a creation whose object exists becomes an update when the release
// owns it (refused otherwise), an update with nothing to change and a deletion of what is
// already gone drop out.
func (rb *helmRollback) withLive(ctx context.Context, k *kubeClient, steps []helmStep) ([]helmStep, error) {
	settled := make([]*helmStep, len(steps))
	errs := make([]error, len(steps))
	slots := make(chan struct{}, helmLiveParallel)

	var wg sync.WaitGroup

	for i, s := range steps {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			settled[i], errs[i] = rb.settle(ctx, k, s)
		})
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	out := []helmStep{}

	for _, s := range settled {
		if s != nil {
			out = append(out, *s)
		}
	}

	rb.plan.Unchanged = len(steps) - len(out) - rb.gone(settled, steps)

	return out, nil
}

// gone counts the deletions dropped because the object no longer exists.
func (rb *helmRollback) gone(settled []*helmStep, steps []helmStep) int {
	n := 0

	for i, s := range settled {
		if s == nil && steps[i].action == helmChangeDelete {
			n++
		}
	}

	return n
}

func (rb *helmRollback) settle(ctx context.Context, k *kubeClient, s helmStep) (*helmStep, error) {
	var live map[string]any

	err := k.get(ctx, s.obj.Path, &live)

	switch {
	case isNotFound(err):
		if s.action == helmChangeDelete {
			return nil, nil
		}

		s.action, s.original = helmChangeCreate, nil

		return &s, nil
	case err != nil:
		return nil, fmt.Errorf("%s: %w", s.obj.label(), err)
	case s.action == helmChangeDelete:
		// Helm reads the policy on the live object: one annotated since stays.
		if (helmObject{Body: live}).annotation(helmAnnotationPolicy) == "keep" {
			s.action = helmChangeKeep
		}

		return &s, nil
	}

	if s.action == helmChangeCreate {
		if !rb.owns(live) {
			return nil, fmt.Errorf("%s already exists and does not belong to release %s", s.obj.label(), rb.plan.Name)
		}

		s.action = helmChangeUpdate
	}

	s.patch = threeWayMergePatch(s.original, s.obj.Body, live)
	if len(s.patch) == 0 {
		return nil, nil
	}

	return &s, nil
}

// withHelmOwnership adds what Helm stamps on every object it installs: the release's name
// and namespace annotations and the managed-by label. Without them the next helm upgrade
// would refuse the objects as not its own.
func withHelmOwnership(obj map[string]any, release, namespace string) {
	meta, _ := obj["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		obj["metadata"] = meta
	}

	set := func(field, key, value string) {
		m, _ := meta[field].(map[string]any)
		if m == nil {
			m = map[string]any{}
			meta[field] = m
		}

		m[key] = value
	}

	set("annotations", helmAnnotationRelease, release)
	set("annotations", helmAnnotationReleaseNS, namespace)
	set("labels", "app.kubernetes.io/managed-by", "Helm")
}

// owns tells whether a live object carries this release's ownership annotations.
func (rb *helmRollback) owns(live map[string]any) bool {
	o := helmObject{Body: live}

	return o.objectLabel("app.kubernetes.io/managed-by") == "Helm" &&
		o.annotation(helmAnnotationRelease) == rb.plan.Name && o.annotation(helmAnnotationReleaseNS) == rb.plan.ReleaseNamespace
}

// applyHelmStep runs one change, or only checks it with dryRun.
func applyHelmStep(ctx context.Context, k *kubeClient, s helmStep, dryRun bool) error {
	q := url.Values{"fieldManager": {helmFieldManager}}
	if dryRun {
		q.Set("dryRun", "All")
	}

	switch s.action {
	case helmChangeCreate:
		body, err := json.Marshal(s.obj.Body)
		if err != nil {
			return err
		}

		collection := s.obj.Path[:strings.LastIndexByte(s.obj.Path, '/')]

		return k.do(ctx, http.MethodPost, collection+"?"+q.Encode(), "application/json", body, nil)
	case helmChangeUpdate:
		body, err := json.Marshal(s.patch)
		if err != nil {
			return err
		}

		return k.do(ctx, http.MethodPatch, s.obj.Path+"?"+q.Encode(), "application/merge-patch+json", body, nil)
	default:
		q.Del("fieldManager")
		q.Set("propagationPolicy", "Background")

		err := k.do(ctx, http.MethodDelete, s.obj.Path+"?"+q.Encode(), "application/json", nil, nil)
		if isNotFound(err) {
			return nil
		}

		return err
	}
}

// runHelmRollback runs a prepared rollback: Flux suspended, revision N+1 recorded as
// pending-rollback, every change made, then N+1 deployed and the others superseded.
func runHelmRollback(ctx context.Context, k *kubeClient, rb *helmRollback, now time.Time) error {
	if len(rb.plan.Blockers) > 0 {
		return errors.New(strings.Join(rb.plan.Blockers, "; "))
	}

	if owner := rb.plan.FluxOwner; owner != "" {
		ns, name, _ := strings.Cut(owner, "/")
		if err := fluxAction(ctx, k, "HelmRelease", ns, name, fluxActionSuspend, now); err != nil {
			return fmt.Errorf("suspend the HelmRelease %s first: %w", owner, err)
		}
	}

	next := rb.plan.From + 1
	description := fmt.Sprintf("Rollback to %d", rb.plan.To)

	if err := createHelmRevision(ctx, k, rb, next, description, now); err != nil {
		return fmt.Errorf("record revision %d: %w", next, err)
	}

	// How it ended is recorded even when the rollback ran out of time: a revision left
	// pending-rollback would lock the release.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), helmRecordTimeout)
	defer cancel()

	ns, nextSecret := rb.plan.Namespace, helmSecretName(rb.plan.Name, next)

	for _, s := range rb.steps {
		if err := applyHelmStep(ctx, k, s, false); err != nil {
			err = fmt.Errorf("%s %s: %w", s.action, s.obj.label(), kubeError(err))
			failure := fmt.Sprintf("Rollback %q failed: %s", rb.plan.Name, err)

			// As Helm: the current revision superseded, the new one failed.
			return errors.Join(
				fmt.Errorf("rollback stopped, revision %d recorded as failed: %w", next, err),
				setHelmStatus(record, k, ns, rb.current.ref.secret, helmStatusSuperseded, "", now),
				setHelmStatus(record, k, ns, nextSecret, helmStatusFailed, failure, now),
			)
		}
	}

	// As Helm: every deployed revision superseded (a failed current one stays failed).
	errs := []error{}

	for _, ref := range rb.refs {
		if ref.status == helmStatusDeployed {
			errs = append(errs, setHelmStatus(record, k, ns, ref.secret, helmStatusSuperseded, "", now))
		}
	}

	return errors.Join(append(errs, setHelmStatus(record, k, ns, nextSecret, helmStatusDeployed, description, now))...)
}
