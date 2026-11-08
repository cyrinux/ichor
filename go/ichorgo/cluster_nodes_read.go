package ichorgo

import "context"

// nodeReadings is one reading per node of a context, as the background monitor's
// ClusterSensorHealth and ClusterStorageHealth return it.
type nodeReadings[T any] struct {
	Context string `json:"context"`
	Nodes   []T    `json:"nodes"`
}

// readClusterNodes runs read for every node of the context in parallel; a demo context
// answers with demo instead. read reports a node that does not answer in its own result,
// so one node never fails the call.
func readClusterNodes[T any](configYAML, contextName string, demo func(string, string) (string, error), read func(context.Context, *session, string) T) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return demo(configYAML, contextName)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		result := nodeReadings[T]{Context: name, Nodes: make([]T, len(nodes))}

		forEachNode(nodes, func(i int, node string) {
			result.Nodes[i] = read(ctx, s, node)
		})

		return toJSON(result)
	})
}
