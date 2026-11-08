package helm

import (
	"context"

	"github.com/cyrinux/ichor/go/kube"
)

// Env is what a rollback needs: the client, and what comes from domains still in the facade.
type Env struct {
	kube.Client

	// Error is the message for an error of a Kubernetes call; nil is kube.Error. The facade's
	// also reduces a needed sign-in to its own message, which the apps recognise.
	Error func(error) error
	// Fatal tells that reading again cannot help; nil is kube.FollowFatal.
	Fatal func(error) bool
	// Rollout reads the rollout of a workload, for the follow.
	Rollout func(ctx context.Context, kind, namespace, name string) (RolloutState, error)
}

// RolloutState is what the follow reads of a workload's rollout.
type RolloutState struct {
	Ready, Desired int
	// Done: every pod runs the latest template and is ready.
	Done bool
	// Failed: the workload exceeded its progress deadline.
	Failed bool
	// Manual: pods are replaced only when deleted (OnDelete, StatefulSet partition).
	Manual bool
	// Stale: the controller has not observed the latest spec yet, so Done and Failed may still
	// describe the previous rollout.
	Stale bool
}

func (e Env) apiError(err error) error {
	if e.Error != nil {
		return e.Error(err)
	}

	return kube.Error(err)
}

func (e Env) fatal(err error) bool {
	if e.Fatal != nil {
		return e.Fatal(err)
	}

	return kube.FollowFatal(err)
}
