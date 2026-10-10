package ichorgo

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

func TestXidTime(t *testing.T) {
	// An xid starts with 4 big-endian bytes of unix seconds, then 8 other bytes.
	want := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	raw := make([]byte, 12)
	binary.BigEndian.PutUint32(raw, uint32(want.Unix()))
	id := xidEncoding.EncodeToString(raw)

	got, ok := xidTime(id)
	if !ok || !got.Equal(want) {
		t.Errorf("xidTime(%q) = %v, %v", id, got, ok)
	}

	if _, ok := xidTime("not-an-xid"); ok {
		t.Error("garbage accepted")
	}
}

func TestDescribeEvent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)

	failed := describeEvent(client.Event{Node: "n1", ID: "x", Payload: &machineapi.ServiceStateEvent{
		Service: "etcd", Action: machineapi.ServiceStateEvent_FAILED, Message: "boom",
	}}, now)
	if failed.Kind != "service" || failed.Subject != "etcd" || failed.Action != "failed" || failed.Severity != "error" || failed.At != now.UnixMilli() {
		t.Errorf("failed = %+v", failed)
	}

	seq := describeEvent(client.Event{Payload: &machineapi.SequenceEvent{
		Sequence: "boot", Action: machineapi.SequenceEvent_START,
	}}, now)
	if seq.Kind != "sequence" || seq.Subject != "boot" || seq.Action != "start" || seq.Severity != "info" {
		t.Errorf("seq = %+v", seq)
	}

	ms := describeEvent(client.Event{Payload: &machineapi.MachineStatusEvent{
		Stage: machineapi.MachineStatusEvent_RUNNING,
		Status: &machineapi.MachineStatusEvent_MachineStatus{Ready: false, UnmetConditions: []*machineapi.MachineStatusEvent_MachineStatus_UnmetCondition{
			{Name: "services", Reason: "etcd not healthy"},
		}},
	}}, now)
	if ms.Kind != "machine" || ms.Action != "not ready" || ms.Severity != "warning" || !strings.Contains(ms.Message, "etcd not healthy") {
		t.Errorf("ms = %+v", ms)
	}
}

func TestMergeContainersDropsPauseAndJoinsStats(t *testing.T) {
	got := mergeContainers(containerNSK8s,
		[]*machineapi.ContainerInfo{
			{Id: "kube-system/coredns-1", PodId: "kube-system/coredns-1", Image: "registry.k8s.io/pause:3.10"},
			{Id: "kube-system/coredns-1:coredns:abc", PodId: "kube-system/coredns-1", Name: "coredns", Image: "coredns:1.12", Status: "CONTAINER_RUNNING", Pid: 42},
			{Id: "default/web-0:app:def", PodId: "default/web-0", Name: "app", Image: "nginx"},
		},
		[]*machineapi.Stat{{Id: "kube-system/coredns-1:coredns:abc", MemoryUsage: 1024, CpuUsage: 5_000_000}},
	)

	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}

	if got[0].Pod != "web-0" || got[0].PodNamespace != "default" || got[0].Memory != 0 {
		t.Errorf("got[0] = %+v", got[0])
	}

	if got[1].Name != "coredns" || got[1].Memory != 1024 || got[1].CPUNanos != 5_000_000 || got[1].Pid != 42 {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestServiceActionValidation(t *testing.T) {
	for _, ok := range []string{"start", "stop", "restart"} {
		if err := validateServiceAction("kubelet", ok); err != nil {
			t.Errorf("%s rejected", ok)
		}
	}

	if validateServiceAction("kubelet", "kill") == nil || validateServiceAction("", "restart") == nil {
		t.Error("invalid action or service accepted")
	}
}

func TestLineSplitter(t *testing.T) {
	var lines []string

	l := newLineSplitter(func(s string) { lines = append(lines, s) })
	l.write([]byte("a\r\nb"))
	l.write([]byte("c\nd"))
	l.flush()

	if strings.Join(lines, "|") != "a|bc|d" {
		t.Errorf("lines = %v", lines)
	}

	_ = common.Data{}
}

func TestNodeFailedEvent(t *testing.T) {
	at := time.UnixMilli(1_700_000_000_000)
	ev := nodeFailedEvent("10.0.0.9", "connection refused", at)

	if ev.Node != "10.0.0.9" || ev.Severity != "error" || ev.Action != "unreachable" || ev.At != at.UnixMilli() {
		t.Fatalf("unexpected event: %+v", ev)
	}

	if ev.ID != "error-10.0.0.9" || ev.Message == "" {
		t.Fatalf("missing id or message: %+v", ev)
	}
}
