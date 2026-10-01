package talosmobile

import (
	"testing"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

func TestDefragTargetMustBeInContext(t *testing.T) {
	ctx := &clientconfig.Context{Endpoints: []string{"e"}, Nodes: []string{"10.0.0.2"}}

	if err := validatePowerTarget(ctx, "10.0.0.2"); err != nil {
		t.Errorf("known node rejected: %v", err)
	}

	// Without a node, the request would go to the endpoint itself.
	if err := validatePowerTarget(ctx, ""); err == nil {
		t.Error("empty node accepted")
	}
}
