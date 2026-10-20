package ichorgo

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	eventsCP     = "10.5.0.2"
	eventsWorker = "10.5.0.3"
)

// eventsAt is when the fake's events happened, as their ids say.
var eventsAt = time.Unix(1_800_000_000, 0)

// xidAt is an xid made at t, like Talos event ids.
func xidAt(t time.Time, seq byte) string {
	raw := make([]byte, 12)
	binary.BigEndian.PutUint32(raw, uint32(t.Unix()))
	raw[11] = seq

	return xidEncoding.EncodeToString(raw)
}

// talosEvent is an event of node as apid relays it.
func talosEvent(t *testing.T, node, id string, payload proto.Message) *machineapi.Event {
	t.Helper()

	value, err := proto.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	typeURL := "talos/runtime/" + string(payload.ProtoReflect().Descriptor().FullName())

	ev := &machineapi.Event{Id: id, Data: &anypb.Any{TypeUrl: typeURL, Value: value}}
	if node != "" {
		ev.Metadata = &common.Metadata{Hostname: node}
	}

	return ev
}

func sendEvents(stream grpc.ServerStreamingServer[machineapi.Event], events ...*machineapi.Event) error {
	for _, ev := range events {
		if err := stream.Send(ev); err != nil {
			return err
		}
	}

	return nil
}

// talosEventsRecorder is an EventListener safe to call from the stream's goroutine.
type talosEventsRecorder struct {
	events chan string
	done   chan string
}

func newTalosEventsRecorder() *talosEventsRecorder {
	return &talosEventsRecorder{events: make(chan string, 64), done: make(chan string, 1)}
}

func (r *talosEventsRecorder) OnEvent(json string)      { r.events <- json }
func (r *talosEventsRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *talosEventsRecorder) next(t *testing.T) nodeEvent {
	t.Helper()

	select {
	case raw := <-r.events:
		var ev nodeEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatal(err)
		}

		return ev
	case <-time.After(10 * time.Second):
		t.Fatal("no event")

		return nodeEvent{}
	}
}

func (r *talosEventsRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("OnDone not called")

		return ""
	}
}

func eventsFake(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, eventsCP, "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, eventsWorker, "v1.11.0", machine.TypeWorker)

	return f, f.start(t, eventsCP, eventsWorker)
}

func TestRunEventsStreamsUntilTheStreamEnds(t *testing.T) {
	f, cfg := eventsFake(t)

	var gotNodes []string

	var gotTail int32

	f.events = func(nodes []string, req *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		gotNodes, gotTail = nodes, req.GetTailEvents()

		return sendEvents(stream,
			talosEvent(t, eventsCP, xidAt(eventsAt, 1), &machineapi.ServiceStateEvent{
				Service: "kubelet", Action: machineapi.ServiceStateEvent_FAILED, Message: "exit status 1",
			}),
			talosEvent(t, eventsWorker, xidAt(eventsAt, 2), &machineapi.MachineStatusEvent{
				Stage: machineapi.MachineStatusEvent_RUNNING,
				Status: &machineapi.MachineStatusEvent_MachineStatus{Ready: false, UnmetConditions: []*machineapi.MachineStatusEvent_MachineStatus_UnmetCondition{
					{Name: "nodeReady", Reason: "node not ready"},
				}},
			}),
		)
	}

	r := newTalosEventsRecorder()
	done := make(chan string, 1)

	go func() { done <- runEvents(t.Context(), cfg, "fake", "", 5, r) }()

	first, second := r.next(t), r.next(t)

	want := nodeEvent{Node: eventsCP, ID: xidAt(eventsAt, 1), At: eventsAt.UnixMilli(), Kind: "service", Subject: "kubelet", Action: "failed", Message: "exit status 1", Severity: "error"}
	if first != want {
		t.Errorf("first event = %+v, want %+v", first, want)
	}

	if second.Node != eventsWorker || second.Kind != "machine" || second.Action != "not ready" ||
		second.Message != "nodeReady: node not ready" || second.Severity != "warning" {
		t.Errorf("second event = %+v", second)
	}

	// The stream ended (EOF): there is no reconnection, the run ends with the stream.
	select {
	case msg := <-done:
		if msg != "EOF" {
			t.Errorf("runEvents = %q, want EOF", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runEvents did not return")
	}

	if strings.Join(gotNodes, ",") != eventsCP+","+eventsWorker || gotTail != 5 {
		t.Errorf("stream opened for %v, tail %d", gotNodes, gotTail)
	}
}

func TestRunEventsStreamError(t *testing.T) {
	f, cfg := eventsFake(t)
	f.events = func(_ []string, _ *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		if err := sendEvents(stream, talosEvent(t, eventsWorker, xidAt(eventsAt, 1), &machineapi.PhaseEvent{Phase: "boot", Action: machineapi.PhaseEvent_START})); err != nil {
			return err
		}

		return status.Error(codes.Unavailable, "connection to "+eventsWorker+" lost")
	}

	r := newTalosEventsRecorder()
	StartEvents(cfg, "fake", eventsWorker, 0, r)

	if ev := r.next(t); ev.Node != eventsWorker || ev.Kind != "phase" || ev.Subject != "boot" || ev.Action != "start" {
		t.Errorf("event = %+v", ev)
	}

	if msg := r.wait(t); msg != "unreachable: connection to "+eventsWorker+" lost" {
		t.Errorf("OnDone = %q", msg)
	}

	// One stream error ends the run: the app opens a new one, watchEvents does not.
	if got := f.called("Events"); len(got) != 1 || got[0] != "Events "+eventsWorker {
		t.Errorf("Events calls = %v", got)
	}
}

func TestRunEventsCancelled(t *testing.T) {
	f, cfg := eventsFake(t)
	f.events = func(_ []string, _ *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		if err := sendEvents(stream, talosEvent(t, eventsCP, xidAt(eventsAt, 1), &machineapi.TaskEvent{Task: "startKubelet", Action: machineapi.TaskEvent_STOP})); err != nil {
			return err
		}

		<-stream.Context().Done()

		return nil
	}

	r := newTalosEventsRecorder()
	run := StartEvents(cfg, "fake", "", 1, r)

	if ev := r.next(t); ev.Kind != "task" || ev.Subject != "startKubelet" || ev.Action != "stop" {
		t.Errorf("event = %+v", ev)
	}

	run.Cancel()

	if msg := r.wait(t); msg != "" {
		t.Errorf("OnDone after Cancel = %q, want empty", msg)
	}
}

func TestRunEventsRejectsUnknownNodeAndConfig(t *testing.T) {
	_, cfg := eventsFake(t)

	if msg := runEvents(t.Context(), cfg, "fake", "10.5.0.9", 0, newTalosEventsRecorder()); !strings.Contains(msg, "not part of this context") {
		t.Errorf("unknown node: %q", msg)
	}

	if msg := runEvents(t.Context(), "not a talosconfig", "fake", "", 0, newTalosEventsRecorder()); msg == "" {
		t.Error("a broken talosconfig must fail")
	}
}

func TestRunEventsDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	r := newTalosEventsRecorder()
	done := make(chan string, 1)

	go func() { done <- runEvents(ctx, demo, "Demo cluster", "192.0.2.10", 1, r) }()

	// The replay: one event of the one node asked for.
	if ev := r.next(t); ev.Node != "192.0.2.10" || ev.Kind != "service" || ev.Subject != "kubelet" {
		t.Errorf("demo event = %+v", ev)
	}

	cancel()

	if msg := <-done; msg != "" {
		t.Errorf("demo run = %q", msg)
	}

	select {
	case raw := <-r.events:
		t.Errorf("event of a node not asked for: %s", raw)
	default:
	}
}

// watchEvents keeps the stream going past what client.EventsWatchV2 stops on: a node that
// fails is an error event, an event type it can't decode is skipped.
func TestWatchEventsSkipsAndReportsNodes(t *testing.T) {
	f, cfg := eventsFake(t)
	f.events = func(_ []string, _ *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		failed := &machineapi.Event{Metadata: &common.Metadata{Hostname: eventsWorker, Error: "connection refused"}}
		unknown := &machineapi.Event{Id: xidAt(eventsAt, 2), Data: &anypb.Any{TypeUrl: "talos/runtime/machine.FutureEvent"}}
		anonymous := talosEvent(t, "", "not-an-xid", &machineapi.ConfigValidationErrorEvent{Error: "invalid cluster name"})

		return sendEvents(stream, failed, unknown, anonymous)
	}

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ch := make(chan eventItem, 8)

	before := time.Now()
	//lint:ignore SA1019 as in runEvents
	err = watchEvents(client.WithNodes(t.Context(), eventsCP, eventsWorker), s.client, 3, ch)

	if err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Errorf("watchEvents = %v, want EOF", err)
	}

	close(ch)

	var got []nodeEvent
	for item := range ch {
		got = append(got, item.event)
	}

	if len(got) != 2 {
		t.Fatalf("events = %+v, want the failed node and the config error", got)
	}

	if e := got[0]; e.Node != eventsWorker || e.ID != "error-"+eventsWorker || e.Action != "unreachable" || e.Severity != "error" || e.Message != "connection refused" {
		t.Errorf("failed node event = %+v", e)
	}

	// No hostname: the event is the peer's; no xid: the receive time.
	if e := got[1]; e.Node == "" || e.Kind != "config" || e.Subject != "validation" || e.Severity != "error" || e.At < before.UnixMilli() {
		t.Errorf("config event = %+v", e)
	}
}

func TestWatchEventsStopsWhenNobodyReads(t *testing.T) {
	f, cfg := eventsFake(t)
	f.events = func(_ []string, _ *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		if err := sendEvents(stream, talosEvent(t, eventsCP, xidAt(eventsAt, 1), &machineapi.RestartEvent{Cmd: 1})); err != nil {
			return err
		}

		<-stream.Context().Done()

		return nil
	}

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(client.WithNode(t.Context(), eventsCP), 200*time.Millisecond)
	defer cancel()

	// An unbuffered channel nobody reads: the event can't be handed over before ctx ends.
	if err := watchEvents(ctx, s.client, 1, make(chan eventItem)); err == nil {
		t.Fatal("watchEvents returned no error after its context ended")
	}
}
