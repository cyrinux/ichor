package ichorgo

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
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
		//lint:ignore SA1019 apid still tags each node's events with it, and the feed reads it
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

func TestRunEventsReopensAfterTheStreamEnds(t *testing.T) {
	fastEventsBackoff(t)

	f, cfg := eventsFake(t)

	var (
		mu       sync.Mutex
		opened   int
		gotNodes []string
		gotTail  int32
	)

	f.events = func(nodes []string, req *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		mu.Lock()
		opened++
		first := opened == 1
		if first {
			gotNodes, gotTail = nodes, req.GetTailEvents()
		}
		mu.Unlock()

		if !first {
			<-stream.Context().Done()

			return nil
		}

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

	// The stream ended (EOF): the feed goes on with a new stream, nothing reaches OnDone.
	if ev := r.next(t); ev.Action != "reconnecting" {
		t.Errorf("after the stream ended = %+v, want the reconnecting line", ev)
	}

	select {
	case msg := <-done:
		t.Fatalf("runEvents ended with %q, want a new stream", msg)
	default:
	}

	mu.Lock()
	defer mu.Unlock()

	if strings.Join(gotNodes, ",") != eventsCP+","+eventsWorker || gotTail != 5 {
		t.Errorf("stream opened for %v, tail %d", gotNodes, gotTail)
	}
}

// fastEventsBackoff shortens the wait before an events stream is reopened.
func fastEventsBackoff(t *testing.T) {
	t.Helper()

	minWait, maxWait := eventsBackoffMin, eventsBackoffMax
	eventsBackoffMin, eventsBackoffMax = time.Millisecond, 4*time.Millisecond

	t.Cleanup(func() { eventsBackoffMin, eventsBackoffMax = minWait, maxWait })
}

// A stream error reopens the stream after the last event relayed: Talos then sends only the
// events after that id (as its buffer does with tail_id), so none is lost or repeated.
func TestRunEventsStreamError(t *testing.T) {
	fastEventsBackoff(t)

	f, cfg := eventsFake(t)

	buffer := []*machineapi.Event{
		talosEvent(t, eventsWorker, xidAt(eventsAt, 1), &machineapi.PhaseEvent{Phase: "boot", Action: machineapi.PhaseEvent_START}),
		talosEvent(t, eventsWorker, xidAt(eventsAt, 2), &machineapi.PhaseEvent{Phase: "boot", Action: machineapi.PhaseEvent_STOP}),
		talosEvent(t, eventsWorker, xidAt(eventsAt, 3), &machineapi.TaskEvent{Task: "startKubelet", Action: machineapi.TaskEvent_START}),
	}

	var (
		mu      sync.Mutex
		tailIDs []string
	)

	f.events = func(_ []string, req *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
		mu.Lock()
		tailIDs = append(tailIDs, req.GetTailId())
		first := len(tailIDs) == 1
		mu.Unlock()

		if first {
			if err := sendEvents(stream, buffer[0]); err != nil {
				return err
			}

			return status.Error(codes.Unavailable, "connection to "+eventsWorker+" lost")
		}

		for _, ev := range buffer {
			if ev.GetId() > req.GetTailId() {
				if err := sendEvents(stream, ev); err != nil {
					return err
				}
			}
		}

		<-stream.Context().Done()

		return nil
	}

	r := newTalosEventsRecorder()
	run := StartEvents(cfg, "fake", eventsWorker, 0, r)

	var got []string
	for range 4 {
		ev := r.next(t)
		got = append(got, ev.Subject+" "+ev.Action)
	}

	want := []string{"boot start", "events reconnecting", "boot stop", "startKubelet start"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("events = %v, want %v", got, want)
	}

	run.Cancel()

	if msg := r.wait(t); msg != "" {
		t.Errorf("OnDone after Cancel = %q, want empty", msg)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(tailIDs) != 2 || tailIDs[1] != xidAt(eventsAt, 1) {
		t.Errorf("streams opened with tail ids %q, want a second one after %s", tailIDs, xidAt(eventsAt, 1))
	}

	select {
	case raw := <-r.events:
		t.Errorf("unexpected event: %s", raw)
	default:
	}
}

// A refusal is not retried: the feed ends with it.
func TestRunEventsPermanentError(t *testing.T) {
	fastEventsBackoff(t)

	f, cfg := eventsFake(t)
	f.events = func(_ []string, _ *machineapi.EventsRequest, _ grpc.ServerStreamingServer[machineapi.Event]) error {
		return status.Error(codes.PermissionDenied, "not authorized")
	}

	r := newTalosEventsRecorder()
	StartEvents(cfg, "fake", eventsWorker, 0, r)

	if msg := r.wait(t); !strings.Contains(msg, "not authorized") {
		t.Errorf("OnDone = %q", msg)
	}

	if got := f.called("Events"); len(got) != 1 {
		t.Errorf("Events calls = %v, want one", got)
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
		//lint:ignore SA1019 apid still reports a failed node through it, and the feed reads it
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
	err = watchEvents(client.WithNodes(t.Context(), eventsCP, eventsWorker), s.client, client.WithTailEvents(3), ch)

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
	if err := watchEvents(ctx, s.client, client.WithTailEvents(1), make(chan eventItem)); err == nil {
		t.Fatal("watchEvents returned no error after its context ended")
	}
}
