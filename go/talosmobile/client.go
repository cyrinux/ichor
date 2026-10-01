package talosmobile

import (
	"context"
	"fmt"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

const (
	callTimeout   = 20 * time.Second
	nodeTimeout   = 8 * time.Second
	healthTimeout = 2 * time.Minute
	// Connections are reused across calls (live graphs poll every few seconds) and closed
	// after this long without use.
	sessionIdle = 2 * time.Minute
)

// session is an open connection to a talosconfig context's endpoints.
type session struct {
	client  *client.Client
	context *clientconfig.Context
	onClose func() // test hook
}

func openSession(configYAML, contextName string) (*session, error) {
	_, cfgCtx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return nil, err
	}

	// client.New only dials lazily, so no context is needed here.
	c, err := client.New(context.Background(), client.WithConfigContext(cfgCtx))
	if err != nil {
		return nil, fmt.Errorf("create Talos client: %w", err)
	}

	return &session{client: c, context: cfgCtx}, nil
}

func (s *session) Close() {
	if s.client != nil {
		_ = s.client.Close() //nolint:errcheck
	}

	if s.onClose != nil {
		s.onClose()
	}
}

var sessions = newSessionCache(sessionIdle, openSession)

// withSession runs fn with a (reused) session, bounded by timeout.
func withSession[T any](configYAML, contextName string, timeout time.Duration, fn func(context.Context, *session) (T, error)) (T, error) {
	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		var zero T

		return zero, err
	}

	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return fn(ctx, s)
}
