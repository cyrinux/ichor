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
)

// session is an open connection to a talosconfig context's endpoints.
type session struct {
	client  *client.Client
	context *clientconfig.Context
}

func openSession(ctx context.Context, configYAML, contextName string) (*session, error) {
	_, cfgCtx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return nil, err
	}

	c, err := client.New(ctx, client.WithConfigContext(cfgCtx))
	if err != nil {
		return nil, fmt.Errorf("create Talos client: %w", err)
	}

	return &session{client: c, context: cfgCtx}, nil
}

func (s *session) Close() {
	_ = s.client.Close() //nolint:errcheck
}

// withSession runs fn with a fresh session bounded by timeout.
func withSession[T any](configYAML, contextName string, timeout time.Duration, fn func(context.Context, *session) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s, err := openSession(ctx, configYAML, contextName)
	if err != nil {
		var zero T

		return zero, err
	}

	defer s.Close()

	return fn(ctx, s)
}
