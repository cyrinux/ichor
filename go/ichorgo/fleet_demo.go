package ichorgo

// The demo context's fleet: five made-up clusters, one of each kind of row, for the fleet
// screen of the demo and the store screenshots. No real cluster, company or account name.

const fleetMinute = 60 * 1000

// DemoFleetSummary is FleetSummary of a made-up fleet as of nowMillis: one bad cluster with an
// unreachable node, one warn with a firing warning alert, one not checked for two hours and
// two fine. Its names are invented, so the result is not masked.
func DemoFleetSummary(nowMillis int64) string {
	out, err := toJSON(summarizeFleet(demoFleetEntries(nowMillis), nowMillis))
	if err != nil {
		return `{"clusters":[],"counts":{"bad":0,"warn":0,"unknown":0,"ok":0},"worst":"unknown","groups":[]}`
	}

	return out
}

// demoFleetEntries is the demo fleet's input, as the apps would build it, checked relative to
// nowMillis.
func demoFleetEntries(nowMillis int64) []fleetEntry {
	ready := func(host string) fleetNode { return fleetNode{Hostname: host, Health: "READY"} }

	return []fleetEntry{
		{
			Key: "demo-lab", Name: "lab", Context: "lab", Tags: []string{},
			TakenAt: nowMillis - 2*60*fleetMinute,
			Nodes:   map[string]fleetNode{"10.0.0.31": ready("lab-1")}, EtcdChecked: true, EtcdAlarms: []string{},
		},
		{
			Key: "demo-prod-eu", Name: "prod-eu", Context: "prod-eu", Tags: []string{"prod", "eu"},
			TakenAt: nowMillis - 4*fleetMinute, Failures: 0,
			Nodes: map[string]fleetNode{
				"10.0.0.11": ready("prod-eu-cp-1"),
				"10.0.0.12": ready("prod-eu-cp-2"),
				"10.0.0.13": {Hostname: "prod-eu-worker-1", Health: "UNREACHABLE"},
			},
			EtcdChecked: true, EtcdAlarms: []string{},
			AmChecked: true, AmIssues: map[string]string{"4f2a9c01": "critical|KubeNodeNotReady|prod-eu-worker-1"},
		},
		{
			Key: "demo-staging", Name: "staging", Context: "staging", Tags: []string{"staging"},
			TakenAt: nowMillis - 6*fleetMinute,
			Nodes: map[string]fleetNode{
				"10.0.1.11": ready("staging-cp-1"),
				"10.0.1.12": ready("staging-worker-1"),
			},
			EtcdChecked: true, EtcdAlarms: []string{},
			AmChecked: true, AmIssues: map[string]string{"7c31e8d2": "warning|KubePodCrashLooping|shop/checkout"},
			GitopsChecked: true, GitopsIssues: map[string]string{},
		},
		{
			Key: "demo-edge", Name: "edge", Context: "edge", Tags: []string{"prod"},
			TakenAt: nowMillis - 3*fleetMinute,
			Nodes:   map[string]fleetNode{"10.0.2.11": ready("edge-1")}, EtcdChecked: true, EtcdAlarms: []string{},
			StorageChecked: true, StorageIssues: map[string]string{},
		},
		{
			Key: "demo-dev", Name: "dev", Context: "dev", Kube: true, Tags: []string{},
			TakenAt: nowMillis - 5*fleetMinute,
			Nodes: map[string]fleetNode{
				"dev-node-1": ready("dev-node-1"),
				"dev-node-2": ready("dev-node-2"),
			},
		},
	}
}
