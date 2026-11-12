package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// What the image imports of AWS, Azure and Google Cloud share: running in the background with
// the audit entry, and wording how a run ended once its cleanup ran.

// startCloudImageRun runs run within timeout in the background, recorded in the activity log as
// audit says. On success done gives the result for OnDone and the message of the last phase.
func startCloudImageRun(config, contextName string, listener JoinListener, audit func() auditAction, timeout time.Duration,
	run func(ctx context.Context) error, done func() (result any, message string), emit func(phase, message string),
) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })
		defer holdCalls(config, contextName)()

		err := recordedRun(config, contextName, audit, func() error { return run(ctx) })
		if err != nil {
			listener.OnDone("", errText(err))

			return
		}

		result, message := done()

		out, err := toJSON(result)
		if err != nil {
			listener.OnDone("", errText(err))

			return
		}

		emit(joinPhaseDone, message)
		listener.OnDone(privacy.mask(out), "")
	}()

	return &JoinRun{cancel: cancel}
}

// cloudCleanupOutcome words how an import ended once its cleanup ran: why it failed (nil when
// it did not), ready what it made ("AMI ami-1 is ready"), deleted what the cleanup removed and
// stillThere what it could not ("" when nothing is left).
func cloudCleanupOutcome(why error, ready, stillThere string, deleted []string) error {
	switch {
	case why == nil && stillThere == "":
		return nil
	case why == nil:
		return fmt.Errorf("%s. %s", ready, stillThere)
	case len(deleted) > 0 && stillThere != "":
		return fmt.Errorf("%w. Deleted: %s. %s", why, strings.Join(deleted, ", "), stillThere)
	case len(deleted) > 0:
		return fmt.Errorf("%w. Deleted: %s", why, strings.Join(deleted, ", "))
	case stillThere != "":
		return fmt.Errorf("%w. %s", why, stillThere)
	}

	return why
}
