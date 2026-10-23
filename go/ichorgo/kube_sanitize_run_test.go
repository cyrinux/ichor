package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sweepListener records a background sweep; onProgress, when set, runs on every progress.
type sweepListener struct {
	progress   []sanitizeProgress
	onProgress func(sanitizeProgress)
	done       chan [2]string
}

func newSweepListener() *sweepListener { return &sweepListener{done: make(chan [2]string, 1)} }

func (l *sweepListener) OnProgress(js string) {
	var p sanitizeProgress
	if err := json.Unmarshal([]byte(js), &p); err != nil {
		panic(err)
	}

	l.progress = append(l.progress, p)

	if l.onProgress != nil {
		l.onProgress(p)
	}
}

func (l *sweepListener) OnDone(resultJSON, errMessage string) {
	l.done <- [2]string{resultJSON, errMessage}
}

// wait is the sweep's result and error message once OnDone came.
func (l *sweepListener) wait(t *testing.T) (sanitizeResult, string) {
	t.Helper()

	select {
	case d := <-l.done:
		var res sanitizeResult
		if d[0] != "" {
			if err := json.Unmarshal([]byte(d[0]), &res); err != nil {
				t.Fatalf("%v: %s", err, d[0])
			}
		}

		return res, d[1]
	case <-time.After(10 * time.Second):
		t.Fatal("no OnDone")

		return sanitizeResult{}, ""
	}
}

func TestStartKubeSanitize(t *testing.T) {
	cases := map[string]struct {
		cancelAfter                  int // cancel once this many pods were tried; 0: never
		deleted, failed, progress    int
		stopped                      bool
		outcome, auditErr, errSubstr string
	}{
		"one pod gone":  {deleted: 2, failed: 1, progress: 3, outcome: auditPartial, auditErr: "1 of 3 pods not deleted"},
		"stopped early": {cancelAfter: 1, deleted: 1, progress: 1, stopped: true, outcome: auditPartial, auditErr: "stopped after 1 of 3 pods"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataDir(t)
			withAuditClock(t, time.Now())

			f := newFakeKubeAPI(t, map[string]string{
				"DELETE /api/v1/namespaces/web/pods/done":     `{"kind":"Pod"}`,
				"DELETE /api/v1/namespaces/web/pods/crashing": `{"kind":"Pod"}`,
				// web/gone answers 404: no answer is set.
			})

			l := newSweepListener()
			runs := make(chan *KubeSanitizeRun, 1)

			if tc.cancelAfter > 0 {
				l.onProgress = func(p sanitizeProgress) {
					if p.Done == tc.cancelAfter {
						(<-runs).Cancel()
					}
				}
			}

			runs <- StartKubeSanitize(kubeStoreFor(t, f), "admin@test", "", "web", "web/done\nweb/gone\nweb/crashing", l)

			res, errMessage := l.wait(t)
			if errMessage != "" || res.Deleted != tc.deleted || len(res.Failed) != tc.failed || res.Stopped != tc.stopped {
				t.Errorf("result %+v, error %q", res, errMessage)
			}

			if len(l.progress) != tc.progress {
				t.Fatalf("progress %+v", l.progress)
			}

			last := l.progress[len(l.progress)-1]
			if last.Done != tc.progress || last.Total != 3 || last.Failed != tc.failed || last.Name == "" || last.At == 0 {
				t.Errorf("last progress %+v", last)
			}

			if dels := deleteRequests(f); len(dels) != tc.progress {
				t.Errorf("deletes %+v, want %d", dels, tc.progress)
			}

			audit := readAudit(t, "", "sanitize-pods")
			if len(audit) != 1 || audit[0].Outcome != tc.outcome || audit[0].Error != tc.auditErr || audit[0].Object != "Pod×3" {
				t.Errorf("audit %+v", audit)
			}
		})
	}
}

func TestStartKubeSanitizeRefused(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{"DELETE /api/v1/namespaces/web/pods/done": `{}`})

	l := newSweepListener()
	StartKubeSanitize(kubeStoreFor(t, f), "admin@test", "", "web", "web/done\nkube-system/coredns-1", l)

	res, errMessage := l.wait(t)
	if !strings.Contains(errMessage, "not in namespace") || res.Deleted != 0 || len(l.progress) != 0 {
		t.Errorf("result %+v, error %q, progress %+v", res, errMessage, l.progress)
	}

	if dels := deleteRequests(f); len(dels) != 0 {
		t.Errorf("a refused sweep deleted %+v", dels)
	}

	if audit := readAudit(t, "", "sanitize-pods"); len(audit) != 1 || audit[0].Outcome != auditFailed {
		t.Errorf("audit %+v", audit)
	}
}

func TestStartKubeSanitizeDemo(t *testing.T) {
	noKubeDial(t)
	withDataDir(t)

	l := newSweepListener()
	StartKubeSanitize(demoKubeconfigForTest(t), "", "", "demo", "demo/worker-6f4b8-uvwxy", l)

	if _, errMessage := l.wait(t); errMessage != errDemoUnavailable.Error() {
		t.Errorf("demo sweep error %q", errMessage)
	}
}
