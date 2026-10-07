package ichorgo

import (
	"context"
	"errors"
)

// SignInListener follows an interactive sign-in (implemented in Kotlin/Swift).
type SignInListener interface {
	// OnPrompt gets a JSON signInPrompt: open its URL in the browser, show a device code.
	OnPrompt(json string)
	// OnDone is called exactly once; errMessage is empty on success (and "" when cancelled
	// too: the app knows it cancelled).
	OnDone(errMessage string)
}

// SignInRun is a handle on an interactive sign-in.
type SignInRun struct {
	cancel    context.CancelFunc
	callbacks chan string
}

// Cancel stops waiting for the user; OnDone follows.
func (r *SignInRun) Cancel() { r.cancel() }

// Complete hands over the URL the browser came back with, when the app received it itself
// (a redirect to its own scheme rather than to the loopback address).
func (r *SignInRun) Complete(callbackURL string) {
	select {
	case r.callbacks <- callbackURL:
	default:
	}
}

type maskedSignInListener struct{ SignInListener }

func (l maskedSignInListener) OnPrompt(json string) { l.SignInListener.OnPrompt(json) }
func (l maskedSignInListener) OnDone(errMessage string) {
	l.SignInListener.OnDone(privacy.maskPlain(errMessage))
}

// StartKubeSignIn signs the named stored context in through its browser or device-code
// method, then stores the result (see AuthStore).
func StartKubeSignIn(storedYAML, contextName string, listener SignInListener) *SignInRun {
	contextName = unmaskContext(storedYAML, contextName)

	listener = maskedSignInListener{listener}

	ctx, cancel := context.WithCancel(context.Background())
	run := &SignInRun{cancel: cancel, callbacks: make(chan string, 1)}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := runSignIn(ctx, storedYAML, contextName, listener, run.callbacks)
		if errors.Is(err, context.Canceled) {
			err = nil
		}

		msg := ""
		if err != nil {
			msg = err.Error()
		}

		listener.OnDone(msg)
	}()

	return run
}

func runSignIn(ctx context.Context, storedYAML, contextName string, listener SignInListener, callbacks <-chan string) error {
	sc, err := signInContext(storedYAML, contextName)
	if err != nil {
		return err
	}

	im, ok := sc.method.(interactiveMethod)
	if !ok {
		return errors.New("this cluster does not sign in in a browser")
	}

	state := kubeAuth.load(sc.key)
	if state.Method != im.name() {
		state = kubeAuthState{Method: im.name()}
	}

	signed, err := im.signIn(ctx, state, func(p signInPrompt) { emitJSON(p, listener.OnPrompt) }, callbacks)
	if err != nil {
		return err
	}

	signed.Method = im.name()
	kubeAuth.forget(sc.key)
	kubeAuth.save(sc.key, signed)
	kubeClients.forgetConfig(storedYAML, contextName)

	return nil
}
