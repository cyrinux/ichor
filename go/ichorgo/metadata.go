//lint:file-ignore SA1019 the per-node metadata is still how a multiplexed answer reports its node and error

package ichorgo

import "github.com/siderolabs/talos/pkg/machinery/api/common"

// Talos marks the per-node metadata of an answer deprecated (it goes away with server-side
// multiplexing), but it is still how a node reports its own failure and name when several
// are asked at once. The deprecated accessors are referenced here only.

// metaError is the error the node reported instead of an answer, "" when it answered.
func metaError(m *common.Metadata) string {
	return m.GetError()
}

// metaHost is the node the answer came from, "" for a single-node call.
func metaHost(m *common.Metadata) string {
	return m.GetHostname()
}
