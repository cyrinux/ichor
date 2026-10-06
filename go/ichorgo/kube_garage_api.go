package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type garageTarget struct{ namespace, pod, container string }

// garageMulti is the answer of a node-scoped admin API endpoint: per node ID, a result or an error.
type garageMulti[T any] struct {
	Success map[string]T      `json:"success"`
	Error   map[string]string `json:"error"`
}

// garageCall runs a node-scoped admin endpoint on node (an ID or "*") through the CLI.
// Callers pass fixed endpoints; body is encoded as JSON, never interpreted by a shell.
func garageCall[T any](ctx context.Context, run execFunc, t garageTarget, endpoint, node string, body any) (garageMulti[T], error) {
	var out garageMulti[T]

	envelope, err := json.Marshal(struct {
		Node string `json:"node"`
		Body any    `json:"body"`
	}{node, body})
	if err != nil {
		return out, err
	}

	argv := []string{"/garage", "json-api", endpoint, string(envelope)}

	for attempt := 0; ; attempt++ {
		stdout, stderr, err := run(ctx, t.namespace, t.pod, t.container, argv)
		if err != nil {
			return out, fmt.Errorf("%s: %w", endpoint, garageCommandError(err, stderr))
		}

		err = json.Unmarshal(stdout, &out)
		if err == nil {
			return out, nil
		}

		// A large answer sometimes arrives cut short, the exec stream closed before the
		// last frames: a read is asked again, a write never is.
		var syntaxErr *json.SyntaxError
		if garageReads[endpoint] && attempt < garageReadRetries && errors.As(err, &syntaxErr) && ctx.Err() == nil {
			out = garageMulti[T]{}

			continue
		}

		return out, fmt.Errorf("unexpected %s answer (%v, %d bytes): %q", endpoint, err, len(stdout), clipUTF8(string(stdout), 200))
	}
}

// garageReads are the endpoints garageCall may run again: they change nothing.
var garageReads = map[string]bool{"ListBlockErrors": true, "GetBlockInfo": true, "ListWorkers": true}

const garageReadRetries = 2

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// garageCommandError keeps the CLI's own "Error: ..." line: its stderr is mostly logs.
func garageCommandError(err error, stderr []byte) error {
	var execErr *kubeExecError
	if !errors.As(err, &execErr) {
		return err
	}

	for line := range strings.SplitSeq(ansiEscape.ReplaceAllString(string(stderr), ""), "\n") {
		if msg, ok := strings.CutPrefix(strings.TrimSpace(line), "Error: "); ok {
			return errors.New(msg)
		}
	}

	return err
}

// garageRun runs one fixed CLI command in the Garage container and returns its stdout.
func garageRun(ctx context.Context, run execFunc, t garageTarget, argv []string) ([]byte, error) {
	stdout, stderr, err := run(ctx, t.namespace, t.pod, t.container, argv)

	return stdout, garageCommandError(err, stderr)
}

// garageClusterStatus is GetClusterStatus: the layout and every known node.
type garageClusterStatus struct {
	LayoutVersion int64               `json:"layoutVersion"`
	Nodes         []garageClusterNode `json:"nodes"`
}

type garageClusterNode struct {
	ID              string `json:"id"`
	Hostname        string `json:"hostname"`
	GarageVersion   string `json:"garageVersion"`
	IsUp            bool   `json:"isUp"`
	LastSeenSecsAgo *int64 `json:"lastSeenSecsAgo"`
	Draining        bool   `json:"draining"`
	DataPartition   *struct {
		Available int64 `json:"available"`
		Total     int64 `json:"total"`
	} `json:"dataPartition"`
	Role *struct {
		Zone string   `json:"zone"`
		Tags []string `json:"tags"`
	} `json:"role"`
}

// garageNodeStats is one node's GetNodeStatistics answer: the block resync and table queues.
type garageNodeStats struct {
	BlockManagerStats struct {
		ResyncErrors   int64 `json:"resyncErrors"`
		ResyncQueueLen int64 `json:"resyncQueueLen"`
	} `json:"blockManagerStats"`
	TableStats []struct {
		InsertQueueLen int64 `json:"insertQueueLen"`
		MerkleQueueLen int64 `json:"merkleQueueLen"`
		GcQueueLen     int64 `json:"gcQueueLen"`
	} `json:"tableStats"`
}
