package ichorgo

import (
	"context"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// The background monitor's view of the sensors: one number per node (its hottest sensor) and
// whether it is throttled, read for every node in one call like ClusterStorageHealth.

type clusterSensors struct {
	Context string             `json:"context"`
	Nodes   []nodeSensorHealth `json:"nodes"`
}

type nodeSensorHealth struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	// Hottest is the warmest temperature sensor; null on a node without any (a VM).
	Hottest   *temperatureSensor `json:"hottest"`
	Throttled bool               `json:"throttled"`
	Error     string             `json:"error,omitempty"` // the node could not be read
}

// ClusterSensorHealth reads, for every node of the context in parallel, its hottest
// temperature sensor (with the sensor's crit and max) and whether its cores are throttled:
// what the temperature alert of the background monitor needs (os:reader). A node that does
// not answer is reported with an error instead of failing the call.
func ClusterSensorHealth(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return demoSensorHealth(configYAML, contextName)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		result := clusterSensors{Context: name, Nodes: make([]nodeSensorHealth, len(nodes))}

		forEachNode(nodes, func(i int, node string) {
			result.Nodes[i] = readNodeSensorHealth(ctx, s, node)
		})

		return toJSON(result)
	})
}

// readNodeSensorHealth reads one node within nodeTimeout. Only the hwmon listing is required:
// without it the node is reported with an error, the rest is best effort.
func readNodeSensorHealth(ctx context.Context, s *session, node string) nodeSensorHealth {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)
	out := nodeSensorHealth{Node: node, Hostname: node}

	sensors := readSensors(nodeCtx, s, node)
	if e := sensors.Errors["hwmon"]; e != "" {
		out.Error = e

		return out
	}

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
		out.Hostname = hs.TypedSpec().Hostname
	}

	out.Hottest, out.Throttled = hottest(sensors.Temperatures), sensors.Throttled

	return out
}

// hottest is the warmest of temps, nil when there is none.
func hottest(temps []temperatureSensor) *temperatureSensor {
	var top *temperatureSensor

	for i := range temps {
		if top == nil || temps[i].Celsius > top.Celsius {
			top = &temps[i]
		}
	}

	return top
}

// demoSensorHealth is the demo cluster's: demo-worker-1 runs hot and throttled (demoSensors).
func demoSensorHealth(configYAML, contextName string) (string, error) {
	name, _, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	nodes := demoNodes()
	result := clusterSensors{Context: name, Nodes: make([]nodeSensorHealth, 0, len(nodes))}

	for _, n := range nodes {
		sensors := demoSensors(n)
		result.Nodes = append(result.Nodes, nodeSensorHealth{
			Node: n.Node, Hostname: n.Hostname, Hottest: hottest(sensors.Temperatures), Throttled: sensors.Throttled,
		})
	}

	return toJSON(result)
}
