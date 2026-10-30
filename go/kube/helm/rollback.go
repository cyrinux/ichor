package helm

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

	"github.com/cyrinux/ichor/go/kube"
)

// `helm rollback` without the Helm SDK. Like Helm, it records revision N+1 as
// pending-rollback (the lock), moves every object from the current revision's manifest to the
// target's (created, three-way patched, deleted unless kept), then marks N+1 deployed and the
// current one superseded, so the helm CLI agrees afterwards. Unlike the CLI it shows the plan
// first, dry-run on the API server, and refuses what it would get wrong: another operation in
// progress, an object some other release owns, a change or a hook the API server refuses. The
// chart's pre-rollback hooks run before the changes and its post-rollback hooks after them
// (hooks.go). A release the Flux helm-controller manages is suspended
// first, or Flux would upgrade it straight back.

const (
	StatusDeployed        = "deployed"
	StatusSuperseded      = "superseded"
	StatusFailed          = "failed"
	StatusPendingRollback = "pending-rollback"

	ChangeCreate = "create"
	ChangeUpdate = "update"
	ChangeDelete = "delete"
	ChangeKeep   = "keep" // helm.sh/resource-policy: keep, left in place as Helm does
	// ChangeReplace is an update sent as a PUT of the whole object (the force option),
	// as `helm rollback --force` does.
	ChangeReplace = "replace"

	liveParallel = 4
	fieldManager = "ichor"

	// PlanTimeout bounds a plan: discovery, a read per object and a dry run per change
	// and per hook, one after the other.
	PlanTimeout = 3 * time.Minute
	// recordTimeout is left for recording how a rollback ended, even past the first.
	recordTimeout = 30 * time.Second
)

// Plan is what a rollback would do, as JSON for the confirmation.
type Plan struct {
	Namespace string `json:"namespace"` // where the release's Secrets are
	// ReleaseNamespace is where its objects go (a Flux HelmRelease's targetNamespace may differ
	// from where it stores the release).
	ReleaseNamespace string   `json:"releaseNamespace"`
	Name             string   `json:"name"`
	From             int      `json:"from"` // the current revision
	To               int      `json:"to"`   // the revision rolled back to
	FromChart        string   `json:"fromChart"`
	ToChart          string   `json:"toChart"`
	FromAppVersion   string   `json:"fromAppVersion"`
	ToAppVersion     string   `json:"toAppVersion"`
	Changes          []Change `json:"changes"`
	Unchanged        int      `json:"unchanged"` // objects already as the target renders them
	// FluxOwner is the HelmRelease ("namespace/name") whose helm-controller manages the
	// release: the rollback suspends it. "" for a release installed by hand.
	FluxOwner string `json:"fluxOwner"`
	// Hooks are the chart's pre-rollback then post-rollback hooks, each event by weight then
	// name: the order they run in.
	Hooks []Hook `json:"hooks"`
	// Blockers say why the rollback cannot run; empty when it can.
	Blockers []string `json:"blockers"`
	// Force: updates are replaced (PUT), not patched.
	Force bool `json:"force"`
	// HistoryMax is how many revisions the rollback keeps, 0 for all of them.
	HistoryMax int `json:"historyMax"`
	// Prune are the revisions whose Secret the rollback deletes once it is deployed, oldest
	// first.
	Prune []int `json:"prune"`
	// Workloads counts the Deployments, StatefulSets and DaemonSets of the target revision:
	// what Follow can wait for.
	Workloads int `json:"workloads"`
}

// Change is one object the rollback creates, updates, deletes or keeps. Error is why the
// API server refused its dry run.
type Change struct {
	Action    string `json:"action"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Error     string `json:"error"`
}

// stored is a revision as stored: its Secret, the release as a generic map (rewritten for
// the new revision, numbers kept exact) and the fields read here.
type stored struct {
	ref     SecretRef
	payload map[string]any
	rel     Release
}

// rollbackStep is a change ready to run.
type rollbackStep struct {
	action   string
	obj      object
	original map[string]any // the current revision's rendering, nil for an object it lacks
	patch    map[string]any // update only
	body     map[string]any // replace only: the target with the live resourceVersion
}

type rollback struct {
	plan    Plan
	opts    Options
	refs    []SecretRef // every revision, newest first
	prune   []SecretRef // the revisions deleted once the rollback is deployed
	current stored
	target  stored
	steps   []rollbackStep
	// The hooks run before and after the steps, never part of them.
	preHooks, postHooks []hookStep
}

// PlanRollback says what rolling the release namespace/name back to revision would do, without
// doing it: every change is dry-run on the API server. revision 0 is the previous one, as for
// `helm rollback`.
func PlanRollback(ctx context.Context, k Env, namespace, name string, revision int, opts Options) (Plan, error) {
	rb, err := prepareRollback(ctx, k, namespace, name, revision, opts)
	if err != nil {
		return Plan{}, err
	}

	return rb.plan, nil
}

// Outcome is what stays out of the error of a rollback, because the caller's Kubernetes call
// wrapper would reduce it to the API error inside it.
type Outcome struct {
	// Prune: the rollback is done but old revisions could not all be deleted; its message
	// starts with PrunePrefix, which tells the apps so.
	Prune error
	// Hook: a hook failed; its message names the hook, and the revision recorded.
	Hook error
}

// Rollback plans the rollback (PlanTimeout at most), then runs it under a deadline sized from
// its hooks.
func Rollback(ctx context.Context, k Env, namespace, name string, revision int, opts Options) (Outcome, error) {
	planCtx, cancelPlan := context.WithTimeout(ctx, PlanTimeout)
	rb, err := prepareRollback(planCtx, k, namespace, name, revision, opts)

	cancelPlan()

	if err != nil {
		return Outcome{}, err
	}

	runCtx, cancelRun := context.WithTimeoutCause(ctx, rollbackTimeout(len(rb.preHooks)+len(rb.postHooks)), errRollbackTimeout)
	defer cancelRun()

	pruneErr, runErr := runRollback(runCtx, k, rb, time.Now())

	if failed := (*hookError)(nil); errors.As(runErr, &failed) {
		return Outcome{Prune: pruneErr, Hook: runErr}, nil
	}

	return Outcome{Prune: pruneErr}, runErr
}

// rollbackTimeout bounds the run of a planned rollback with hooks hooks: the plan's work
// again and the changes, then per hook the wait for before-hook-creation's delete and the
// wait for the hook itself.
func rollbackTimeout(hooks int) time.Duration {
	return PlanTimeout + time.Duration(hooks)*2*hookTimeout
}

// prepareRollback reads both revisions and the live objects, and dry-runs every change.
func prepareRollback(ctx context.Context, k Env, namespace, name string, revision int, opts Options) (*rollback, error) {
	refs, err := SecretRefs(ctx, k.Client, namespace, name)
	if err != nil {
		return nil, err
	}

	refs = historyRefs(refs, name)
	if len(refs) == 0 {
		return nil, fmt.Errorf("no Helm release %s/%s", namespace, name)
	}

	targetRef, err := pickTargetRef(refs, revision)
	if err != nil {
		return nil, err
	}

	rb := &rollback{refs: refs, opts: opts, prune: pruneRefs(refs, opts.HistoryMax)}

	if rb.current, err = readStored(ctx, k.Client, refs[0]); err != nil {
		return nil, fmt.Errorf("revision %d: %w", refs[0].Revision, err)
	}

	if rb.target, err = readStored(ctx, k.Client, targetRef); err != nil {
		return nil, fmt.Errorf("revision %d: %w", targetRef.Revision, err)
	}

	cur, tgt := rb.current.rel, rb.target.rel
	releaseNS := cmp.Or(cur.Namespace, namespace)
	rb.plan = Plan{
		Namespace: namespace, ReleaseNamespace: releaseNS, Name: name, From: refs[0].Revision, To: targetRef.Revision,
		FromChart: chartLabel(cur), ToChart: chartLabel(tgt),
		FromAppVersion: cur.Chart.Metadata.AppVersion, ToAppVersion: tgt.Chart.Metadata.AppVersion,
		Changes: []Change{}, Hooks: []Hook{}, Blockers: rollbackBlockers(rb.current),
		Force: opts.Force, HistoryMax: opts.HistoryMax, Prune: revisions(rb.prune),
	}

	kinds := newKindResolver(k.Client)

	curObjs, err := objects(ctx, kinds, cur.Manifest, releaseNS)
	if err == nil {
		var tgtObjs []object

		if tgtObjs, err = objects(ctx, kinds, tgt.Manifest, releaseNS); err == nil {
			rb.plan.Workloads = len(workloadObjects(tgtObjs))
			rb.plan.FluxOwner = fluxOwner(namespace, curObjs, tgtObjs)
			err = rb.planSteps(ctx, k, curObjs, tgtObjs)
		}
	}

	if err != nil {
		rb.plan.Blockers = append(rb.plan.Blockers, err.Error())
	}

	if err := rb.planHooks(ctx, k, kinds, releaseNS); err != nil {
		rb.plan.Blockers = append(rb.plan.Blockers, err.Error())
	}

	return rb, nil
}

// historyRefs keeps one Secret per revision of release name (the one Helm named, if a
// copy exists), newest first.
func historyRefs(refs []SecretRef, name string) []SecretRef {
	byRevision := map[int]SecretRef{}

	for _, ref := range refs {
		if ref.Name != name {
			continue
		}

		if _, seen := byRevision[ref.Revision]; seen && ref.Secret != secretName(name, ref.Revision) {
			continue
		}

		byRevision[ref.Revision] = ref
	}

	out := make([]SecretRef, 0, len(byRevision))
	for _, ref := range byRevision {
		out = append(out, ref)
	}

	slices.SortFunc(out, func(a, b SecretRef) int { return cmp.Compare(b.Revision, a.Revision) })

	return out
}

// secretName is the Secret Helm 3 stores a revision in.
func secretName(name string, revision int) string {
	return "sh.helm.release.v1." + name + ".v" + strconv.Itoa(revision)
}

// pickTargetRef is the revision to roll back to; 0 means the one before the current.
func pickTargetRef(refs []SecretRef, revision int) (SecretRef, error) {
	if revision == 0 {
		if revision = refs[0].Revision - 1; revision < 1 {
			return SecretRef{}, errors.New("there is no earlier revision to roll back to")
		}
	}

	if revision == refs[0].Revision {
		return SecretRef{}, fmt.Errorf("revision %d is the current one", revision)
	}

	for _, ref := range refs {
		if ref.Revision == revision {
			return ref, nil
		}
	}

	return SecretRef{}, fmt.Errorf("revision %d is not kept any more (Helm keeps the last ones only)", revision)
}

func chartLabel(rel Release) string {
	return strings.TrimSuffix(rel.Chart.Metadata.Name+"-"+rel.Chart.Metadata.Version, "-")
}

// rollbackBlockers are the reasons a rollback must not run whatever its objects.
func rollbackBlockers(current stored) []string {
	blockers := []string{}

	if status := cmp.Or(current.rel.Info.Status, current.ref.Status); strings.HasPrefix(status, "pending-") {
		blockers = append(blockers, fmt.Sprintf("another operation is in progress (revision %d is %s)", current.ref.Revision, status))
	}

	return blockers
}

// fluxOwner is the HelmRelease whose helm-controller rendered these objects (it labels
// each one), "namespace/name", or "".
func fluxOwner(namespace string, lists ...[]object) string {
	for _, list := range lists {
		for _, o := range list {
			if name := o.objectLabel(fluxLabelName); name != "" {
				return cmp.Or(o.objectLabel(fluxLabelNamespace), namespace) + "/" + name
			}
		}
	}

	return ""
}

// planSteps compares both renderings with the live objects, keeps the changes that change
// something and dry-runs them, in Helm's order: creations and updates by kind, deletions in
// reverse.
func (rb *rollback) planSteps(ctx context.Context, k Env, current, target []object) error {
	slices.SortStableFunc(target, func(a, b object) int { return kindRank(a.Kind) - kindRank(b.Kind) })
	slices.SortStableFunc(current, func(a, b object) int { return kindRank(b.Kind) - kindRank(a.Kind) })

	for _, list := range [][]object{current, target} {
		for _, o := range list {
			withOwnership(o.Body, rb.plan.Name, rb.plan.ReleaseNamespace)
		}
	}

	byKey := map[string]object{}
	for _, o := range current {
		byKey[o.key()] = o
	}

	inTarget := map[string]bool{}
	steps := []rollbackStep{}

	for _, o := range target {
		inTarget[o.key()] = true

		if was, ok := byKey[o.key()]; ok {
			steps = append(steps, rollbackStep{action: ChangeUpdate, obj: o, original: was.Body})
		} else {
			steps = append(steps, rollbackStep{action: ChangeCreate, obj: o})
		}
	}

	for _, o := range current {
		if inTarget[o.key()] {
			continue
		}

		if o.annotation(annotationPolicy) == "keep" {
			rb.plan.Changes = append(rb.plan.Changes, Change{Action: ChangeKeep, Kind: o.Kind, Namespace: o.Namespace, Name: o.Name})

			continue
		}

		steps = append(steps, rollbackStep{action: ChangeDelete, obj: o})
	}

	steps, err := rb.withLive(ctx, k, steps)
	if err != nil {
		return err
	}

	failed := 0

	for _, s := range steps {
		change := Change{Action: s.action, Kind: s.obj.Kind, Namespace: s.obj.Namespace, Name: s.obj.Name}
		rb.plan.Changes = append(rb.plan.Changes, change)

		if s.action == ChangeKeep {
			continue
		}

		if err := applyStep(ctx, k, s, true); err != nil {
			rb.plan.Changes[len(rb.plan.Changes)-1].Error = k.apiError(err).Error()
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
func (rb *rollback) withLive(ctx context.Context, k Env, steps []rollbackStep) ([]rollbackStep, error) {
	settled := make([]*rollbackStep, len(steps))
	errs := make([]error, len(steps))
	slots := make(chan struct{}, liveParallel)

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

	out := []rollbackStep{}

	for _, s := range settled {
		if s != nil {
			out = append(out, *s)
		}
	}

	rb.plan.Unchanged = len(steps) - len(out) - rb.gone(settled, steps)

	return out, nil
}

// gone counts the deletions dropped because the object no longer exists.
func (rb *rollback) gone(settled []*rollbackStep, steps []rollbackStep) int {
	n := 0

	for i, s := range settled {
		if s == nil && steps[i].action == ChangeDelete {
			n++
		}
	}

	return n
}

func (rb *rollback) settle(ctx context.Context, k Env, s rollbackStep) (*rollbackStep, error) {
	var live map[string]any

	err := k.Get(ctx, s.obj.Path, &live)

	switch {
	case kube.IsNotFound(err):
		if s.action == ChangeDelete {
			return nil, nil
		}

		s.action, s.original = ChangeCreate, nil

		return &s, nil
	case err != nil:
		return nil, fmt.Errorf("%s: %w", s.obj.label(), err)
	case s.action == ChangeDelete:
		// Helm reads the policy on the live object: one annotated since stays.
		if (object{Body: live}).annotation(annotationPolicy) == "keep" {
			s.action = ChangeKeep
		}

		return &s, nil
	}

	if s.action == ChangeCreate {
		if !rb.owns(live) {
			return nil, fmt.Errorf("%s already exists and does not belong to release %s", s.obj.label(), rb.plan.Name)
		}

		s.action = ChangeUpdate
	}

	// Helm replaces unconditionally: a replace with nothing to change is sent all the same.
	if rb.opts.Force {
		s.action, s.body = ChangeReplace, replaceBody(s.obj, live)

		return &s, nil
	}

	s.patch = threeWayMergePatch(s.original, s.obj.Body, live)
	if len(s.patch) == 0 {
		return nil, nil
	}

	return &s, nil
}

// withOwnership adds what Helm stamps on every object it installs: the release's name
// and namespace annotations and the managed-by label. Without them the next helm upgrade
// would refuse the objects as not its own.
func withOwnership(obj map[string]any, release, namespace string) {
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

	set("annotations", annotationRelease, release)
	set("annotations", annotationReleaseNS, namespace)
	set("labels", "app.kubernetes.io/managed-by", "Helm")
}

// owns tells whether a live object carries this release's ownership annotations.
func (rb *rollback) owns(live map[string]any) bool {
	o := object{Body: live}

	return o.objectLabel("app.kubernetes.io/managed-by") == "Helm" &&
		o.annotation(annotationRelease) == rb.plan.Name && o.annotation(annotationReleaseNS) == rb.plan.ReleaseNamespace
}

// applyStep runs one change, or only checks it with dryRun.
func applyStep(ctx context.Context, k Env, s rollbackStep, dryRun bool) error {
	q := url.Values{"fieldManager": {fieldManager}}
	if dryRun {
		q.Set("dryRun", "All")
	}

	switch s.action {
	case ChangeCreate:
		body, err := json.Marshal(s.obj.Body)
		if err != nil {
			return err
		}

		collection := s.obj.Path[:strings.LastIndexByte(s.obj.Path, '/')]

		return k.Do(ctx, http.MethodPost, collection+"?"+q.Encode(), "application/json", body, nil)
	case ChangeUpdate:
		body, err := json.Marshal(s.patch)
		if err != nil {
			return err
		}

		return k.Do(ctx, http.MethodPatch, s.obj.Path+"?"+q.Encode(), "application/merge-patch+json", body, nil)
	case ChangeReplace:
		if !dryRun {
			return replaceObject(ctx, k, s.obj, s.obj.Path+"?"+q.Encode())
		}

		body, err := json.Marshal(s.body)
		if err != nil {
			return err
		}

		return k.Do(ctx, http.MethodPut, s.obj.Path+"?"+q.Encode(), "application/json", body, nil)
	default:
		q.Del("fieldManager")
		q.Set("propagationPolicy", "Background")

		err := k.Do(ctx, http.MethodDelete, s.obj.Path+"?"+q.Encode(), "application/json", nil, nil)
		if kube.IsNotFound(err) {
			return nil
		}

		return err
	}
}

// runRollback runs a prepared rollback: Flux suspended, revision N+1 recorded as
// pending-rollback, the pre-rollback hooks run, every change made, the post-rollback hooks
// run, then N+1 deployed and the others superseded, then the revisions beyond the history
// max deleted (a failed rollback leaves them all). A pre-rollback hook that fails records
// N+1 failed with nothing changed (the current revision stays as it is); a change that
// fails records it failed and the current one superseded, as Helm. A post-rollback hook that
// fails does the same: the spec's choice, not Helm's (Helm leaves N+1 pending-rollback).
// The description recorded is one line; the error returned keeps every detail.
// A delete refused comes back as pruneErr with err nil: the rollback itself is done.
func runRollback(ctx context.Context, k Env, rb *rollback, now time.Time) (pruneErr, err error) {
	if len(rb.plan.Blockers) > 0 {
		return nil, errors.New(strings.Join(rb.plan.Blockers, "; "))
	}

	if owner := rb.plan.FluxOwner; owner != "" {
		ns, name, _ := strings.Cut(owner, "/")
		if err := k.Suspend(ctx, ns, name, now); err != nil {
			return nil, fmt.Errorf("suspend the HelmRelease %s first: %w", owner, err)
		}
	}

	next := rb.plan.From + 1
	description := fmt.Sprintf("Rollback to %d", rb.plan.To)

	if err := createRevision(ctx, k, rb, next, description, now); err != nil {
		return nil, fmt.Errorf("record revision %d: %w", next, err)
	}

	// How it ended is recorded even when the rollback ran out of time: a revision left
	// pending-rollback would lock the release.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()

	ns, nextSecret := rb.plan.Namespace, secretName(rb.plan.Name, next)

	if err := runHooks(ctx, k, rb.preHooks); err != nil {
		return nil, errors.Join(
			fmt.Errorf("rollback stopped before any change, revision %d recorded as failed: %w", next, err),
			setStatus(record, k, ns, nextSecret, StatusFailed, failureDescription(rb.plan.Name, err), now),
		)
	}

	// As Helm: the current revision superseded, the new one failed.
	failed := func(err error) error {
		return errors.Join(
			fmt.Errorf("rollback stopped, revision %d recorded as failed: %w", next, err),
			setStatus(record, k, ns, rb.current.ref.Secret, StatusSuperseded, "", now),
			setStatus(record, k, ns, nextSecret, StatusFailed, failureDescription(rb.plan.Name, err), now),
		)
	}

	for _, s := range rb.steps {
		if err := applyStep(ctx, k, s, false); err != nil {
			return nil, failed(fmt.Errorf("%s %s: %w", s.action, s.obj.label(), k.apiError(err)))
		}
	}

	if err := runHooks(ctx, k, rb.postHooks); err != nil {
		return nil, failed(err)
	}

	// As Helm: every deployed revision superseded (a failed current one stays failed).
	errs := []error{}

	for _, ref := range rb.refs {
		if ref.Status == StatusDeployed {
			errs = append(errs, setStatus(record, k, ns, ref.Secret, StatusSuperseded, "", now))
		}
	}

	if err := errors.Join(append(errs, setStatus(record, k, ns, nextSecret, StatusDeployed, description, now))...); err != nil {
		return nil, err
	}

	return pruneHistory(record, k, rb.prune), nil
}

// failureDescription is a failed rollback's description for the release Secret, on one
// line: the errors errors.Join put on separate lines joined with "; ".
func failureDescription(name string, err error) string {
	lines := []string{}

	for _, line := range strings.FieldsFunc(err.Error(), func(r rune) bool { return r == '\n' || r == '\r' }) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return fmt.Sprintf("Rollback %q failed: %s", name, strings.Join(lines, "; "))
}
