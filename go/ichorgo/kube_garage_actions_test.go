package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	nodeA = "aaaaaaaaaaaaaaaa1111111111111111aaaaaaaaaaaaaaaa1111111111111111"
	nodeB = "bbbbbbbbbbbbbbbb2222222222222222bbbbbbbbbbbbbbbb2222222222222222"
	nodeC = "cccccccccccccccc3333333333333333cccccccccccccccc3333333333333333"
)

func blockHash(c byte) string { return strings.Repeat(string(c), 64) }

// fakeGarageAPI answers `garage json-api ENDPOINT {node, body}` from canned answers keyed by
// "ENDPOINT NODE" (or "ENDPOINT NODE HASH" for GetBlockInfo), and records every call.
type fakeGarageAPI struct {
	t       *testing.T
	answers map[string]string
	mu      sync.Mutex
	calls   []string
}

func (f *fakeGarageAPI) run(_ context.Context, namespace, pod, container string, argv []string) ([]byte, []byte, error) {
	if namespace != "storage" || pod != "garage-0" || container != "garage" {
		f.t.Errorf("exec target %s/%s/%s", namespace, pod, container)
	}

	if slices.Equal(argv, garageCommands.status) {
		return []byte(`{"nodes":[{"id":"` + nodeA + `","hostname":"garage-a"},{"id":"` + nodeB + `","hostname":"garage-b"}]}`), nil, nil
	}

	if len(argv) != 4 || argv[0] != "/garage" || argv[1] != "json-api" {
		f.t.Errorf("unexpected argv %v", argv)

		return nil, nil, errors.New("not allowed")
	}

	var env struct {
		Node string          `json:"node"`
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal([]byte(argv[3]), &env); err != nil {
		f.t.Fatalf("envelope %q: %v", argv[3], err)
	}

	key := argv[2] + " " + env.Node
	if argv[2] == "GetBlockInfo" {
		var body struct {
			BlockHash string `json:"blockHash"`
		}
		_ = json.Unmarshal(env.Body, &body)
		key += " " + body.BlockHash
	}

	f.mu.Lock()
	f.calls = append(f.calls, argv[2]+" "+env.Node+" "+string(env.Body))
	answer, ok := f.answers[key]
	f.mu.Unlock()

	if !ok {
		f.t.Errorf("no answer for %s", key)

		return nil, nil, errors.New("no answer")
	}

	return []byte(answer), nil, nil
}

func (f *fakeGarageAPI) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string

	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}

	return out
}

var testGarageTarget = garageTarget{namespace: "storage", pod: "garage-0", container: "garage"}

// garageBlockAnswers: node A has a live block and a stale-ref block whose refcount is off;
// node B has deleted data awaiting cleanup (refcount 0). unreachableC adds a node that failed.
func garageBlockAnswers(unreachableC, busy bool) map[string]string {
	listErr := `{}`
	if unreachableC {
		listErr = `{"` + nodeC + `":"Network error: Not connected"}`
	}

	workers := `[]`
	if busy {
		workers = `[{"name":"Block refcount repair worker"}]`
	}

	return map[string]string{
		"ListBlockErrors *": `{"success":{
			"` + nodeA + `":[
				{"blockHash":"` + blockHash('1') + `","refcount":1,"errorCount":3,"lastTrySecsAgo":10,"nextTryInSecs":60},
				{"blockHash":"` + blockHash('2') + `","refcount":1,"errorCount":9,"lastTrySecsAgo":20,"nextTryInSecs":120}],
			"` + nodeB + `":[
				{"blockHash":"` + blockHash('3') + `","refcount":0,"errorCount":5,"lastTrySecsAgo":30,"nextTryInSecs":90}]},
			"error":` + listErr + `}`,
		"ListWorkers *": `{"success":{"` + nodeA + `":` + workers + `},"error":{}}`,
		"GetBlockInfo " + nodeA + " " + blockHash('1'): `{"success":{"` + nodeA + `":{"blockHash":"x","refcount":1,"versions":[
			{"versionId":"v1","refDeleted":false,"versionDeleted":false,"garbageCollected":false,"backlink":{"object":{"bucketId":"b1","key":"photos/a.jpg"}}}]}},"error":{}}`,
		"GetBlockInfo " + nodeA + " " + blockHash('2'): `{"success":{"` + nodeA + `":{"blockHash":"x","refcount":2,"versions":[
			{"versionId":"v2","refDeleted":false,"versionDeleted":true,"garbageCollected":false,"backlink":{"upload":{"uploadId":"u1","uploadDeleted":true,"uploadGarbageCollected":false,"bucketId":"b1","key":"big.tar"}}}]}},"error":{}}`,
		"GetBlockInfo " + nodeB + " " + blockHash('3'): `{"success":{"` + nodeB + `":{"blockHash":"x","refcount":0,"versions":[
			{"versionId":"v3","refDeleted":true,"versionDeleted":true,"garbageCollected":true,"backlink":null}]}},"error":{}}`,
		"LaunchRepairOperation *":   `{"success":{"` + nodeA + `":null,"` + nodeB + `":null},"error":{}}`,
		"RetryBlockResync " + nodeB: `{"success":{"` + nodeB + `":{"count":1}},"error":{}}`,
	}
}

func TestReadGarageBlockReport(t *testing.T) {
	f := &fakeGarageAPI{t: t, answers: garageBlockAnswers(true, false)}

	report, err := readGarageBlockReport(context.Background(), f.run, testGarageTarget)
	if err != nil {
		t.Fatal(err)
	}

	if report.Errored != 3 || report.Detailed != 3 || report.Live != 1 || report.CleanupOnly != 2 ||
		report.StaleRefs != 1 || report.RefcountMismatches != 1 || report.Retryable != 1 || !report.unreachable || report.RepairsRunning {
		t.Fatalf("counts: %+v", report)
	}

	if len(report.Nodes) != 3 || report.Nodes[0].Hostname != "garage-a" || report.Nodes[0].Errored != 2 {
		t.Fatalf("nodes, most errors first: %+v", report.Nodes)
	}

	if report.Nodes[2].ID != nodeC || report.Nodes[2].Error == "" {
		t.Errorf("unreachable node: %+v", report.Nodes[2])
	}

	a := report.Nodes[0].Blocks
	if a[0].Hash != blockHash('2') || a[0].Impact != garageImpactStale || !a[0].StaleRef || !a[0].RefcountMismatch {
		t.Errorf("longest failing block first, stale: %+v", a[0])
	}

	if a[1].Impact != garageImpactLive || len(a[1].Refs) != 1 || a[1].Refs[0].Kind != "object" || a[1].Refs[0].Key != "photos/a.jpg" || !a[1].Refs[0].Live {
		t.Errorf("live block: %+v", a[1])
	}

	if a[0].Refs[0].Kind != "upload" || a[0].Refs[0].UploadID != "u1" || a[0].Refs[0].Key != "big.tar" {
		t.Errorf("upload backlink: %+v", a[0].Refs)
	}

	if b := report.Nodes[1].Blocks[0]; b.Impact != garageImpactCleanup || b.StaleRef || b.RefcountMismatch {
		t.Errorf("cleanup block: %+v", b)
	}

	if got := report.retry[nodeB]; !slices.Equal(got, []string{blockHash('3')}) {
		t.Errorf("retry: %v", report.retry)
	}
}

func TestRepairGarageBlocks(t *testing.T) {
	f := &fakeGarageAPI{t: t, answers: garageBlockAnswers(false, false)}

	res, err := repairGarageBlocks(context.Background(), f.run, testGarageTarget)
	if err != nil {
		t.Fatal(err)
	}

	if !res.BlockRefs || !res.BlockRc || res.Retried != 1 || len(res.Errors) != 0 || res.Unreachable || res.RepairsRunning {
		t.Fatalf("result: %+v", res)
	}

	launched := f.called("LaunchRepairOperation")
	if !slices.Contains(launched, `LaunchRepairOperation * {"repairType":"blockRefs"}`) || !slices.Contains(launched, `LaunchRepairOperation * {"repairType":"blockRc"}`) {
		t.Errorf("repairs: %v", launched)
	}

	// Every block failing on node B is unreferenced: one call retries them all.
	if got := f.called("RetryBlockResync"); !slices.Equal(got, []string{"RetryBlockResync " + nodeB + ` {"all":true}`}) {
		t.Errorf("retry: %v", got)
	}
}

func TestGarageCallRetriesTruncatedReads(t *testing.T) {
	calls := map[string]int{}
	run := func(_ context.Context, _, _, _ string, argv []string) ([]byte, []byte, error) {
		calls[argv[2]]++
		if calls[argv[2]] == 1 {
			return []byte(`{"success":{"` + nodeA + `":[`), nil, nil
		}

		return []byte(`{"success":{"` + nodeA + `":[]},"error":{}}`), nil, nil
	}

	if _, err := garageCall[[]struct{}](context.Background(), run, testGarageTarget, "ListBlockErrors", "*", nil); err != nil || calls["ListBlockErrors"] != 2 {
		t.Errorf("read: %d calls, %v", calls["ListBlockErrors"], err)
	}

	if _, err := garageCall[struct{}](context.Background(), run, testGarageTarget, "SetWorkerVariable", nodeA, nil); err == nil || calls["SetWorkerVariable"] != 1 {
		t.Errorf("write asked again: %d calls, %v", calls["SetWorkerVariable"], err)
	}
}

func TestRetryGarageBlocksByChunks(t *testing.T) {
	f := &fakeGarageAPI{t: t, answers: map[string]string{
		"RetryBlockResync " + nodeA: `{"success":{"` + nodeA + `":{"count":1000}},"error":{}}`,
	}}

	hashes := make([]string, garageRetryChunk+500)
	for i := range hashes {
		hashes[i] = blockHash('1')
	}

	retried, err := retryGarageBlocks(context.Background(), f.run, testGarageTarget, nodeA, hashes, false)
	if err != nil || retried != 2000 {
		t.Fatalf("retried %d, %v", retried, err)
	}

	calls := f.called("RetryBlockResync")
	if len(calls) != 2 || !strings.Contains(calls[0], `"blockHashes":[`) || strings.Contains(calls[0], `"all"`) {
		t.Errorf("calls: %d", len(calls))
	}
}

func TestRepairGarageBlocksHoldsMetadataRepairs(t *testing.T) {
	for name, answers := range map[string]map[string]string{
		"node unreachable": garageBlockAnswers(true, false),
		"repair running":   garageBlockAnswers(false, true),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeGarageAPI{t: t, answers: answers}

			res, err := repairGarageBlocks(context.Background(), f.run, testGarageTarget)
			if err != nil {
				t.Fatal(err)
			}

			if res.BlockRefs || res.BlockRc || len(f.called("LaunchRepairOperation")) != 0 {
				t.Errorf("metadata repair launched: %+v", res)
			}

			if res.Retried != 1 || res.Unreachable != (name == "node unreachable") || res.RepairsRunning != (name == "repair running") {
				t.Errorf("result: %+v", res)
			}
		})
	}
}

func TestSetGarageTranquility(t *testing.T) {
	f := &fakeGarageAPI{t: t, answers: map[string]string{
		"SetWorkerVariable " + nodeA: `{"success":{"` + nodeA + `":{"variable":"resync-tranquility","value":"0"}},"error":{}}`,
		"SetWorkerVariable " + nodeB: `{"success":{"` + nodeB + `":{"variable":"resync-tranquility","value":"2"}},"error":{}}`,
		"SetWorkerVariable *":        `{"success":{},"error":{"` + nodeC + `":"Network error"}}`,
	}}

	if err := setGarageTranquility(context.Background(), f.run, testGarageTarget, nodeA, garageTranquilityFull); err != nil {
		t.Fatal(err)
	}

	if got := f.called("SetWorkerVariable " + nodeA); !slices.Equal(got, []string{"SetWorkerVariable " + nodeA + ` {"value":"0","variable":"resync-tranquility"}`}) {
		t.Errorf("call: %v", got)
	}

	if err := setGarageTranquility(context.Background(), f.run, testGarageTarget, nodeB, garageTranquilityFull); err == nil || !strings.Contains(err.Error(), "kept resync tranquility 2") {
		t.Errorf("value not applied: %v", err)
	}

	if err := setGarageTranquility(context.Background(), f.run, testGarageTarget, "*", garageTranquilityDefault); err == nil || !strings.Contains(err.Error(), "Network error") {
		t.Errorf("node error: %v", err)
	}
}

func TestKubeGarageActionsValidate(t *testing.T) {
	for _, tc := range []struct {
		node  string
		value int
	}{{"not-an-id", 0}, {"abc", 0}, {nodeA, -1}, {nodeA, garageTranquilityMax + 1}} {
		if err := KubeGarageSetTranquility("", "", "", "storage", "garage-0", tc.node, tc.value); err == nil {
			t.Errorf("%q %d accepted", tc.node, tc.value)
		}
	}

	if _, err := KubeGarageRepairBlocks("", "", "", "", "garage-0"); err == nil {
		t.Error("empty namespace accepted")
	}
}

func TestGarageTargetOf(t *testing.T) {
	pod := func(name, image string, ready bool) string {
		p := fakePod("storage", name, "n1", ready, map[string]string{"app.kubernetes.io/name": "garage"}, "garage", image)
		b, _ := json.Marshal(p)

		return string(b)
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/storage/pods/garage-0": pod("garage-0", "dxflrs/garage:v2.3.0", true),
		"GET /api/v1/namespaces/storage/pods/garage-1": pod("garage-1", "dxflrs/garage:v2.3.0", false),
		"GET /api/v1/namespaces/storage/pods/nginx-0":  pod("nginx-0", "nginx:1.27", true),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if got, err := garageTargetOf(context.Background(), k, "storage", "garage-0"); err != nil || got != testGarageTarget {
		t.Errorf("garage pod: %+v %v", got, err)
	}

	if _, err := garageTargetOf(context.Background(), k, "storage", "garage-1"); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Errorf("unready pod: %v", err)
	}

	if _, err := garageTargetOf(context.Background(), k, "storage", "nginx-0"); err == nil || !strings.Contains(err.Error(), "does not run Garage") {
		t.Errorf("other pod: %v", err)
	}
}
