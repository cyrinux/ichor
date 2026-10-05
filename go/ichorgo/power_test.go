package ichorgo

import (
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

func TestValidatePowerTarget(t *testing.T) {
	ctx := &clientconfig.Context{
		Endpoints: []string{"talos.example.org"},
		Nodes:     []string{"10.0.0.2", "10.0.0.3"},
	}

	if err := validatePowerTarget(ctx, "10.0.0.3"); err != nil {
		t.Errorf("known node rejected: %v", err)
	}

	// An empty node would make the request hit the endpoint itself.
	for _, node := range []string{"", "  ", "10.0.0.9", "talos.example.org"} {
		if err := validatePowerTarget(ctx, node); err == nil {
			t.Errorf("node %q should be rejected", node)
		}
	}
}

func TestValidatePowerTargetEndpointsAsNodes(t *testing.T) {
	ctx := &clientconfig.Context{Endpoints: []string{"10.1.0.1"}}

	if err := validatePowerTarget(ctx, "10.1.0.1"); err != nil {
		t.Errorf("endpoint-as-node rejected: %v", err)
	}
}

func TestParseRebootMode(t *testing.T) {
	cases := map[string]machineapi.RebootRequest_Mode{
		"":           machineapi.RebootRequest_DEFAULT,
		"default":    machineapi.RebootRequest_DEFAULT,
		"powercycle": machineapi.RebootRequest_POWERCYCLE,
		"FORCE":      machineapi.RebootRequest_FORCE,
	}

	for in, want := range cases {
		got, err := parseRebootMode(in)
		if err != nil || got != want {
			t.Errorf("parseRebootMode(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	if _, err := parseRebootMode("kexec"); err == nil {
		t.Error("unknown mode must be rejected, not silently mapped to default")
	}
}
