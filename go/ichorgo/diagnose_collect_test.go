package ichorgo

import (
	"strings"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// diagnosisFake is a control plane and a worker whose kubelet is unhealthy (the fake's
// service list), with Kubernetes node states, a crash-looping static pod and a few events.
func diagnosisFake(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f, cfg := eventsFake(t)

	for node, ready := range map[string]bool{eventsCP: true, eventsWorker: false} {
		ns := k8s.NewNodeStatus(k8s.NamespaceName, "host-"+strings.ReplaceAll(node, ".", "-"))
		ns.TypedSpec().Nodename = ns.Metadata().ID()
		ns.TypedSpec().NodeReady = ready
		ns.TypedSpec().Unschedulable = !ready
		f.put(node, ns)
	}

	apiserver := k8s.NewStaticPodStatus(k8s.NamespaceName, "kube-system/kube-apiserver-cp")
	apiserver.TypedSpec().PodStatus = map[string]any{
		"phase":      "Running",
		"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
	}

	scheduler := k8s.NewStaticPodStatus(k8s.NamespaceName, "kube-system/kube-scheduler-cp")
	scheduler.TypedSpec().PodStatus = map[string]any{
		"phase":      "Running",
		"conditions": []any{map[string]any{"type": "Ready", "status": "False"}},
		"containerStatuses": []any{map[string]any{
			"restartCount": float64(7),
			"state":        map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff", "message": "back-off 5m0s"}},
		}},
	}

	f.put(eventsCP, scheduler, apiserver)

	f.events = func(_ []string, _ *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		err := sendEvents(stream,
			talosEvent(t, eventsWorker, xidAt(eventsAt.Add(2*time.Second), 2), &machineapi.ServiceStateEvent{
				Service: "kubelet", Action: machineapi.ServiceStateEvent_FAILED, Message: "exit status 1",
			}),
			talosEvent(t, eventsCP, xidAt(eventsAt, 1), &machineapi.ServiceStateEvent{
				Service: "etcd", Action: machineapi.ServiceStateEvent_RUNNING, Message: "Health check successful",
			}),
			talosEvent(t, eventsCP, xidAt(eventsAt.Add(time.Second), 3), &machineapi.ConfigValidationErrorEvent{Error: "unknown key"}),
		)
		if err != nil {
			return err
		}

		// Like Talos: the stream stays open after the replay.
		<-stream.Context().Done()

		return nil
	}

	return f, cfg
}

func TestCollectDiagnosisOnTwoNodes(t *testing.T) {
	_, cfg := diagnosisFake(t)

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	data := collectDiagnosis(t.Context(), s, kubeTarget{cfg, "fake", ""})

	if len(data.Nodes) != 2 || data.Etcd == nil || data.EtcdNote != "" || data.GitOps != nil || data.DataServices != nil {
		t.Fatalf("data = %+v", data)
	}

	cp, worker := data.Nodes[0], data.Nodes[1]
	if cp.Node != eventsCP || worker.Node != eventsWorker || cp.Role != "controlplane" || worker.Role != "worker" {
		t.Fatalf("nodes = %s %s / %s %s", cp.Node, cp.Role, worker.Node, worker.Role)
	}

	// The unhealthy kubelet gets its log tail, on each node.
	for _, n := range data.Nodes {
		if len(n.Logs) != 1 || n.Logs[0].Service != "kubelet" || n.Logs[0].Err != "" ||
			len(n.Logs[0].Lines) != 1 || n.Logs[0].Lines[0] != "log line of kubelet" {
			t.Errorf("%s logs = %+v", n.Node, n.Logs)
		}

		if len(n.Services) != 2 || n.ServicesErr != "" || n.Running != 2 || n.Clock == nil || n.Resources == nil {
			t.Errorf("%s = %+v", n.Node, n)
		}
	}

	if cp.Kube == nil || !cp.Kube.Ready || worker.Kube == nil || worker.Kube.Ready || !worker.Kube.Unschedulable {
		t.Errorf("kube states = %+v / %+v", cp.Kube, worker.Kube)
	}

	// Static pods only on the control plane, sorted.
	want := []staticPodState{
		{Name: "kube-system/kube-apiserver-cp", Phase: "Running", Ready: true},
		{Name: "kube-system/kube-scheduler-cp", Phase: "Running", Restarts: 7, Waiting: "CrashLoopBackOff: back-off 5m0s"},
	}
	if len(cp.StaticPods) != 2 || cp.StaticPods[0] != want[0] || cp.StaticPods[1] != want[1] || worker.StaticPods != nil {
		t.Errorf("static pods = %+v / %+v", cp.StaticPods, worker.StaticPods)
	}

	// Warnings and errors only, oldest first; the stream went quiet, it did not fail.
	if len(data.Events) != 2 || data.EventsNote != "" ||
		data.Events[0].Kind != "config" || data.Events[1].Subject != "kubelet" || data.Events[1].Node != eventsWorker {
		t.Errorf("events = %+v, note %q", data.Events, data.EventsNote)
	}

	report := renderDiagnosis(data)
	for _, part := range []string{
		"static pod kube-system/kube-scheduler-cp: Running, NOT READY, 7 restarts, waiting: CrashLoopBackOff: back-off 5m0s",
		"kubernetes node host-10-5-0-3: NOT READY",
		"kubelet log, last 1 lines:\n    | log line of kubelet",
		"RECENT WARNING AND ERROR EVENTS (2, oldest first)",
		eventsWorker + " error: service kubelet failed: exit status 1",
	} {
		if !strings.Contains(report, part) {
			t.Errorf("report lacks %q:\n%s", part, report)
		}
	}
}

// The text handed to the model holds placeholders only: no node address or name.
func TestCollectDiagnosisAnonymizedPrompt(t *testing.T) {
	_, cfg := diagnosisFake(t)

	d, err := CollectDiagnosis(cfg, "fake", "", true)
	if err != nil {
		t.Fatal(err)
	}

	prompt := d.Prompt("en", "host-10-5-0-3 flaps since 10.5.0.3 was rebooted")
	if prompt == "" || !d.Anonymized() {
		t.Fatal("no prompt")
	}

	for _, private := range []string{"10.5.0.", "host-10-5-0"} {
		if strings.Contains(prompt, private) {
			t.Errorf("the prompt shows %q:\n%s", private, prompt)
		}
	}

	for _, part := range []string{"<operator_note>", "CrashLoopBackOff", "log line of kubelet", "failed: exit status 1"} {
		if !strings.Contains(prompt, part) {
			t.Errorf("the prompt lacks %q:\n%s", part, prompt)
		}
	}

	// The answer gets the real names back.
	if got := d.reveal(d.maskNote("restart host-10-5-0-3")); got != "restart host-10-5-0-3" {
		t.Errorf("reveal = %q", got)
	}
}

func TestDiagnoseNodeUnreachableAndFailedLogs(t *testing.T) {
	f, cfg := eventsFake(t)
	f.failLogs = "kubelet"

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if n := diagnoseNode(t.Context(), s.client, nodeOverview{Node: "10.5.0.9"}); n.Services != nil || n.Logs != nil || n.Clock != nil {
		t.Errorf("an unreachable node was asked: %+v", n)
	}

	got := fetchLogTail(client.WithNode(t.Context(), eventsCP), s.client, "kubelet")
	if got.Service != "kubelet" || got.Lines != nil || !strings.Contains(got.Err, "log not found") {
		t.Errorf("failed log tail = %+v", got)
	}

	// A node that does not answer the COSI state has no static pod list.
	if pods := fetchStaticPods(client.WithNode(t.Context(), "10.5.0.9"), s.client); pods != nil {
		t.Errorf("static pods of a down node = %+v", pods)
	}
}

func TestRecentProblemEventsNotes(t *testing.T) {
	f, cfg := eventsFake(t)

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if events, note := recentProblemEvents(t.Context(), s.client, nil); events != nil || note != "no reachable node" {
		t.Errorf("no node: %v, %q", events, note)
	}

	f.events = func(_ []string, _ *machineapi.EventsRequest, _ grpc.ServerStreamingServer[machineapi.Event]) error {
		return status.Error(codes.PermissionDenied, "not authorized")
	}

	events, note := recentProblemEvents(t.Context(), s.client, []string{eventsCP})
	if events != nil || !strings.HasPrefix(note, "could not read the events: ") || !strings.Contains(note, "not authorized") {
		t.Errorf("failed stream: %v, %q", events, note)
	}

	// A replay that ends cleanly (EOF) within the window is not a failure: no "EOF" note.
	f.events = func(_ []string, _ *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		return sendEvents(stream, talosEvent(t, eventsCP, xidAt(eventsAt, 1), &machineapi.TaskEvent{Task: "startKubelet", Action: machineapi.TaskEvent_STOP}))
	}

	if events, note := recentProblemEvents(t.Context(), s.client, []string{eventsCP}); events != nil || note != "" {
		t.Errorf("clean end: %v, %q", events, note)
	}
}
