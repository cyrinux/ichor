package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The sweep in the background: Delete in the apps starts it and closes the sheet, the
// operations notification (Android) or Live Activity (iOS) counts the pods, and Stop ends it
// after the pod being deleted.

// KubeSanitizeListener follows a background pod sweep (implemented in Kotlin/Swift).
type KubeSanitizeListener interface {
	// OnProgress gets {"done","total","name","failed","at"} after every pod.
	OnProgress(json string)
	// OnDone is called exactly once with the result (see KubeSanitize; the pods tried so far,
	// "stopped" true, after a Cancel) and errMessage: empty on success, on a partial sweep and
	// after a Cancel; set when the sweep was refused, could not run or ran out of time.
	OnDone(resultJSON string, errMessage string)
}

// KubeSanitizeRun is a handle on a running pod sweep.
type KubeSanitizeRun struct {
	cancel context.CancelFunc
}

// Cancel stops the sweep once the pod being deleted is done; OnDone follows.
func (r *KubeSanitizeRun) Cancel() { r.cancel() }

// sanitizeProgress is sent after every pod: Done pods tried of Total, Failed of them not deleted.
type sanitizeProgress struct {
	Done   int    `json:"done"`
	Total  int    `json:"total"`
	Name   string `json:"name"` // "namespace/name", the pod just tried
	Failed int    `json:"failed"`
	At     int64  `json:"at"`
}

// sanitizeRunTimeout bounds a background sweep: one page of pods, each with its own call budget.
const sanitizeRunTimeout = 30 * time.Minute

// kubeSweep is what a sweep deletes, as the apps pass it.
type kubeSweep struct {
	configYAML, contextName, kubeServer, namespace, names string
}

// StartKubeSanitize runs KubeSanitize in the background, reporting after every pod. A
// refused sweep (demo, malformed or out-of-scope names) deletes nothing and ends with
// OnDone at once. One audit entry for the sweep, as KubeSanitize, partial "stopped after
// k of n pods" after a Cancel. Needs delete on pods. kubeServer: see KubePods.
func StartKubeSanitize(configYAML, contextName, kubeServer, namespace, names string, listener KubeSanitizeListener) *KubeSanitizeRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeSanitizeListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), sanitizeRunTimeout)
	emit := func(p sanitizeProgress) {
		p.At = time.Now().UnixMilli()
		emitJSON(p, listener.OnProgress)
	}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })

		res, err := sweepPods(ctx, kubeSweep{configYAML, contextName, kubeServer, namespace, names}, emit)
		clearPartial(&err)

		out, encErr := toJSON(res)
		if encErr != nil {
			out, err = "", encErr
		}

		listener.OnDone(out, errText(err))
	}()

	return &KubeSanitizeRun{cancel: cancel}
}

// sweepPods deletes s's pods (its context already unmasked) and records the sweep once: a
// partialOutcome when some pods were not deleted or the sweep was stopped, an error when it
// was refused or timed out.
func sweepPods(ctx context.Context, s kubeSweep, emit func(sanitizeProgress)) (sanitizeResult, error) {
	contextName := s.contextName
	namespace := privacy.revealNamespace(strings.TrimSpace(s.namespace))
	pods, parseErr := sanitizeTargets(namespace, s.names)
	res := sanitizeResult{Failed: []sanitizeFailure{}}

	action := func() auditAction {
		return auditAction{
			Server: s.kubeServer, Action: "sanitize-pods", Namespace: namespace,
			Object: fmt.Sprintf("Pod×%d", len(pods)), Params: fmt.Sprintf("pods=%d", len(pods)),
		}
	}

	err := recordedRun(s.configYAML, contextName, action, func() error {
		if parseErr != nil {
			return parseErr
		}

		if isDemoContext(s.configYAML, contextName) {
			return errDemoUnavailable
		}

		err := kubeDo(ctx, kubeTarget{s.configYAML, contextName, s.kubeServer}, func(ctx context.Context, k *kubeClient) error {
			res = deletePods(ctx, k, pods, emit)

			return nil
		})
		if err != nil {
			return kubeMutationError(err)
		}

		return sweepOutcome(ctx, res, len(pods))
	})

	return res, err
}

// sweepOutcome is how a sweep that ran ended: nil when every pod went, a partialOutcome
// when some did not or it was stopped, an error when it ran out of time.
func sweepOutcome(ctx context.Context, res sanitizeResult, total int) error {
	tried := res.Deleted + len(res.Failed)

	switch {
	case res.Stopped && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("the sweep timed out after %d of %d pods", tried, total)
	case res.Stopped:
		return partialOutcome{fmt.Sprintf("stopped after %d of %d pods", tried, total)}
	case len(res.Failed) > 0:
		return partialOutcome{fmt.Sprintf("%d of %d pods not deleted", len(res.Failed), total)}
	}

	return nil
}
