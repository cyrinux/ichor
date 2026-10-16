package ichorgo

import "time"

// demoClusterAPI is what the demo management cluster answers (v1beta2, Talos providers, Sidero
// Metal): a healthy production cluster, a staging cluster whose control plane is still being
// provisioned (one machine stuck for two hours), a lab cluster that failed, and five bare-metal
// servers, one never accepted and one powered off while in use. Everything invented.
func demoClusterAPI() capiReport {
	now := time.Now()
	ago := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	machine := func(cluster, name, deployment, phase, node, addr string, controlPlane bool, created time.Duration) capiMachine {
		running := phase == "Running"

		return capiMachine{
			Namespace: "fleet", Name: name, Cluster: cluster, ControlPlane: controlPlane, Deployment: deployment,
			Phase: phase, Node: node, Version: "v1.34.1", ProviderID: providerIDOf(node), Addresses: addressesOf(addr),
			BootstrapReady: running, InfrastructureReady: running,
			LastUpdated: ago(created - time.Minute), CreatedAt: ago(created), Conditions: []capiCondition{},
		}
	}

	stuck := machine("staging", "staging-cp-x7k2p", "", capiPhaseProvisioning, "", "", true, 2*time.Hour)
	stuck.Stuck, stuck.BootstrapReady = true, true
	stuck.Conditions = []capiCondition{{Type: "InfrastructureReady", Status: "False", Reason: "WaitingForServer", Message: "no server available in server class large"}}

	failed := machine("old-lab", "old-lab-cp-q9d4m", "", capiPhaseFailed, "", "10.0.2.10", true, 72*time.Hour)
	failed.FailureReason, failed.FailureMessage = "CreateError", "server srv-b2 did not boot from disk"

	r := emptyClusterAPI()
	r.Installed, r.Version, r.Sidero, r.TalosControlPlane, r.TalosBootstrap = true, "v1beta2", true, true, true

	r.Clusters = []capiCluster{
		{
			Namespace: "fleet", Name: "prod-edge", Phase: capiPhaseProvisioned, Endpoint: "10.0.0.5:6443",
			InfraKind: "MetalCluster", ControlPlaneKind: "TalosControlPlane", InfrastructureReady: true, ControlPlaneReady: true,
			ControlPlane: &capiReplicas{Desired: 3, Ready: 3, Available: 3, UpToDate: 3},
			Workers:      capiReplicas{Desired: 4, Ready: 4, Available: 4, UpToDate: 4},
			Versions:     []string{"v1.34.1"}, Conditions: []capiCondition{},
		},
		{
			Namespace: "fleet", Name: "staging", Phase: capiPhaseProvisioning, Endpoint: "10.0.1.5:6443",
			InfraKind: "MetalCluster", ControlPlaneKind: "TalosControlPlane", InfrastructureReady: true,
			ControlPlane: &capiReplicas{Desired: 1},
			Workers:      capiReplicas{Desired: 4, Ready: 2, Available: 2, UpToDate: 2},
			Versions:     []string{"v1.34.1"},
			Conditions: []capiCondition{{
				Type: "ControlPlaneInitialized", Status: "False", Reason: "NotInitialized", Message: "waiting for the first control plane machine",
			}},
		},
		{
			Namespace: "fleet", Name: "old-lab", Phase: capiPhaseFailed, Endpoint: "10.0.2.5:6443",
			InfraKind: "MetalCluster", ControlPlaneKind: "TalosControlPlane", InfrastructureReady: true,
			ControlPlane: &capiReplicas{Desired: 1}, Versions: []string{"v1.33.4"}, Conditions: []capiCondition{},
			FailureReason: "CreateError", FailureMessage: "the control plane machine failed to provision",
		},
	}

	r.Machines = []capiMachine{
		machine("prod-edge", "prod-edge-cp-4fj8s", "", "Running", "prod-edge-cp-1", "10.0.0.11", true, 30*24*time.Hour),
		machine("prod-edge", "prod-edge-cp-9wq2n", "", "Running", "prod-edge-cp-2", "10.0.0.12", true, 30*24*time.Hour),
		machine("prod-edge", "prod-edge-cp-tz5c7", "", "Running", "prod-edge-cp-3", "10.0.0.13", true, 30*24*time.Hour),
		machine("prod-edge", "prod-edge-workers-6b8d-k2m9x", "prod-edge-workers", "Running", "prod-edge-w-1", "10.0.0.21", false, 12*24*time.Hour),
		machine("prod-edge", "prod-edge-workers-6b8d-p4r7t", "prod-edge-workers", "Running", "prod-edge-w-2", "10.0.0.22", false, 12*24*time.Hour),
		machine("prod-edge", "prod-edge-workers-6b8d-v8h3q", "prod-edge-workers", "Running", "prod-edge-w-3", "10.0.0.23", false, 12*24*time.Hour),
		machine("prod-edge", "prod-edge-workers-6b8d-z1n6w", "prod-edge-workers", "Running", "prod-edge-w-4", "10.0.0.24", false, 12*24*time.Hour),
		stuck,
		failed,
	}

	r.MachineDeployments = []capiMachineDeployment{
		{
			Namespace: "fleet", Name: "prod-edge-workers", Cluster: "prod-edge", Phase: "Running",
			Desired: 4, Ready: 4, Available: 4, UpToDate: 4, Version: "v1.34.1",
		},
		{
			Namespace: "fleet", Name: "staging-workers", Cluster: "staging", Phase: "ScalingUp",
			Desired: 4, Ready: 2, Available: 2, UpToDate: 2, Unavailable: 2, Version: "v1.34.1",
		},
	}

	server := func(name, host, power string, accepted, inUse, ready, clean bool, classes ...string) sideroServer {
		return sideroServer{
			Name: name, Hostname: host, Accepted: accepted, Ready: ready, InUse: inUse, IsClean: clean, Power: power,
			Addresses: []string{}, ServerClasses: classes, CPU: "AMD EPYC 7313P, 16 cores", MemoryBytes: 128 << 30,
		}
	}

	r.Servers = []sideroServer{
		server("srv-a1", "rack1-u01", "on", true, true, true, false, "any"),
		server("srv-a2", "rack1-u02", "on", true, true, true, false, "any", "large"),
		server("srv-b1", "rack2-u01", "unknown", false, false, false, false),
		server("srv-b2", "rack2-u02", sideroPowerOff, true, true, false, false, "any"),
		server("srv-c1", "rack3-u01", "on", true, false, false, true, "any"),
	}
	r.Servers[0].Addresses, r.Servers[1].Addresses = []string{"10.0.0.11"}, []string{"10.0.0.12"}

	r.ServerClasses = []sideroServerClass{{Name: "any", Available: 1, InUse: 3}, {Name: "large", Available: 0, InUse: 1}}

	finishClusterAPI(&r)

	return r
}

func providerIDOf(node string) string {
	if node == "" {
		return ""
	}

	return "sidero://" + node
}

func addressesOf(addr string) []string {
	if addr == "" {
		return []string{}
	}

	return []string{addr}
}
