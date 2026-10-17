package ichorgo

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// Two nodes with room for CPU and memory; big-0 (on a) asks 6Gi of ephemeral storage and a
// filler pod on b holds 6Gi of b's 10Gi.
const ephemeralPodsBody = `{"items":[
	{"metadata":{"name":"big-0","namespace":"apps","ownerReferences":[{"kind":"StatefulSet","name":"big","controller":true}]},
	 "spec":{"nodeName":"a","containers":[{"name":"app","resources":{
	   "requests":{"cpu":"1","memory":"1Gi","ephemeral-storage":"6Gi"},"limits":{"memory":"1Gi","ephemeral-storage":"6Gi"}}}]},
	 "status":{"phase":"Running","qosClass":"Burstable"}},
	{"metadata":{"name":"filler","namespace":"apps"},
	 "spec":{"nodeName":"b","containers":[{"name":"x","resources":{"requests":{"ephemeral-storage":"6Gi"}}}]},
	 "status":{"phase":"Running","qosClass":"BestEffort"}}]}`

func ephemeralNodesBody(ephemeral, pressureOnB bool) string {
	node := func(name string, pressure bool) string {
		allocatable, conditions := "", ""
		if ephemeral {
			allocatable = `,"ephemeral-storage":"10Gi"`
		}

		if pressure {
			conditions = `,{"type":"DiskPressure","status":"True"}`
		}

		return fmt.Sprintf(`{"metadata":{"name":%q},"status":{"allocatable":{"cpu":"4","memory":"8Gi","pods":"10"%s},`+
			`"conditions":[{"type":"Ready","status":"True"}%s]}}`, name, allocatable, conditions)
	}

	return `{"items":[` + node("a", false) + "," + node("b", pressureOnB) + `]}`
}

func ephemeralSummary(bigUsed float64) string {
	return fmt.Sprintf(`{"node":{"fs":{"availableBytes":%d,"capacityBytes":%d,"usedBytes":%d},"runtime":{"imageFs":{"capacityBytes":%d}}},
		"pods":[{"podRef":{"name":"big-0","namespace":"apps"},"ephemeral-storage":{"usedBytes":%.0f}}]}`,
		int64(8<<30), int64(20<<30), int64(12<<30), int64(20<<30), bigUsed)
}

func TestKubeCapacityEphemeralFitAndDisk(t *testing.T) {
	const gi = float64(1 << 30)

	cases := []struct {
		name                string
		ephemeral, pressure bool
		summaryStatus       int
		bigUsed             float64

		moreFit, pending   int
		shortEphemeral     float64
		measured, forbid   bool
		bPressure          bool
		risk               string
		freeA, allocatable float64
	}{
		{
			name: "fits CPU and memory, not ephemeral storage", ephemeral: true, summaryStatus: http.StatusOK, bigUsed: 1 * gi,
			moreFit: 0, pending: 1, shortEphemeral: 6 * gi, measured: true, risk: riskOK, freeA: 4 * gi, allocatable: 20 * gi,
		},
		{
			// No ephemeral-storage in allocatable: unknown, not full. CPU: 3 on a, 4 on b.
			name: "unknown allocatable is no limit", summaryStatus: http.StatusOK, bigUsed: 1 * gi,
			moreFit: 7, measured: true, risk: riskOK,
		},
		{
			name: "a node under DiskPressure", ephemeral: true, pressure: true, summaryStatus: http.StatusOK, bigUsed: 1 * gi,
			moreFit: 0, pending: 1, shortEphemeral: 6 * gi, measured: true, bPressure: true, risk: riskOK, freeA: 4 * gi, allocatable: 20 * gi,
		},
		{
			// nodes/proxy refused: no disk, everything else still computed.
			name: "kubelet forbidden", ephemeral: true, summaryStatus: http.StatusForbidden,
			moreFit: 0, pending: 1, shortEphemeral: 6 * gi, forbid: true, risk: riskOK, freeA: 4 * gi, allocatable: 20 * gi,
		},
		{
			// 5.6 of its 6Gi limit: past 90 %.
			name: "near its ephemeral limit", ephemeral: true, summaryStatus: http.StatusOK, bigUsed: 5.6 * gi,
			moreFit: 0, pending: 1, shortEphemeral: 6 * gi, measured: true, risk: riskDiskNear, freeA: 4 * gi, allocatable: 20 * gi,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeKubeAPI(t, map[string]string{
				"GET /api/v1/nodes": ephemeralNodesBody(tc.ephemeral, tc.pressure),
				"GET /api/v1/pods":  ephemeralPodsBody,
			})
			for _, n := range []string{"a", "b"} {
				f.answerWith("GET /api/v1/nodes/"+n+"/proxy/stats/summary", tc.summaryStatus, ephemeralSummary(tc.bigUsed))
			}

			c, err := readCapacity(context.Background(), openFakeKube(t, f))
			if err != nil {
				t.Fatal(err)
			}

			var big capacityWorkload

			for _, w := range c.Workloads {
				if w.Name == "big" {
					big = w
				}
			}

			if big.MoreFit != tc.moreFit || big.Risk != tc.risk || big.EphemeralRequest != 6*gi || big.EphemeralLimit != 6*gi {
				t.Errorf("big %+v", big)
			}

			if n := c.NPlusOne; n.PendingPods != tc.pending || n.ShortEphemeral != tc.shortEphemeral || n.OK != (tc.pending == 0) {
				t.Errorf("nPlusOne %+v", n)
			}

			a, b := c.Nodes[0], c.Nodes[1]
			if c.DiskMeasured != tc.measured || c.DiskForbidden != tc.forbid || a.DiskMeasured != tc.measured || b.DiskPressure != tc.bPressure || a.DiskPressure {
				t.Errorf("disk %v/%v, a %+v, b %+v", c.DiskMeasured, c.DiskForbidden, a, b)
			}

			if a.EphemeralRequests != 6*gi || a.EphemeralFree != tc.freeA || c.Totals.EphemeralAllocatable != tc.allocatable {
				t.Errorf("ephemeral a %+v, totals %+v", a, c.Totals)
			}

			if tc.measured && (a.DiskAvailable != 8*gi || c.Totals.DiskCapacity != 40*gi || c.Totals.DiskMeasuredNodes != 2) {
				t.Errorf("measured disk a %+v, totals %+v", a, c.Totals)
			}

			if !tc.measured && (a.DiskCapacity != 0 || c.Totals.DiskMeasuredNodes != 0) {
				t.Errorf("unmeasured disk a %+v, totals %+v", a, c.Totals)
			}

			// CPU and memory alone would leave room: the cluster still answers in full.
			if c.Totals.CPUAllocatable != 8 || len(c.Workloads) != 2 {
				t.Errorf("totals %+v, workloads %d", c.Totals, len(c.Workloads))
			}
		})
	}
}
