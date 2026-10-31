package flux

import (
	"context"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/kube"
)

// Env is what reading Flux needs: the client, and what comes from domains still in the facade.
type Env struct {
	kube.Client

	// Error is the message for an error of a Kubernetes call; nil is kube.Error. The facade's
	// also reduces a needed sign-in to its own message, which the apps recognise.
	Error func(error) error
	// Version is the Flux release the cluster runs, "" when unknown; nil is unknown.
	Version func(ctx context.Context) string
	// UnhealthyPods reads the pods of namespaces that are not healthy; nil shows none.
	UnhealthyPods func(ctx context.Context, namespaces []string) ([]kube.Pod, error)
}

func (e Env) version(ctx context.Context) string {
	if e.Version == nil {
		return ""
	}

	return e.Version(ctx)
}

// sectionError is the message of a section that failed, for its "error" field.
func (e Env) sectionError(errs ...error) string {
	message := e.Error
	if message == nil {
		message = kube.Error
	}

	var msgs []string

	for _, err := range errs {
		if err != nil {
			msgs = append(msgs, message(err).Error())
		}
	}

	return strings.Join(slices.Compact(msgs), "; ")
}
