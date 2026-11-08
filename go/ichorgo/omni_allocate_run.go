package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	omniVersionUndefined = "undefined"
	omniPhaseRunning     = "running"
)

// StartOmniAllocate puts an unallocated machine in a machine set of the context's cluster and
// follows it: allocated → installing → booting → done (stage RUNNING and ready). A set without a
// machine class gets a MachineSetNode, as omnictl creates it. A set with a class takes machines
// by their labels: the machine must match (else the label it lacks is named); a static set then
// has its count raised by one, an unlimited one needs no write. OnDone's address is always "":
// Omni keeps the list. Bounded by 20 minutes; Cancel only stops following, the allocation stays.
// Refused on the demo and in privacy mode. Omni's Operator role. Recorded in the activity log as
// omni-allocate (the machine and the set).
func StartOmniAllocate(configYAML, contextName, machineID, machineSetID string, listener JoinListener) *JoinRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedJoinListener{listener}

	a := &omniAllocate{
		config: configYAML, context: contextName,
		machineID: strings.TrimSpace(machineID), setID: strings.TrimSpace(machineSetID),
		listener: listener, poll: omniAllocatePoll,
	}

	return a.start(omniAllocateTimeout)
}

// omniAllocate is one run of StartOmniAllocate.
type omniAllocate struct {
	config, context  string
	machineID, setID string
	listener         JoinListener
	poll             time.Duration
	hostname         string // the machine's, for the messages
	written          bool   // the allocation is made: Cancel cannot undo it
}

func (a *omniAllocate) start(timeout time.Duration) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { a.listener.OnDone("", msg) })
		defer holdCalls(a.config, a.context)()

		err := recordedRun(a.config, a.context, a.auditAction, func() error { return a.run(ctx, timeout) })
		if err != nil {
			a.listener.OnDone("", errText(err))

			return
		}

		a.emit(joinPhaseDone, a.name()+" is running in the cluster")
		a.listener.OnDone("", "")
	}()

	return &JoinRun{cancel: cancel}
}

func (a *omniAllocate) auditAction() auditAction {
	return auditAction{Action: "omni-allocate", Node: a.hostname, Params: "machine=" + a.machineID + " set=" + a.setID}
}

func (a *omniAllocate) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, a.listener.OnProgress)
}

func (a *omniAllocate) name() string {
	return cmpOr(a.hostname, a.machineID)
}

func (a *omniAllocate) run(ctx context.Context, timeout time.Duration) error {
	if isTalosDemoContext(a.config, a.context) {
		return errDemoUnavailable
	}

	if privacy.isEnabled() {
		return errOmniAllocatePrivacy
	}

	if a.machineID == "" || a.setID == "" {
		return errors.New("pick a machine and a machine set")
	}

	s, release, err := acquireSigned(a.config, a.context)
	if err != nil {
		return err
	}

	defer release()

	cc, err := omniAllocateDial(s)
	if err != nil {
		return err
	}

	defer cc.Close() //nolint:errcheck

	if err := a.allocate(ctx, cc, s.context.Cluster); err != nil {
		return a.ended(ctx, err, timeout)
	}

	return a.ended(ctx, a.follow(ctx, cc), timeout)
}

// ended turns the run's context ending into what happened to the machine.
func (a *omniAllocate) ended(ctx context.Context, err error, timeout time.Duration) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s was not running within %s: the allocation stays, look at the machine in Omni", a.name(), timeout)
	case ctx.Err() != nil && a.written:
		return errOmniAllocateStopped
	case ctx.Err() != nil:
		return errors.New("stopped before the allocation: nothing changed in Omni")
	}

	return err
}

// allocate checks the machine and the set, then makes the one write this set needs.
func (a *omniAllocate) allocate(ctx context.Context, cc *grpc.ClientConn, cluster string) error {
	machine, err := omniGet(ctx, cc, omniMachineStatusType, a.machineID)
	if err != nil {
		return omniAllocateRead(err, "machine "+a.machineID)
	}

	m, owner := decodeOmniMachine(machine)
	a.hostname = m.Hostname

	if owner != "" {
		return fmt.Errorf("%s already belongs to cluster %s", a.name(), owner)
	}

	res, err := omniGet(ctx, cc, omniMachineSetType, a.setID)
	if err != nil {
		return omniAllocateRead(err, "machine set "+a.setID)
	}

	if got := res.GetMetadata().GetLabels()[omniLabelCluster]; got != cluster {
		return fmt.Errorf("machine set %s is not of cluster %s", a.setID, cluster)
	}

	set := decodeOmniMachineSet(res)
	if set.Allocation.Type == omniAllocationNone {
		return a.bind(ctx, cc, res)
	}

	return a.byClass(ctx, cc, res, set, m)
}

// byClass: the class picks by labels, so a machine that does not match would never come. A
// static count raised for a machine that does match may still take another matching one.
func (a *omniAllocate) byClass(ctx context.Context, cc *grpc.ClientConn, res *v1alpha1.Resource, set omniMachineSet, m omniMachine) error {
	selectors, err := omniClassLabels(ctx, cc, set.Allocation.ClassName)
	if err != nil {
		return omniAllocateRead(err, "machine class "+set.Allocation.ClassName)
	}

	if missing := omniSelectorMissing(selectors, m.Labels); missing != "" {
		return fmt.Errorf("machine class %s will not pick %s: it needs %s", set.Allocation.ClassName, a.name(), missing)
	}

	if set.Allocation.Type == omniAllocationUnlimited {
		a.written = true
		a.emit(omniPhaseAllocated, "machine class "+set.Allocation.ClassName+" will pick "+a.name()+" up")

		return nil
	}

	if err := a.raiseCount(ctx, cc, res); err != nil {
		return err
	}

	a.emit(omniPhaseAllocated, fmt.Sprintf("machine set %s now takes %d machines of class %s", a.setID, set.Allocation.Count+1, set.Allocation.ClassName))

	return nil
}

// bind creates the MachineSetNode omni.NewMachineSetNode builds: the machine's id, the set's
// cluster and role labels, an empty spec.
func (a *omniAllocate) bind(ctx context.Context, cc *grpc.ClientConn, set *v1alpha1.Resource) error {
	from := set.GetMetadata().GetLabels()
	labels := map[string]string{omniLabelMachineSet: a.setID}

	for _, key := range []string{omniLabelCluster, omniLabelRoleControlPlane, omniLabelRoleWorker} {
		if v, ok := from[key]; ok {
			labels[key] = v
		}
	}

	a.written = true

	_, err := v1alpha1.NewStateClient(cc).Create(ctx, &v1alpha1.CreateRequest{
		Resource: &v1alpha1.Resource{
			Metadata: &v1alpha1.Metadata{
				Namespace: omniNamespace, Type: omniMachineSetNodeType, Id: a.machineID,
				Version: omniVersionUndefined, Phase: omniPhaseRunning, Labels: labels,
			},
			Spec: &v1alpha1.Spec{YamlSpec: "{}\n"},
		},
		Options: &v1alpha1.CreateOptions{},
	})
	if err != nil {
		a.written = false

		return omniWriteError(err)
	}

	a.emit(omniPhaseAllocated, a.name()+" is bound to machine set "+a.setID)

	return nil
}

// raiseCount writes the set back with one machine more, at the version it was read at: Omni
// refuses the write when the set changed meanwhile.
func (a *omniAllocate) raiseCount(ctx context.Context, cc *grpc.ClientConn, set *v1alpha1.Resource) error {
	spec, err := omniCountRaised(set.GetSpec().GetProtoSpec())
	if err != nil {
		return fmt.Errorf("read machine set %s: %w", a.setID, err)
	}

	phase := omniPhaseRunning
	a.written = true

	_, err = v1alpha1.NewStateClient(cc).Update(ctx, &v1alpha1.UpdateRequest{
		NewResource: &v1alpha1.Resource{Metadata: set.GetMetadata(), Spec: &v1alpha1.Spec{ProtoSpec: spec}},
		Options:     &v1alpha1.UpdateOptions{Owner: set.GetMetadata().GetOwner(), ExpectedPhase: &phase},
	})
	if err != nil {
		a.written = false

		return omniWriteError(err)
	}

	return nil
}

// omniCountRaised is a MachineSetSpec with its allocation's machine_count one higher; every other
// field stays as it came, the ones the app does not know too.
func omniCountRaised(spec []byte) ([]byte, error) {
	field := omniAllocationField(spec)

	var out []byte

	for rest := spec; len(rest) > 0; {
		n, typ, size := protowire.ConsumeTag(rest)
		if size < 0 {
			return nil, protowire.ParseError(size)
		}

		valueSize := protowire.ConsumeFieldValue(n, typ, rest[size:])
		if valueSize < 0 {
			return nil, protowire.ParseError(valueSize)
		}

		whole := rest[:size+valueSize]
		value := rest[size : size+valueSize]
		rest = rest[size+valueSize:]

		if n != field || typ != protowire.BytesType {
			out = append(out, whole...)

			continue
		}

		msg, size := protowire.ConsumeBytes(value)
		if size < 0 {
			return nil, protowire.ParseError(size)
		}

		raised, err := protoVarintReplaced(msg, 2, protoVarint(msg, 2)+1)
		if err != nil {
			return nil, err
		}

		out = protowire.AppendTag(out, field, protowire.BytesType)
		out = protowire.AppendBytes(out, raised)
	}

	return out, nil
}

// protoVarintReplaced is msg with varint field num set to value.
func protoVarintReplaced(msg []byte, num protowire.Number, value uint64) ([]byte, error) {
	var out []byte

	for len(msg) > 0 {
		n, typ, size := protowire.ConsumeTag(msg)
		if size < 0 {
			return nil, protowire.ParseError(size)
		}

		valueSize := protowire.ConsumeFieldValue(n, typ, msg[size:])
		if valueSize < 0 {
			return nil, protowire.ParseError(valueSize)
		}

		if n != num {
			out = append(out, msg[:size+valueSize]...)
		}

		msg = msg[size+valueSize:]
	}

	out = protowire.AppendTag(out, num, protowire.VarintType)

	return protowire.AppendVarint(out, value), nil
}

// omniAllocateRead says which resource a read of the run failed on.
func omniAllocateRead(err error, what string) error {
	if status.Code(err) == codes.NotFound {
		return errors.New("omni has no " + what)
	}

	return omniReadError(err)
}

// follow polls the machine's status in the cluster until it runs and is ready. The status appears
// only once Omni has taken the machine: until then, and on a passing failure, it polls on.
func (a *omniAllocate) follow(ctx context.Context, cc *grpc.ClientConn) error {
	var phase, message string

	for {
		res, err := omniGet(ctx, cc, omniClusterMachineStatus, a.machineID)

		switch {
		case status.Code(err) == codes.PermissionDenied:
			return omniReadError(err)
		case err == nil:
			next, text, done := omniStageStep(a.name(), res.GetSpec().GetProtoSpec())
			if done {
				return nil
			}

			if next != "" && (next != phase || text != message) {
				phase, message = next, text
				a.emit(next, text)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.poll):
		}
	}
}

// omniStageStep reads ClusterMachineStatusSpec {ready 1, stage 2, last_config_error 5} into a
// phase; "" for a stage that says nothing new (unknown ones included).
func omniStageStep(name string, spec []byte) (phase, message string, done bool) {
	ready, stage := protoVarint(spec, 1) != 0, protoVarint(spec, 2)

	configError, _ := protoField(spec, 5) //nolint:errcheck // malformed: not shown

	switch stage {
	case omniStageRunning:
		if ready {
			return "", "", true
		}

		phase, message = omniPhaseBooting, name+" runs, waiting for it to be ready"
	case omniStageBooting, omniStageRebooting, omniStagePoweringOn:
		phase, message = omniPhaseBooting, name+" is booting"
	case omniStageInstalling, omniStageConfiguring, omniStageUpgrading:
		phase, message = joinPhaseInstalling, name+" is installing Talos and taking its config"
	default:
		return "", "", false
	}

	if len(configError) > 0 {
		message += ": " + string(configError)
	}

	return phase, message, false
}
