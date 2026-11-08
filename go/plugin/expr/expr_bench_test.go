package expr

import (
	"encoding/json"
	"fmt"
	"testing"
)

// benchFields are 20 expressions of the kind a plugin's fields declare (kube-green's, plus
// conditions, filters and quantities).
var benchFields = []string{
	`$.metadata.name`,
	`$.metadata.namespace`,
	`$.spec.weekdays`,
	`$.spec.sleepAt`,
	`$.spec.wakeUpAt | default("")`,
	`$.spec.timeZone | default("UTC")`,
	`$.spec.suspendDeployments | default(true)`,
	`$.spec.suspendCronJobs | default(false)`,
	`len($.spec.excludeRef | default([]))`,
	`$.status.operation | default("")`,
	`$.status.operation == "SLEEP"`,
	`$.status.lastScheduleTime | time()`,
	`conditions.Ready`,
	`!conditions.Degraded`,
	`$.status.conditions[?(@.type == "Ready")].reason | first()`,
	`$.spec.memory | quantity() > 1000000`,
	`$.spec.replicas * 2 + 1`,
	`"sleepinfo-" + $.metadata.name`,
	`$.metadata.labels["kube-green.com/managed"] == "true" ? "managed" : "manual"`,
	`$.spec.excludeRef[*].kind | join(",")`,
}

// BenchmarkEval evaluates 20 fields over 10,000 objects per iteration: the budget is
// < 100 ms per iteration on a laptop.
func BenchmarkEval(b *testing.B) {
	nodes := make([]Node, len(benchFields))
	for i, src := range benchFields {
		n, err := Parse(src)
		if err != nil {
			b.Fatal(err)
		}

		nodes[i] = n
	}

	objects := make([]any, 10_000)
	for i := range objects {
		var obj any
		if err := json.Unmarshal(fmt.Appendf(nil, `{"metadata":{"name":"s-%d","namespace":"demo-%d","labels":{"kube-green.com/managed":"true"}},`+
			`"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"08:00","replicas":%d,"memory":"2Gi","excludeRef":[{"kind":"Deployment"}]},`+
			`"status":{"operation":"SLEEP","lastScheduleTime":"2026-10-15T06:00:00Z","conditions":[{"type":"Ready","status":"True","reason":"Ok"}]}}`,
			i, i%7, i%5), &obj); err != nil {
			b.Fatal(err)
		}

		objects[i] = obj
	}

	b.ReportAllocs()

	for b.Loop() {
		for _, obj := range objects {
			env := Env{Object: obj, Now: now}

			for _, n := range nodes {
				if _, err := n.Eval(env); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}
