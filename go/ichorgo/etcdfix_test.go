package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// nospaceOverview is three members, b2 the leader, with a NOSPACE alarm.
func nospaceOverview(alarm bool) etcdOverview {
	ov := etcdOverview{
		Members: []etcdMember{{ID: "a1", Hostname: "cp-1"}, {ID: "b2", Hostname: "cp-2"}, {ID: "c3", Hostname: "cp-3"}},
		Statuses: []etcdNodeStatus{
			{Node: "10.0.0.1", MemberID: "a1", DbSize: 900, DbSizeInUse: 800},
			{Node: "10.0.0.2", MemberID: "b2", IsLeader: true, DbSize: 900, DbSizeInUse: 100},
			{Node: "10.0.0.3", MemberID: "c3", DbSize: 900, DbSizeInUse: 300},
		},
		Alarms: []etcdAlarm{},
	}

	if alarm {
		ov.Alarms = []etcdAlarm{{MemberID: "a1", Alarm: "NOSPACE"}}
	}

	return ov
}

// stubFixer answers the fix from canned overviews and records the calls.
type stubFixer struct {
	overviews []etcdOverview
	calls     []string
	defragErr map[string]error
	onDefrag  func(node string)
}

func (s *stubFixer) overview(context.Context) (etcdOverview, error) {
	s.calls = append(s.calls, "overview")
	ov := s.overviews[0]

	if len(s.overviews) > 1 {
		s.overviews = s.overviews[1:]
	}

	return ov, nil
}

func (s *stubFixer) snapshot(_ context.Context, node string, progress func(int64)) error {
	s.calls = append(s.calls, "snapshot "+node)
	progress(2 << 20)

	return nil
}

func (s *stubFixer) defrag(_ context.Context, node string) error {
	s.calls = append(s.calls, "defrag "+node)

	if s.onDefrag != nil {
		s.onDefrag(node)
	}

	return s.defragErr[node]
}

func (s *stubFixer) disarm(_ context.Context, node string) error {
	s.calls = append(s.calls, "disarm "+node)

	return nil
}

func TestRunEtcdFixOrder(t *testing.T) {
	after := nospaceOverview(false)
	after.Statuses[0].DbSize, after.Statuses[1].DbSize, after.Statuses[2].DbSize = 800, 100, 300

	f := &stubFixer{overviews: []etcdOverview{nospaceOverview(true), after}}

	var events []etcdFixProgress

	if err := runEtcdFix(context.Background(), f, "", true, func(p etcdFixProgress) { events = append(events, p) }); err != nil {
		t.Fatal(err)
	}

	// Followers first, most to reclaim first; the leader last, also the snapshot source.
	want := []string{"overview", "snapshot 10.0.0.2", "defrag 10.0.0.3", "defrag 10.0.0.1", "defrag 10.0.0.2", "disarm 10.0.0.3", "overview"}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("calls = %v", f.calls)
	}

	last := events[len(events)-1]
	if last.Phase != fixRecheck || last.Step != 4 || last.Steps != fixSteps || !strings.Contains(last.Message, "alarm is cleared") {
		t.Errorf("last = %+v", last)
	}

	reclaimed := map[string]int64{}
	for _, m := range last.Members {
		reclaimed[m.Hostname] = m.ReclaimedBytes

		if m.State != fixDone {
			t.Errorf("%s state = %s", m.Hostname, m.State)
		}
	}

	if reclaimed["cp-1"] != 100 || reclaimed["cp-2"] != 800 || reclaimed["cp-3"] != 600 {
		t.Errorf("reclaimed = %v", reclaimed)
	}
}

func TestRunEtcdFixStopsOnAFailedDefrag(t *testing.T) {
	f := &stubFixer{overviews: []etcdOverview{nospaceOverview(true)}, defragErr: map[string]error{"10.0.0.1": errors.New("deadline exceeded")}}

	var last etcdFixProgress

	err := runEtcdFix(context.Background(), f, "", false, func(p etcdFixProgress) { last = p })
	if err == nil || !strings.Contains(err.Error(), "defragmenting cp-1 failed") {
		t.Fatalf("err = %v", err)
	}

	if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "disarm") || strings.HasPrefix(c, "snapshot") }) {
		t.Errorf("calls = %v", f.calls)
	}

	if last.Members[1].State != fixFailed || last.Members[2].State != fixPending {
		t.Errorf("members = %+v", last.Members)
	}
}

func TestRunEtcdFixCancelStopsBetweenMembers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &stubFixer{overviews: []etcdOverview{nospaceOverview(true)}, onDefrag: func(string) { cancel() }}

	err := runEtcdFix(ctx, f, "", false, func(etcdFixProgress) {})
	if err == nil || !strings.Contains(err.Error(), "stopped before the defrag step") {
		t.Fatalf("err = %v", err)
	}

	if !slices.Equal(f.calls, []string{"overview", "defrag 10.0.0.3"}) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestRunEtcdFixRefusals(t *testing.T) {
	f := &stubFixer{overviews: []etcdOverview{nospaceOverview(false)}}
	if err := runEtcdFix(context.Background(), f, "", true, func(etcdFixProgress) {}); err == nil || !strings.Contains(err.Error(), "no NOSPACE alarm") {
		t.Errorf("no alarm = %v", err)
	}

	unread := nospaceOverview(true)
	unread.AlarmsError = "unreachable"
	f = &stubFixer{overviews: []etcdOverview{unread}}

	if err := runEtcdFix(context.Background(), f, "", true, func(etcdFixProgress) {}); err == nil || !strings.Contains(err.Error(), "cannot read the etcd alarms") {
		t.Errorf("unread = %v", err)
	}

	f = &stubFixer{overviews: []etcdOverview{nospaceOverview(true), nospaceOverview(true)}}
	if err := runEtcdFix(context.Background(), f, "", false, func(etcdFixProgress) {}); err == nil || !strings.Contains(err.Error(), "still active") {
		t.Errorf("still = %v", err)
	}
}

// fixRecorder is an EtcdFixListener keeping what it is told.
type fixRecorder struct {
	mu     sync.Mutex
	events []etcdFixProgress
	done   chan string
}

func (r *fixRecorder) OnProgress(s string) {
	var p etcdFixProgress
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	r.mu.Lock()
	r.events = append(r.events, p)
	r.mu.Unlock()
}

func (r *fixRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *fixRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(30 * time.Second):
		t.Fatal("OnDone not called")

		return ""
	}
}

func TestStartEtcdNospaceFixFake(t *testing.T) {
	withDataDir(t)

	f, cfg := threeControlPlanes(t, true)
	f.alarms = []*machineapi.EtcdMemberAlarm{{MemberId: 0xa1, Alarm: machineapi.EtcdMemberAlarm_NOSPACE}}
	data := bytes.Repeat([]byte("etcd-db-"), 1<<10)
	f.snapshot = func(stream grpc.ServerStreamingServer[common.Data]) error { return sendChunks(stream, data) }

	dest := filepath.Join(t.TempDir(), "etcd.snapshot")
	r := &fixRecorder{done: make(chan string, 1)}

	StartEtcdNospaceFix(cfg, "fake", "192.0.2.51", dest, "", "", r)

	if msg := r.wait(t); msg != "" {
		t.Fatalf("fix failed: %s", msg)
	}

	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("snapshot file: %v", err)
	}

	if got := f.called("EtcdDefragment"); len(got) != 3 || got[2] != "EtcdDefragment 192.0.2.51" {
		t.Errorf("defrags = %v (the leader last)", got)
	}

	if got := f.called("EtcdAlarmDisarm"); len(got) != 1 {
		t.Errorf("disarms = %v", got)
	}

	entries := readAudit(t, "fake", "etcd-nospace-fix")
	if len(entries) != 1 || entries[0].Params != "snapshot=yes" || entries[0].Outcome != auditOK {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartEtcdNospaceFixFakeDefragFails(t *testing.T) {
	f, cfg := threeControlPlanes(t, true)
	f.alarms = []*machineapi.EtcdMemberAlarm{{MemberId: 0xa1, Alarm: machineapi.EtcdMemberAlarm_NOSPACE}}
	f.defragErr = map[string]error{"192.0.2.52": status.Error(codes.DeadlineExceeded, "defrag timed out")}

	r := &fixRecorder{done: make(chan string, 1)}
	StartEtcdNospaceFix(cfg, "fake", "", "", "", "", r)

	if msg := r.wait(t); !strings.Contains(msg, "defragmenting host-192-0-2-52 failed") {
		t.Fatalf("msg = %q", msg)
	}

	if got := f.called("EtcdAlarmDisarm"); len(got) != 0 {
		t.Errorf("disarmed after a failed defrag: %v", got)
	}
}

func TestStartEtcdNospaceFixDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	r := &fixRecorder{done: make(chan string, 1)}
	StartEtcdNospaceFix(cfg, "Demo cluster", "", "", "", "", r)

	if msg := r.wait(t); msg != errDemoUnavailable.Error() {
		t.Fatalf("demo = %q", msg)
	}
}
