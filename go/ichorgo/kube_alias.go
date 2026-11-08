package ichorgo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/cyrinux/ichor/go/kube"
)

// The Kubernetes transport lives in go/kube. These are the thin unexported names the
// kube_* files call, so none of them names the package: a domain moved out later imports
// go/kube directly.

const (
	kubeProbeTimeout = kube.ProbeTimeout
	kubeMaxBody      = kube.MaxBody
	kubeListExpired  = kube.ListExpired
)

type (
	kubeAPIError    = kube.APIError
	kubeList[T any] = kube.List[T]
)

// api is the transport view of k: its address, its HTTP client and its bearer token.
func (k *kubeClient) api() kube.Client {
	return kube.Client{Base: k.base, HTTP: k.http, Token: k.bearer}
}

func (k *kubeClient) probe(ctx context.Context) error { return k.api().Probe(ctx) }

func (k *kubeClient) get(ctx context.Context, path string, out any) error {
	return k.api().Get(ctx, path, out)
}

func (k *kubeClient) patch(ctx context.Context, path, contentType string, body any, out any) error {
	return k.api().Patch(ctx, path, contentType, body, out)
}

func (k *kubeClient) endpoint(path string) (*url.URL, error) { return k.api().Endpoint(path) }

func (k *kubeClient) getRaw(ctx context.Context, path string, header map[string]string) (status int, ctype string, body []byte, err error) {
	return k.api().GetRaw(ctx, path, header)
}

func (k *kubeClient) post(ctx context.Context, path string, body any, out any) error {
	return k.api().Post(ctx, path, body, out)
}

func (k *kubeClient) getText(ctx context.Context, path string) (string, error) {
	return k.api().GetText(ctx, path)
}

func (k *kubeClient) do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	return k.api().Do(ctx, method, path, contentType, body, out)
}

func (k *kubeClient) send(ctx context.Context, method, path, accept, contentType string, body []byte, header map[string]string) (*http.Response, []byte, error) {
	return k.api().Send(ctx, method, path, accept, contentType, body, header)
}

func (k *kubeClient) stream(ctx context.Context, path, accept string, read func(io.Reader) error) error {
	return k.api().Stream(ctx, path, accept, read)
}

func (k *kubeClient) request(ctx context.Context, method, path, accept, contentType string, body []byte, header map[string]string) (*http.Response, error) {
	return k.api().Request(ctx, method, path, accept, contentType, body, header)
}

// scopedPath: see kube.ScopedPath.
func scopedPath(group, namespace, resource string) string {
	return kube.ScopedPath(group, namespace, resource)
}

func serviceProxyPath(namespace, service, path string) string {
	return kube.ServiceProxyPath(namespace, service, path)
}

func isKubeStatus(body []byte) bool { return kube.IsStatus(body) }

func kubeCode(err error) int { return kube.Code(err) }

func isNotFound(err error) bool { return kube.IsNotFound(err) }

func isForbidden(err error) bool { return kube.IsForbidden(err) }

func ignoreNotFound(err error) error { return kube.IgnoreNotFound(err) }

func kubeStatusError(code int, data []byte) error { return kube.StatusError(code, data) }

func kubeTransportError(err error) string { return kube.TransportError(err) }

// kubeError is the message for any error of a Kubernetes call: kube.Error, and a needed
// sign-in as itself (without what wrapped it), which the apps recognise by its code.
func kubeError(err error) error {
	var (
		apiErr *kubeAPIError
		signIn *errSignInRequired
	)

	if !errors.As(err, &apiErr) && errors.As(err, &signIn) {
		return signIn
	}

	return kube.Error(err)
}
