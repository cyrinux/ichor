package ichorgo

import (
	"fmt"
	"log/slog"
)

// A Go panic that reaches gomobile aborts the whole app, so none may leave the package:
// exported functions recover in maskResult/maskErr, background runs with onPanic.
// recover only works when called by the deferred function itself, hence one helper per use.

func panicError(r any) error {
	err := fmt.Errorf("internal error: %v", r)
	diag(slog.LevelError, "api", "panic recovered", err)

	return err
}

// recoverPanic turns a panic into *err (use with defer and a named result).
func recoverPanic(err *error) {
	if r := recover(); r != nil {
		*err = panicError(r)
	}
}

// safeCall runs fn, returning a panic as an error: for the package's inner goroutines,
// whose caller waits for that error.
func safeCall(fn func() error) (err error) {
	defer recoverPanic(&err)

	return fn()
}

// onPanic reports a panic in a background run through done, so that its listener still gets
// OnDone (use with defer at the top of the run's goroutine).
func onPanic(done func(errMessage string)) {
	if r := recover(); r != nil {
		done(panicError(r).Error())
	}
}
