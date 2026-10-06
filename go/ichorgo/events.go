package ichorgo

import (
	"context"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/proto"
)

// EventListener receives node events as JSON (implemented in Kotlin/Swift).
type EventListener interface {
	OnEvent(json string)
	// OnDone is called exactly once when the stream ends; errMessage is empty when cancelled.
	OnDone(errMessage string)
}

// EventsRun is a handle on a running events stream.
type EventsRun struct {
	cancel context.CancelFunc
}

// Cancel stops the stream.
func (r *EventsRun) Cancel() { r.cancel() }

type nodeEvent struct {
	Node     string `json:"node"`
	ID       string `json:"id"`
	At       int64  `json:"at"`      // unix milliseconds (from the event id; receive time as fallback)
	Kind     string `json:"kind"`    // service, sequence, phase, task, machine, config, address, restart, other
	Subject  string `json:"subject"` // e.g. the service or sequence name
	Action   string `json:"action"`  // e.g. "running", "start", "stop"
	Message  string `json:"message"`
	Severity string `json:"severity"` // info | warning | error
}

// StartEvents streams events from nodes (comma-separated; empty = the context's nodes),
// like `talosctl events`, replaying the last tail events per node first (os:reader).
func StartEvents(configYAML, contextName, nodes string, tail int, listener EventListener) *EventsRun {
	contextName, nodes = unmaskTargets(configYAML, contextName, nodes)

	listener = maskedEventListener{listener}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		listener.OnDone(runEvents(ctx, configYAML, contextName, nodes, tail, listener))
	}()

	return &EventsRun{cancel: cancel}
}

func runEvents(ctx context.Context, configYAML, contextName, nodes string, tail int, listener EventListener) string {
	if isDemoContext(configYAML, contextName) {
		targets := demoNodes()
		selected := func(node string) bool {
			return strings.TrimSpace(nodes) == "" || slices.Contains(splitCSV(nodes), node)
		}
		emit := func(n nodeOverview, at time.Time) {
			if !selected(n.Node) {
				return
			}
			emitJSON(nodeEvent{Node: n.Node, ID: fmt.Sprintf("demo-%d-%s", at.UnixNano(), n.Hostname), At: at.UnixMilli(), Kind: "service", Subject: "kubelet", Action: "running", Message: "Demo: node health check successful", Severity: "info"}, listener.OnEvent)
		}
		if tail > 0 {
			for _, n := range targets {
				emit(n, time.Now().Add(-time.Minute))
			}
		}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-ctx.Done():
				return ""
			case at := <-ticker.C:
				emit(targets[i%len(targets)], at)
				i++
			}
		}
	}
	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return err.Error()
	}

	defer release()

	targets, err := supportTargets(targetNodes(s.context), nodes)
	if err != nil {
		return err.Error()
	}

	ch := make(chan eventItem)

	go func() {
		// Multi-node proxying (WithNodes, answers tagged through common.Metadata) is deprecated
		// since Talos 1.14 and planned for removal in 2.0: by then, open one stream per node.
		err := safeCall(func() error { return watchEvents(client.WithNodes(ctx, targets...), s.client, tail, ch) })
		if err != nil && ctx.Err() == nil {
			select {
			case ch <- eventItem{err: err}:
			case <-ctx.Done():
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ""
		case res := <-ch:
			if res.err != nil {
				if ctx.Err() != nil {
					return ""
				}

				return friendlyError(res.err)
			}

			ev := res.event
			if ev.Kind == "address" {
				privacy.learnHost(ev.Subject, "node")
			}

			emitJSON(ev, listener.OnEvent)
		}
	}
}

// describeEvent turns a Talos event into a timeline entry.
func describeEvent(ev client.Event, received time.Time) nodeEvent {
	out := nodeEvent{
		Node:     ev.Node,
		ID:       ev.ID,
		At:       received.UnixMilli(),
		Kind:     "other",
		Severity: "info",
	}

	if t, ok := xidTime(ev.ID); ok {
		out.At = t.UnixMilli()
	}

	switch p := ev.Payload.(type) {
	case *machineapi.ServiceStateEvent:
		out.Kind, out.Subject, out.Action, out.Message = "service", p.GetService(), strings.ToLower(p.GetAction().String()), p.GetMessage()

		switch {
		case p.GetAction() == machineapi.ServiceStateEvent_FAILED:
			out.Severity = "error"
		case p.GetHealth() != nil && !p.GetHealth().GetUnknown() && !p.GetHealth().GetHealthy():
			out.Severity = "warning"
		}
	case *machineapi.SequenceEvent:
		out.Kind, out.Subject, out.Action = "sequence", p.GetSequence(), strings.ToLower(p.GetAction().String())
		if e := p.GetError(); e != nil && e.GetMessage() != "" {
			out.Message, out.Severity = e.GetMessage(), "error"
		}
	case *machineapi.PhaseEvent:
		out.Kind, out.Subject, out.Action = "phase", p.GetPhase(), strings.ToLower(p.GetAction().String())
	case *machineapi.TaskEvent:
		out.Kind, out.Subject, out.Action = "task", p.GetTask(), strings.ToLower(p.GetAction().String())
	case *machineapi.MachineStatusEvent:
		out.Kind, out.Subject = "machine", strings.ToLower(p.GetStage().String())

		if st := p.GetStatus(); st != nil {
			out.Action = map[bool]string{true: "ready", false: "not ready"}[st.GetReady()]

			var unmet []string
			for _, c := range st.GetUnmetConditions() {
				unmet = append(unmet, c.GetName()+": "+c.GetReason())
			}

			out.Message = strings.Join(unmet, "; ")
			if !st.GetReady() {
				out.Severity = "warning"
			}
		}
	case *machineapi.ConfigLoadErrorEvent:
		out.Kind, out.Subject, out.Message, out.Severity = "config", "load", p.GetError(), "error"
	case *machineapi.ConfigValidationErrorEvent:
		out.Kind, out.Subject, out.Message, out.Severity = "config", "validation", p.GetError(), "error"
	case *machineapi.AddressEvent:
		out.Kind, out.Subject, out.Message = "address", p.GetHostname(), strings.Join(p.GetAddresses(), ", ")
	case *machineapi.RestartEvent:
		out.Kind, out.Subject = "restart", fmt.Sprintf("%d", p.GetCmd())
	default:
		if ev.Payload != nil {
			out.Subject = string(proto.MessageName(ev.Payload).Name())
		} else {
			out.Subject = ev.TypeURL
		}
	}

	return out
}

var xidEncoding = base32.NewEncoding("0123456789abcdefghijklmnopqrstuv").WithPadding(base32.NoPadding)

// xidTime decodes the creation time embedded in an xid (Talos event ids): 20 base32hex
// characters, the first 4 bytes being big-endian unix seconds.
func xidTime(id string) (time.Time, bool) {
	if len(id) != 20 {
		return time.Time{}, false
	}

	raw, err := xidEncoding.DecodeString(id)
	if err != nil || len(raw) < 4 {
		return time.Time{}, false
	}

	return time.Unix(int64(binary.BigEndian.Uint32(raw[:4])), 0), true
}

var errUnsupportedAction = errors.New("unsupported service action")

type eventItem struct {
	event nodeEvent
	err   error
}

// watchEvents streams events like client.EventsWatchV2, but keeps going where that ends the
// whole stream: events of a type this client can't decode are skipped, and a node that fails
// (offline, say) is reported as an error event while the other nodes keep streaming.
func watchEvents(ctx context.Context, c *client.Client, tail int, ch chan<- eventItem) error {
	stream, err := c.Events(ctx, client.WithTailEvents(int32(tail)))
	if err != nil {
		return fmt.Errorf("error fetching events: %w", err)
	}

	if err = stream.CloseSend(); err != nil {
		return err
	}

	defaultNode := client.RemotePeer(stream.Context())

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		var ev nodeEvent

		if md := msg.GetMetadata(); metaError(md) != "" {
			node := metaHost(md)
			if node == "" {
				node = defaultNode
			}

			ev = nodeFailedEvent(node, metaError(md), time.Now())
		} else {
			decoded, err := client.UnmarshalEvent(msg)
			if err != nil {
				var unsupported client.EventNotSupportedError
				if errors.As(err, &unsupported) {
					continue
				}

				return err
			}

			if decoded == nil {
				continue
			}

			if decoded.Node == "" {
				decoded.Node = defaultNode
			}

			ev = describeEvent(*decoded, time.Now())
		}

		select {
		case ch <- eventItem{event: ev}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// nodeFailedEvent tells that node's event stream failed (offline, say).
func nodeFailedEvent(node, message string, at time.Time) nodeEvent {
	return nodeEvent{
		Node:     node,
		ID:       "error-" + node,
		At:       at.UnixMilli(),
		Kind:     "other",
		Subject:  "events",
		Action:   "unreachable",
		Message:  friendlyError(errors.New(message)),
		Severity: "error",
	}
}
