package ichorgo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// The Kubernetes facade: how a client is opened for a talosconfig context (a Talos admin
// kubeconfig, a stored kubeconfig, Omni), and the funnels every Kubernetes call goes
// through (the client cache, the demo inventory, mutation errors). The requests themselves
// are in kube_transport.go.

// kubeClientTTL is how long a fetched admin kubeconfig is reused. Talos signs a new
// certificate on every Kubeconfig call, so the app does not ask for one per request.
const kubeClientTTL = 30 * time.Minute

// kubeClient is a minimal Kubernetes REST client: JSON over HTTPS with the credentials of
// a kubeconfig. It replaces client-go, which would add megabytes to the app for a few calls.
type kubeClient struct {
	base  *url.URL
	http  *http.Client
	token string
	// tokens replaces token for a cluster that signs in through a method (OIDC, EKS…).
	tokens *authTokenSource
	// unauthorized is what a refused token means for this cluster, "" for the API server's own message.
	unauthorized string
	tls          *tls.Config // the kubeconfig's TLS settings, for connections outside http (exec)
	// namespace is the kubeconfig context's namespace, "" when it sets none.
	namespace string
}

// openKubeClient builds a client for kubeconfig and picks the first API server address that
// answers: the kubeconfig's own server, then each Talos endpoint host on the same port. The
// server is often a VIP or an internal name the phone cannot reach while the Talos endpoints
// are; Talos puts every node address in the API server certificate, so TLS still verifies.
// A server the user set (see NormalizeKubeServer) is the only address tried.
func openKubeClient(ctx context.Context, kubeconfig string, talosEndpoints []string, server string) (*kubeClient, error) {
	creds, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return openKubeClientCreds(ctx, creds, talosEndpoints, server)
}

// openKubeClientCreds is openKubeClient for credentials already read.
func openKubeClientCreds(ctx context.Context, creds *kubeCredentials, talosEndpoints []string, server string) (*kubeClient, error) {
	candidates := []*url.URL{creds.server}

	switch {
	case server != "":
		if err := creds.applyKubeServer(server); err != nil {
			return nil, err
		}

		candidates = []*url.URL{creds.server}
	case !creds.tls.InsecureSkipVerify:
		// Without verification, another address could be anyone: only the configured one.
		candidates = kubeServerCandidates(creds.server, talosEndpoints)
	}

	// Probed in parallel (one probe timeout in all), the first candidate in order that
	// answers wins: the kubeconfig's own server is preferred when it works.
	clients := make([]*kubeClient, len(candidates))
	results := make([]chan error, len(candidates))

	for i, base := range candidates {
		clients[i], results[i] = newKubeClient(base, creds), make(chan error, 1)
		go func() { results[i] <- safeCall(func() error { return clients[i].probe(ctx) }) }()
	}

	// Once decided, the other probes finish in the background and their clients are closed.
	discardFrom := func(i int) {
		go func() {
			for j := i; j < len(results); j++ {
				<-results[j]
				clients[j].close()
			}
		}()
	}

	var failures []string

	for i, result := range results {
		err := <-result

		var apiErr *kubeAPIError

		switch {
		case err == nil:
			discardFrom(i + 1)

			return clients[i], nil
		case errors.As(err, &apiErr):
			// It answered: the address is right, the credentials are not.
			clients[i].close()
			discardFrom(i + 1)

			return nil, err
		default:
			clients[i].close()
			failures = append(failures, candidates[i].Host+": "+kubeTransportError(err))
		}
	}

	return nil, errors.New("Kubernetes API not reachable from this device (" + strings.Join(failures, "; ") + ")")
}

// kubeServerCandidates lists server, then each Talos endpoint host with server's port.
func kubeServerCandidates(server *url.URL, talosEndpoints []string) []*url.URL {
	port := server.Port()
	if port == "" {
		port = "443"
	}

	out := []*url.URL{server}
	seen := []string{server.Host}

	for _, endpoint := range talosEndpoints {
		host := endpoint
		if h, _, err := net.SplitHostPort(endpoint); err == nil {
			host = h
		}

		host = strings.Trim(host, "[]")
		if host == "" {
			continue
		}

		addr := net.JoinHostPort(host, port)
		if slices.Contains(seen, addr) {
			continue
		}

		seen = append(seen, addr)
		out = append(out, &url.URL{Scheme: "https", Host: addr, Path: server.Path})
	}

	return out
}

func newKubeClient(base *url.URL, creds *kubeCredentials) *kubeClient {
	// The API server does not redirect: the client never sends the credentials anywhere else.
	httpc := httpx.New(httpx.Opts{TLS: creds.tls, DialTimeout: kubeProbeTimeout, IdleTimeout: sessionIdle})

	return &kubeClient{base: base, http: httpc, token: creds.token, tokens: creds.tokens, tls: creds.tls.Clone(), namespace: creds.namespace}
}

// bearer is the token to send, "" for none (a client certificate).
func (k *kubeClient) bearer(ctx context.Context) (string, error) {
	if k.tokens != nil {
		return k.tokens.get(ctx)
	}

	return k.token, nil
}

// close drops the client's idle connections (its TLS sessions hold the client key).
func (k *kubeClient) close() { k.http.CloseIdleConnections() }

// withKube runs fn with the context's Kubernetes client, bounded by callTimeout once the
// client is there. A call that gets no answer or is refused for its credentials drops the
// client: the next one fetches a fresh kubeconfig and looks for a reachable address again
// (the phone may have changed networks). A client just opened and probed is kept on a
// timeout: the address answered a moment ago, the call itself was slow.
func withKube[T any](target kubeTarget, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	k, fresh, err := kubeClients.get(target)
	if err != nil {
		var zero T

		return zero, err
	}

	// Started after opening: a cold open (kubeconfig fetch and probe) must not eat the budget.
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	return callKube(ctx, target, k, fresh, fn)
}

// withKubeContext is withKube bounded by ctx instead of callTimeout, for the calls that run
// longer (a network test). Cancelling ctx keeps the client: the address did not fail.
func withKubeContext[T any](ctx context.Context, target kubeTarget, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	var zero T

	k, fresh, err := kubeClients.get(target)
	if err != nil {
		return zero, err
	}

	return callKube(ctx, target, k, fresh, fn)
}

// callKube runs fn with k, dropping the client when the call shows it no longer works.
func callKube[T any](ctx context.Context, target kubeTarget, k *kubeClient, fresh bool, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	var zero T

	out, err := fn(ctx, k)
	if err != nil {
		var apiErr *kubeAPIError

		switch {
		case errors.As(err, &apiErr):
			if apiErr.Code == http.StatusUnauthorized {
				if k.tokens != nil {
					k.tokens.invalidate()
				}

				kubeClients.forget(target, k)

				if k.unauthorized != "" {
					return zero, errors.New(k.unauthorized)
				}
			}
		case errors.Is(err, context.Canceled):
		case fresh && errors.Is(err, context.DeadlineExceeded):
		default:
			kubeClients.forget(target, k)
		}

		return zero, kubeError(err)
	}

	return out, nil
}

// kubeReadJSON reads with fn (demo() in the demo inventory) and returns the result as JSON.
// target.context is the unmasked context name.
func kubeReadJSON[T any](target kubeTarget, demo func() T, fn func(context.Context, *kubeClient) (T, error)) (string, error) {
	if isDemoContext(target.config, target.context) {
		return toJSON(demo())
	}

	res, err := withKube(target, fn)
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// noResult adapts an action to the result-returning helpers.
func noResult(fn func(context.Context, *kubeClient) error) func(context.Context, *kubeClient) (struct{}, error) {
	return func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, fn(ctx, k)
	}
}

// kubeDo is withKubeContext for an action without a result.
func kubeDo(ctx context.Context, target kubeTarget, fn func(context.Context, *kubeClient) error) error {
	_, err := withKubeContext(ctx, target, noResult(fn))

	return err
}

// kubeMutate runs the action fn, refused in the demo inventory. target.context is the
// unmasked context name.
func kubeMutate(target kubeTarget, fn func(context.Context, *kubeClient) error) error {
	if isDemoContext(target.config, target.context) {
		return errDemoUnavailable
	}

	_, err := withKube(target, noResult(fn))

	return kubeMutationError(err)
}

// kubeMutationError explains an action that got no answer: the API server may have
// applied it anyway, so a blind retry could do it twice.
func kubeMutationError(err error) error {
	var apiErr *kubeAPIError
	if err == nil || errors.As(err, &apiErr) {
		return err
	}

	return fmt.Errorf("%w (it may have been applied: refresh before trying again)", err)
}

// openKubeClientForContext fetches an admin kubeconfig from Talos (bounded by callTimeout)
// and probes the API server addresses (bounded by kubeProbeTimeout). A cluster added from a
// kubeconfig uses its own context, at its own server only.
func openKubeClientForContext(target kubeTarget) (*kubeClient, error) {
	if isKubeconfig(target.config) {
		return openStoredKubeClient(target)
	}

	var (
		endpoints []string
		omni      *kubeClient
	)

	kubeconfig, err := withSession(target.config, target.context, callTimeout, func(ctx context.Context, s *session) (string, error) {
		// Through Omni: its kube proxy, with an Omni sign-in (no Talos admin kubeconfig).
		if s.signing != nil {
			k, err := omniKubeClient(ctx, s)
			omni = k

			return "", err
		}

		endpoints = s.context.Endpoints

		return fetchKubeconfig(ctx, s)
	})
	if omni != nil || err != nil {
		return omni, err
	}

	return openKubeClient(context.Background(), kubeconfig, endpoints, target.server)
}

// openStoredKubeClient opens a cluster added from a kubeconfig. A context that signs in
// through a method gets its token source; the first token is minted before probing, so a
// needed sign-in is reported as such rather than as an unreachable server.
func openStoredKubeClient(target kubeTarget) (*kubeClient, error) {
	sc, err := signInContext(target.config, target.context)
	if err != nil {
		return nil, err
	}

	if sc.method == nil {
		kubeconfig, err := kubeContextYAML(target.config, target.context)
		if err != nil {
			return nil, err
		}

		return openKubeClient(context.Background(), kubeconfig, nil, target.server)
	}

	tokens := kubeAuth.source(sc.key, sc.method)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if _, err := tokens.get(ctx); err != nil {
		return nil, err
	}

	kubeconfig, err := kubeContextYAMLWithoutUser(target.config, target.context)
	if err != nil {
		return nil, err
	}

	creds, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	creds.token, creds.tokens = "", tokens

	return openKubeClientCreds(context.Background(), creds, nil, target.server)
}
