package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunbookListener follows a runbook run (implemented in Kotlin/Swift).
type RunbookListener interface {
	// OnStep gets {"index","id","state","message","startedAt","endedAt"} when step index (from
	// 0) starts and when it ends: state waiting (a destructive step waits for Confirm),
	// running, ok, failed or skipped; times in unix ms.
	OnStep(index int, stateJSON string)
	// OnDone gets {"ok","steps":[…]} once every step ran or was skipped.
	OnDone(resultJSON string)
	// OnError says why the runbook did not start (it does not load, the demo); then nothing
	// else is called. Exactly one of OnDone and OnError is called.
	OnError(message string)
}

// RunbookRun is a handle on a running runbook.
type RunbookRun struct {
	cancel  context.CancelFunc
	confirm chan int
}

// Confirm lets the destructive step index, waiting since its "waiting" state, run. A
// confirmation for another step is ignored.
func (r *RunbookRun) Confirm(index int) {
	select {
	case r.confirm <- index:
	default:
	}
}

// Stop ends the run: a running step that has its own handle (maintenance, snapshot, a
// GitOps follow) is cancelled through it and reported failed; the next steps are skipped.
func (r *RunbookRun) Stop() { r.cancel() }

const (
	runbookWaiting = "waiting"
	runbookRunning = "running"
	runbookOK      = "ok"
	runbookFailed  = "failed"
	runbookSkipped = "skipped"
)

type runbookStepState struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	State     string `json:"state"`
	Message   string `json:"message"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt"`
}

type runbookResult struct {
	OK    bool               `json:"ok"`
	Steps []runbookStepState `json:"steps"`
}

// StartRunbook is StartRunbookWith with no values: a runbook with a required parameter does
// not start.
func StartRunbook(configYAML, contextName, kubeServer, runbookYAML, continueIDs string, listener RunbookListener) *RunbookRun {
	return StartRunbookWith(configYAML, contextName, kubeServer, runbookYAML, continueIDs, "", listener)
}

// StartRunbookWith runs the steps of runbookYAML in order (docs/runbooks.md), with valuesJSON
// ({"name":"value"}, "" for none) in place of its parameters, each through the
// exported function its button calls, so each keeps its own can-I gate, demo refusal and
// Activity entry; one more entry, "runbook", records the whole. It stops at the first
// failed step unless that step has continueOnError, or its id is in continueIDs (comma
// separated: the steps the user chose to go past). Each destructive step waits, in state
// waiting, until Confirm(its index) or Stop. The demo is refused before any step. Roles:
// each step's (see RunbookPlanWith). kubeServer: see KubePods.
func StartRunbookWith(configYAML, contextName, kubeServer, runbookYAML, continueIDs, valuesJSON string, listener RunbookListener) *RunbookRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedRunbookListener{listener}

	ctx, cancel := context.WithCancel(context.Background())
	handle := &RunbookRun{cancel: cancel, confirm: make(chan int, 1)}

	go func() {
		defer cancel()
		defer onPanic(listener.OnError)
		defer holdCalls(configYAML, contextName)()

		var rb runbook

		values, err := parseRunbookValues(valuesJSON)
		if err == nil {
			rb, err = loadRunbookWith(runbookYAML, values)
		}

		if err == nil {
			rb = rb.continuingAt(splitCSV(continueIDs))
		}

		if err == nil && isDemoContext(configYAML, contextName) {
			err = errDemoUnavailable
		}

		if err != nil {
			listener.OnError(err.Error())

			return
		}

		r := runbookExec{target: kubeTarget{configYAML, contextName, kubeServer}, listener: listener, confirm: handle.confirm}
		emitJSON(r.run(ctx, rb), listener.OnDone)
	}()

	return handle
}

type maskedRunbookListener struct{ RunbookListener }

func (l maskedRunbookListener) OnStep(index int, stateJSON string) {
	l.RunbookListener.OnStep(index, privacy.mask(stateJSON))
}

func (l maskedRunbookListener) OnDone(resultJSON string) {
	l.RunbookListener.OnDone(privacy.mask(resultJSON))
}

func (l maskedRunbookListener) OnError(message string) {
	l.RunbookListener.OnError(privacy.mask(message))
}

type runbookExec struct {
	target   kubeTarget
	listener RunbookListener
	confirm  <-chan int
}

// confirmed waits until step index is confirmed; false when the run was stopped first.
func (r runbookExec) confirmed(ctx context.Context, index int) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case i := <-r.confirm:
			if i == index {
				return true
			}
		}
	}
}

func (r runbookExec) run(ctx context.Context, rb runbook) runbookResult {
	res := runbookResult{OK: true, Steps: make([]runbookStepState, len(rb.Steps))}
	stopped := ""

	for i, s := range rb.Steps {
		st := runbookStepState{Index: i, ID: s.ID}

		if stopped == "" && ctx.Err() != nil {
			stopped, res.OK = "the run was stopped", false
		}

		if stopped != "" {
			st.State, st.Message = runbookSkipped, stopped
			res.Steps[i] = st
			emitJSON(st, func(js string) { r.listener.OnStep(i, js) })

			continue
		}

		if runbookActions[s.Action].destructive {
			st.State = runbookWaiting
			emitJSON(st, func(js string) { r.listener.OnStep(i, js) })

			if !r.confirmed(ctx, i) {
				stopped, res.OK = "the run was stopped", false
				st.State, st.Message = runbookSkipped, stopped
				res.Steps[i] = st
				emitJSON(st, func(js string) { r.listener.OnStep(i, js) })

				continue
			}
		}

		st.State, st.StartedAt = runbookRunning, time.Now().UnixMilli()
		emitJSON(st, func(js string) { r.listener.OnStep(i, js) })

		msg, err := r.step(ctx, s)

		st.EndedAt, st.State, st.Message = time.Now().UnixMilli(), runbookOK, msg
		if err != nil {
			st.State, st.Message, res.OK = runbookFailed, err.Error(), false

			if !s.ContinueOnError {
				stopped = "step " + s.ID + " failed"
			}
		}

		res.Steps[i] = st
		emitJSON(st, func(js string) { r.listener.OnStep(i, js) })
	}

	r.record(rb, res)

	return res
}

// record adds the runbook's own Activity entry, after its steps' entries.
func (r runbookExec) record(rb runbook, res runbookResult) {
	ok := 0

	for _, s := range res.Steps {
		if s.State == runbookOK {
			ok++
		}
	}

	var err error
	if !res.OK {
		err = fmt.Errorf("%d of %d steps ran", ok, len(res.Steps))
	}

	recordOutcome(r.target.config, r.target.context, auditAction{
		Server: r.target.server, Action: "runbook", Object: rb.Name, Params: fmt.Sprintf("steps=%d ok=%d", len(res.Steps), ok),
	}, err)
}

// step runs one step through its exported function; the message says what it did.
func (r runbookExec) step(ctx context.Context, s runbookStep) (string, error) {
	cfg, cluster, server := r.target.config, r.target.context, r.target.server
	t, p := s.Target, s.Params

	switch s.Action {
	case "scale":
		warning, err := KubeScale(cfg, cluster, server, t.Kind, t.Namespace, t.Name, *p.Replicas)

		return strings.TrimSpace(fmt.Sprintf("scaled to %d. %s", *p.Replicas, warning)), err
	case "rolloutRestart":
		return "restarted", KubeRolloutRestart(cfg, cluster, server, t.Kind, t.Namespace, t.Name)
	case "suspendCronJob":
		return auditVerb(*p.Suspend, "suspended", "resumed"), KubeSuspendCronJob(cfg, cluster, server, t.Namespace, t.Name, *p.Suspend)
	case "triggerCronJob":
		job, err := KubeTriggerCronJob(cfg, cluster, server, t.Namespace, t.Name)

		return "started Job " + job, err
	case "argoSync":
		return r.argoSync(ctx, t)
	case "fluxReconcile":
		return r.fluxReconcile(ctx, t)
	case "etcdSnapshot":
		return r.etcdSnapshot(ctx, t.Node)
	case "nodeReboot":
		return "reboot requested (mode " + p.Mode + ")", Reboot(cfg, cluster, t.Node, p.Mode)
	case "nodeMaintenance":
		return r.maintenance(ctx, t.Node, p)
	}

	return "", fmt.Errorf("unknown action %q", s.Action)
}

// awaitRun receives a background run's end, and cancels the run when the runbook stops.
func awaitRun[T any](ctx context.Context, done <-chan T, cancel func()) T {
	select {
	case v := <-done:
		return v
	case <-ctx.Done():
		cancel()

		return <-done
	}
}

type runbookMaintenance struct{ done chan string }

func (m runbookMaintenance) OnProgress(string)        {}
func (m runbookMaintenance) OnDone(errMessage string) { m.done <- errMessage }
func (r runbookExec) maintenance(ctx context.Context, node string, p runbookParams) (string, error) {
	l := runbookMaintenance{done: make(chan string, 1)}
	run := StartNodeMaintenance(r.target.config, r.target.context, r.target.server, node, p.Action, p.IncludeBare, p.Acknowledge, l)

	if msg := awaitRun(ctx, l.done, run.Cancel); msg != "" {
		return "", errors.New(msg)
	}

	return "maintenance (" + p.Action + ") done", nil
}

type runbookSnapshot struct{ done chan error }

func (s runbookSnapshot) OnProgress(int64) {}
func (s runbookSnapshot) OnDone(_ string, _ int64, _ string, errMessage string) {
	if errMessage != "" {
		s.done <- errors.New(errMessage)

		return
	}

	s.done <- nil
}

// etcdSnapshot writes the snapshot into the app's data directory, under snapshots/.
func (r runbookExec) etcdSnapshot(ctx context.Context, node string) (string, error) {
	dir := dataDir()
	if dir == "" {
		return "", errors.New("no data directory to write the snapshot to")
	}

	dir = filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	safe := strings.Map(func(c rune) rune {
		if c == '/' || c == '\\' || c == ':' {
			return '-'
		}

		return c
	}, node)
	path := filepath.Join(dir, fmt.Sprintf("etcd-%s-%s.db", safe, time.Now().UTC().Format("20060102-150405")))

	l := runbookSnapshot{done: make(chan error, 1)}
	run := StartEtcdSnapshot(r.target.config, r.target.context, node, path, l)

	if err := awaitRun(ctx, l.done, run.Cancel); err != nil {
		return "", err
	}

	return "snapshot written to " + path, nil
}

// followDone is a GitOps follow's end: its result and why following failed.
type followDone struct{ result, errMessage string }

type runbookFollow struct{ done chan followDone }

func (f runbookFollow) OnProgress(string) {}
func (f runbookFollow) OnDone(result, errMessage string) {
	f.done <- followDone{result, errMessage}
}

// followed turns a follow's end into the step's outcome; ok is the result that means done.
func followed(d followDone, ok string) (string, error) {
	switch {
	case d.errMessage != "":
		return "", errors.New(d.errMessage)
	case d.result == "":
		return "", errors.New("stopped while following; the operation goes on")
	case !strings.EqualFold(d.result, ok):
		return "", errors.New("ended " + d.result)
	}

	return d.result, nil
}

func (r runbookExec) argoSync(ctx context.Context, t runbookTarget) (string, error) {
	cfg, cluster, server := r.target.config, r.target.context, r.target.server

	if err := KubeArgoAction(cfg, cluster, server, t.Namespace, t.Name, argoActionSync, ""); err != nil {
		return "", err
	}

	f := runbookFollow{done: make(chan followDone, 1)}
	run := StartArgoSyncFollow(cfg, cluster, server, t.Namespace, t.Name, f)

	return followed(awaitRun(ctx, f.done, run.Cancel), "Succeeded")
}

func (r runbookExec) fluxReconcile(ctx context.Context, t runbookTarget) (string, error) {
	cfg, cluster, server := r.target.config, r.target.context, r.target.server

	if err := KubeFluxAction(cfg, cluster, server, t.Kind, t.Namespace, t.Name, fluxActionReconcile); err != nil {
		return "", err
	}

	f := runbookFollow{done: make(chan followDone, 1)}
	run := StartFluxReconcileFollow(cfg, cluster, server, t.Kind, t.Namespace, t.Name, f)

	return followed(awaitRun(ctx, f.done, run.Cancel), "ready")
}
