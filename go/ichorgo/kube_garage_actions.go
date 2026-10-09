package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The Garage maintenance the app offers, all through `garage json-api` in a Garage pod (the
// node's own RPC secret, no admin token): what persistent block-resync errors touch, the
// safe repairs for them, and how hard each node's resync worker pushes.

const (
	// garageActionTimeout bounds a block report or a repair: one exec per errored block looked up.
	garageActionTimeout = 60 * time.Second
	// garageBlocksPerNode and garageBlocksTotal cap the blocks looked up with GetBlockInfo:
	// a cluster can have thousands failing, a sample tells what they are.
	garageBlocksPerNode = 10
	garageBlocksTotal   = 40
	garageLookups       = 6 // GetBlockInfo calls in flight
	// garageRetryChunk keeps a RetryBlockResync argv, sent in the exec URL, under ~100 KB.
	garageRetryChunk = 1000
	// Resync tranquility: 0 resyncs at full speed, 2 is Garage's default.
	garageTranquilityFull    = 0
	garageTranquilityDefault = 2
	garageTranquilityMax     = 100
)

// Block impacts: what a block that fails to resync would cost if it is lost.
const (
	garageImpactLive    = "live"      // a live object or upload references it
	garageImpactStale   = "stale-ref" // only a reference to deleted metadata keeps it
	garageImpactCleanup = "cleanup"   // deleted data awaiting garbage collection
	garageImpactUnknown = "unknown"   // not looked up, or the lookup failed
)

// garageExecLimits let ListBlockErrors answer: some 170 bytes per block, and a cluster that
// lost a node can have tens of thousands failing.
var garageExecLimits = execLimits{timeout: 50 * time.Second, maxOutput: 32 << 20}

var (
	garageNodeIDPattern    = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	garageBlockHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// garageTarget is the Garage container the app runs the CLI in.
type garageBlockReport struct {
	Errored            int               `json:"errored"`  // blocks failing to resync, every node
	Detailed           int               `json:"detailed"` // of which looked up
	Live               int               `json:"live"`
	CleanupOnly        int               `json:"cleanupOnly"`        // looked up, no live reference
	StaleRefs          int               `json:"staleRefs"`          // referenced by deleted metadata: block-refs repair
	RefcountMismatches int               `json:"refcountMismatches"` // refcount off: block-rc repair
	Retryable          int               `json:"retryable"`          // refcount 0: safe to retry now
	RepairsRunning     bool              `json:"repairsRunning"`     // a block-refs or block-rc repair is busy
	Nodes              []garageBlockNode `json:"nodes"`

	// retry is the errored blocks with no reference, by node: RetryBlockResync clears their backoff.
	retry map[string][]string
	// unreachable is set when a node did not answer: repairs then wait for the whole cluster.
	unreachable bool
}

type garageBlockNode struct {
	ID       string        `json:"id"`
	Hostname string        `json:"hostname"`
	Errored  int           `json:"errored"`
	Error    string        `json:"error"`
	Blocks   []garageBlock `json:"blocks"`
}

type garageBlock struct {
	Hash             string           `json:"hash"`
	Refcount         int64            `json:"refcount"`
	Errors           int64            `json:"errors"`
	LastTrySecs      int64            `json:"lastTrySecs"`
	NextTrySecs      int64            `json:"nextTrySecs"`
	Impact           string           `json:"impact"`
	StaleRef         bool             `json:"staleRef"`
	RefcountMismatch bool             `json:"refcountMismatch"`
	Refs             []garageBlockRef `json:"refs"`
	Error            string           `json:"error"`
}

type garageBlockRef struct {
	Kind     string `json:"kind"` // object|upload|version
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	UploadID string `json:"uploadId"`
	Version  string `json:"version"`
	Live     bool   `json:"live"`
}

type garageRepairResult struct {
	BlockRefs      bool     `json:"blockRefs"`      // block-refs repair launched on every node
	BlockRc        bool     `json:"blockRc"`        // block-rc repair launched on every node
	RepairsRunning bool     `json:"repairsRunning"` // one was already running: not launched again
	Unreachable    bool     `json:"unreachable"`    // a node did not answer: no repair launched
	Retried        int64    `json:"retried"`        // resyncs rescheduled
	Errors         []string `json:"errors"`
}

// KubeGarageBlockErrors tells what the blocks failing to resync in a Garage cluster are: the
// objects that reference them, live or deleted, and the metadata repairs they need (os:admin).
// namespace and pod name a ready Garage pod (the instance's "pod"). kubeServer: see KubePods.
func KubeGarageBlockErrors(configYAML, contextName, kubeServer, namespace, pod string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoGarageBlockReport())
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageBlockReport, error) {
			return readGarageBlockReport(ctx, run, t)
		})
	if err != nil {
		return "", err
	}

	return toJSON(report)
}

// KubeGarageRepairBlocks does the safe repairs for the blocks failing to resync (os:admin):
// a block-refs and a block-rc repair on every node when references or refcounts are off
// (not while one runs, nor with a node unreachable), and a resync retry, clearing the
// backoff, of the blocks nothing references any more. Answers a garageRepairResult.
func KubeGarageRepairBlocks(configYAML, contextName, kubeServer, namespace, pod string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "garage-repair-blocks", Namespace: namespace, Object: "Pod/" + pod})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	res, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod, repairGarageBlocks)
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(res)
}

// KubeGarageSetTranquility sets a node's resync tranquility (os:admin): 0 resyncs at full
// speed, 2 is Garage's default. garageNode is a Garage node ID, or "*" for every node.
func KubeGarageSetTranquility(configYAML, contextName, kubeServer, namespace, pod, garageNode string, value int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	garageNode = strings.TrimSpace(garageNode)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "garage-tranquility", Namespace: namespace, Object: "Pod/" + pod, Params: fmt.Sprintf("node=%s tranquility=%d", garageNode, value)})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return err
	}

	if garageNode != "*" && !garageNodeIDPattern.MatchString(garageNode) {
		return fmt.Errorf("invalid Garage garageNode ID %q", garageNode)
	}

	if value < 0 || value > garageTranquilityMax {
		return fmt.Errorf("resync tranquility must be between 0 and %d", garageTranquilityMax)
	}

	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	_, err = withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (struct{}, error) {
			return struct{}{}, setGarageTranquility(ctx, run, t, garageNode, value)
		})

	return kubeMutationError(err)
}

// withGarage runs fn against the Garage container of namespace/pod once it is sure the pod
// runs Garage and is ready: the app only ever runs Garage's CLI in a Garage container.
// Garage's own errors do not drop the Kubernetes client, which answered fine.
func withGarage[T any](target kubeTarget, namespace, pod string, fn func(context.Context, execFunc, garageTarget) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), garageActionTimeout)
	defer cancel()

	var garageErr error

	out, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (T, error) {
		var zero T

		t, err := garageTargetOf(ctx, k, namespace, pod)
		if err != nil {
			return zero, err
		}

		run := func(ctx context.Context, namespace, pod, container string, argv []string) ([]byte, []byte, error) {
			return k.execWith(ctx, garageExecLimits, namespace, pod, container, argv)
		}

		out, err := fn(ctx, run, t)
		garageErr = err

		return out, nil
	})
	if err != nil {
		return out, err
	}

	return out, garageErr
}

// garageTargetOf finds the Garage container of a pod, refusing a pod that does not run Garage.
func garageTargetOf(ctx context.Context, k *kubeClient, namespace, pod string) (garageTarget, error) {
	var p dsPod
	if err := k.get(ctx, podPath(namespace, pod), &p); err != nil {
		return garageTarget{}, err
	}

	groups := findGarageGroups([]dsPod{p})
	if len(groups) == 0 {
		return garageTarget{}, fmt.Errorf("pod %s/%s does not run Garage", namespace, pod)
	}

	container := groups[0].container
	if !p.containerReady(container) {
		return garageTarget{}, fmt.Errorf("Garage is not ready in pod %s/%s: refresh and try again", namespace, pod)
	}

	return garageTarget{namespace: namespace, pod: pod, container: container}, nil
}

func setGarageTranquility(ctx context.Context, run execFunc, t garageTarget, node string, value int) error {
	want := strconv.Itoa(value)

	res, err := garageCall[struct {
		Variable string `json:"variable"`
		Value    string `json:"value"`
	}](ctx, run, t, "SetWorkerVariable", node, map[string]string{"variable": garageTranquilityVar, "value": want})
	if err != nil {
		return err
	}

	if msg := firstGarageError(res.Error); msg != "" {
		return errors.New(msg)
	}

	if len(res.Success) == 0 {
		return errors.New("no Garage node set the resync tranquility")
	}

	for id, v := range res.Success {
		if v.Value != want {
			return fmt.Errorf("node %s kept resync tranquility %s", shortGarageID(id), v.Value)
		}
	}

	return nil
}

// readGarageBlockReport lists the blocks failing to resync on every node and looks up a
// sample of them: which objects reference them, and whether their metadata is consistent.
func readGarageBlockReport(ctx context.Context, run execFunc, t garageTarget) (garageBlockReport, error) {
	type blockError struct {
		BlockHash      string `json:"blockHash"`
		Refcount       int64  `json:"refcount"`
		ErrorCount     int64  `json:"errorCount"`
		LastTrySecsAgo int64  `json:"lastTrySecsAgo"`
		NextTryInSecs  int64  `json:"nextTryInSecs"`
	}

	var (
		errs    garageMulti[[]blockError]
		workers garageMulti[[]struct {
			Name string `json:"name"`
		}]
		statusOut           []byte
		errsErr, workersErr error
		wg                  sync.WaitGroup
	)

	wg.Go(func() { errs, errsErr = garageCall[[]blockError](ctx, run, t, "ListBlockErrors", "*", nil) })
	wg.Go(func() {
		workers, workersErr = garageCall[[]struct {
			Name string `json:"name"`
		}](ctx, run, t, "ListWorkers", "*", map[string]bool{"busyOnly": true})
	})
	wg.Go(func() { statusOut, _ = garageRun(ctx, run, t, garageCommands.status) })
	wg.Wait()

	if errsErr != nil {
		return garageBlockReport{}, errsErr
	}

	report := garageBlockReport{Nodes: []garageBlockNode{}, retry: map[string][]string{}, unreachable: len(errs.Error) > 0 || workersErr != nil}
	report.RepairsRunning = garageRepairBusy(workers.Success)
	hostnames := garageHostnames(statusOut)

	for id, msg := range errs.Error {
		report.Nodes = append(report.Nodes, garageBlockNode{ID: id, Hostname: hostnames[id], Error: msg, Blocks: []garageBlock{}})
	}

	type lookup struct{ node, block int }

	var lookups []lookup

	for id, list := range errs.Success {
		node := garageBlockNode{ID: id, Hostname: hostnames[id], Errored: len(list), Blocks: []garageBlock{}}
		report.Errored += len(list)

		// Referenced blocks first, the only ones that can back a live object, then those
		// failing the longest: they are the ones that will not heal alone.
		slices.SortFunc(list, func(a, b blockError) int {
			return cmp.Or(cmp.Compare(min(b.Refcount, 1), min(a.Refcount, 1)), cmp.Compare(b.ErrorCount, a.ErrorCount))
		})

		for _, e := range list {
			if !garageBlockHashPattern.MatchString(e.BlockHash) {
				continue
			}

			if e.Refcount == 0 {
				report.retry[id] = append(report.retry[id], e.BlockHash)
				report.Retryable++
			}

			if len(node.Blocks) < garageBlocksPerNode {
				node.Blocks = append(node.Blocks, garageBlock{
					Hash: e.BlockHash, Refcount: e.Refcount, Errors: e.ErrorCount,
					LastTrySecs: e.LastTrySecsAgo, NextTrySecs: e.NextTryInSecs, Impact: garageImpactUnknown, Refs: []garageBlockRef{},
				})
			}
		}

		report.Nodes = append(report.Nodes, node)
	}

	// Nodes with the most errors first, then by hostname.
	slices.SortFunc(report.Nodes, func(a, b garageBlockNode) int {
		if a.Errored != b.Errored {
			return b.Errored - a.Errored
		}

		return strings.Compare(a.Hostname+a.ID, b.Hostname+b.ID)
	})

	for n := range report.Nodes {
		for b := range report.Nodes[n].Blocks {
			if len(lookups) < garageBlocksTotal {
				lookups = append(lookups, lookup{n, b})
			}
		}
	}

	forEachLimit(lookups, garageLookups, func(_ int, l lookup) {
		node := &report.Nodes[l.node]

		lookupGarageBlock(ctx, run, t, node.ID, &node.Blocks[l.block])
	})

	for _, node := range report.Nodes {
		for _, b := range node.Blocks {
			if b.Impact == garageImpactUnknown {
				continue
			}

			report.Detailed++

			switch b.Impact {
			case garageImpactLive:
				report.Live++
			default:
				report.CleanupOnly++
			}

			if b.StaleRef {
				report.StaleRefs++
			}

			if b.RefcountMismatch {
				report.RefcountMismatches++
			}
		}
	}

	return report, nil
}

// lookupGarageBlock fills a block's references and impact from GetBlockInfo on its node.
func lookupGarageBlock(ctx context.Context, run execFunc, t garageTarget, node string, b *garageBlock) {
	type version struct {
		VersionID        string `json:"versionId"`
		RefDeleted       bool   `json:"refDeleted"`
		VersionDeleted   bool   `json:"versionDeleted"`
		GarbageCollected bool   `json:"garbageCollected"`
		Backlink         *struct {
			Object *struct {
				BucketID string `json:"bucketId"`
				Key      string `json:"key"`
			} `json:"object"`
			Upload *struct {
				UploadID string  `json:"uploadId"`
				BucketID *string `json:"bucketId"`
				Key      *string `json:"key"`
			} `json:"upload"`
		} `json:"backlink"`
	}

	res, err := garageCall[struct {
		Refcount int64     `json:"refcount"`
		Versions []version `json:"versions"`
	}](ctx, run, t, "GetBlockInfo", node, map[string]string{"blockHash": b.Hash})
	if err != nil {
		b.Error = err.Error()

		return
	}

	info, ok := res.Success[node]
	if !ok {
		b.Error = firstGarageError(res.Error)

		return
	}

	b.Refcount = info.Refcount
	live, refs := false, int64(0)

	for _, v := range info.Versions {
		deleted := v.RefDeleted || v.VersionDeleted || v.GarbageCollected
		live = live || !deleted

		if !v.RefDeleted {
			refs++

			// A reference kept alive by a version already deleted: block-refs fixes it.
			if v.VersionDeleted || v.GarbageCollected {
				b.StaleRef = true
			}
		}

		ref := garageBlockRef{Kind: "version", Version: v.VersionID, Live: !deleted}

		switch {
		case v.Backlink != nil && v.Backlink.Object != nil:
			ref.Kind, ref.Bucket, ref.Key = "object", v.Backlink.Object.BucketID, v.Backlink.Object.Key
		case v.Backlink != nil && v.Backlink.Upload != nil:
			u := v.Backlink.Upload
			ref.Kind, ref.UploadID = "upload", u.UploadID

			if u.BucketID != nil {
				ref.Bucket = *u.BucketID
			}

			if u.Key != nil {
				ref.Key = *u.Key
			}
		}

		b.Refs = append(b.Refs, ref)
	}

	b.RefcountMismatch = info.Refcount != refs

	switch {
	case live:
		b.Impact = garageImpactLive
	case b.StaleRef:
		b.Impact = garageImpactStale
	default:
		b.Impact = garageImpactCleanup
	}
}

// repairGarageBlocks is what garage-block-repair does by hand: metadata repairs when the
// sample shows inconsistent references or refcounts, and a retry of unreferenced blocks.
func repairGarageBlocks(ctx context.Context, run execFunc, t garageTarget) (garageRepairResult, error) {
	report, err := readGarageBlockReport(ctx, run, t)
	if err != nil {
		return garageRepairResult{}, err
	}

	res := garageRepairResult{Errors: []string{}, RepairsRunning: report.RepairsRunning, Unreachable: report.unreachable}

	if !report.unreachable && !report.RepairsRunning {
		for _, repair := range []struct {
			kind     string
			needed   bool
			launched *bool
		}{{"blockRefs", report.StaleRefs > 0, &res.BlockRefs}, {"blockRc", report.RefcountMismatches > 0, &res.BlockRc}} {
			if !repair.needed {
				continue
			}

			out, err := garageCall[json.RawMessage](ctx, run, t, "LaunchRepairOperation", "*", map[string]string{"repairType": repair.kind})

			switch {
			case err != nil:
				res.Errors = append(res.Errors, err.Error())
			case len(out.Error) > 0:
				res.Errors = append(res.Errors, repair.kind+": "+firstGarageError(out.Error))
			default:
				*repair.launched = true
			}
		}
	}

	errored := map[string]int{}
	for _, n := range report.Nodes {
		errored[n.ID] = n.Errored
	}

	for node, hashes := range report.retry {
		retried, err := retryGarageBlocks(ctx, run, t, node, hashes, len(hashes) == errored[node])
		res.Retried += retried

		if err != nil {
			res.Errors = append(res.Errors, err.Error())
		}
	}

	return res, nil
}

// retryGarageBlocks clears the backoff of a node's blocks so its resync worker tries them
// now: every errored block in one call when all are unreferenced, else hashes by chunks.
func retryGarageBlocks(ctx context.Context, run execFunc, t garageTarget, node string, hashes []string, all bool) (int64, error) {
	bodies := []any{map[string]bool{"all": true}}
	if !all {
		bodies = bodies[:0]
		for chunk := range slices.Chunk(hashes, garageRetryChunk) {
			bodies = append(bodies, map[string][]string{"blockHashes": chunk})
		}
	}

	var retried int64

	for _, body := range bodies {
		out, err := garageCall[struct {
			Count int64 `json:"count"`
		}](ctx, run, t, "RetryBlockResync", node, body)
		if err != nil {
			return retried, err
		}

		retried += out.Success[node].Count

		if msg := firstGarageError(out.Error); msg != "" {
			return retried, errors.New(msg)
		}
	}

	return retried, nil
}

// garageRepairBusy tells whether a block-refs or block-rc repair worker runs on any node.
func garageRepairBusy(workers map[string][]struct {
	Name string `json:"name"`
}) bool {
	for _, list := range workers {
		for _, w := range list {
			name := strings.ToLower(w.Name)
			if strings.Contains(name, "block_ref repair") || strings.Contains(name, "refcount repair") {
				return true
			}
		}
	}

	return false
}

func garageHostnames(statusOut []byte) map[string]string {
	var st garageClusterStatus

	out := map[string]string{}
	if json.Unmarshal(statusOut, &st) == nil {
		for _, n := range st.Nodes {
			out[n.ID] = n.Hostname
		}
	}

	return out
}

// firstGarageError is one node's error, the first in node ID order, prefixed with that node.
func firstGarageError(errs map[string]string) string {
	if len(errs) == 0 {
		return ""
	}

	ids := slices.Sorted(maps.Keys(errs))

	return "node " + shortGarageID(ids[0]) + ": " + errs[ids[0]]
}

func shortGarageID(id string) string {
	if len(id) > 16 {
		return id[:16]
	}

	return id
}
