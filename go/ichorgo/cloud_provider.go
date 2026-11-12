package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Creating a billable machine for a new node, at any provider. The user data (Omni's join config,
// or a new member's config) goes with the create, so the node never sits in maintenance mode on a
// public address. The run follows the machine until it is running, then the node until it joins
// (plain Talos) or appears in Omni. A run that fails or is stopped deletes the one machine it
// created, except a control plane that already answered as a cluster member: deleting it could
// leave a dead etcd member behind, so the person decides.
//
// The seam is the run only. Each provider keeps its own test and options, whose JSON the apps
// decode: Hetzner sells a type per location, OVHcloud a flavor per region under a plan code.

const (
	// cloudCreateTimeout bounds the run: the machine's start, then a join (joinTimeout).
	cloudCreateTimeout  = joinTimeout + 5*time.Minute
	cloudPollInterval   = 3 * time.Second
	cloudCleanupTimeout = 45 * time.Second
	cloudCleanupTries   = 3

	// Phases of the machine, before the join's.
	cloudPhaseCreating = "creating"
	cloudPhaseStarting = "starting"
	cloudPhaseOmni     = "omni"
	cloudPhaseCleanup  = "cleanup"
)

// cloudProvider is what differs between providers in a run.
type cloudProvider interface {
	// label names the provider in a message ("Hetzner"), machineWord what it sells ("server").
	label() string
	machineWord() string
	auditAction() auditAction
	// prepare checks everything that can be refused before a write and makes the user data.
	prepare(ctx context.Context, omni bool) (userData string, err error)
	create(ctx context.Context, userData string) error
	// waitRunning returns once the machine runs, with join.node set to its address.
	waitRunning(ctx context.Context) error
	// findUnanswered looks for the machine of a create with no clear answer.
	findUnanswered(ctx context.Context, why error)
	// machineID is the machine this run created, "" before.
	machineID() string
	deleteMachine(ctx context.Context) error
	// transient: worth trying again (rate limit, a server error, no answer).
	transient(err error) bool
}

// cloudCreateRun is one run, at the provider it is given.
type cloudCreateRun struct {
	provider cloudProvider
	join     *nodeJoin
	specErr  error
	poll     time.Duration
	omni     *bool // the provider reads it through its own field
	// omniHostnames lists the machines' host names (the Omni API; replaced in tests).
	omniHostnames func(context.Context, *session) ([]string, error)
}

func (c *cloudCreateRun) start(timeout time.Duration) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { c.join.listener.OnDone("", msg) })
		defer holdCalls(c.join.config, c.join.context)()

		err := recordedRun(c.join.config, c.join.context, c.provider.auditAction, func() error { return c.run(ctx, timeout) })
		if err != nil {
			c.join.listener.OnDone("", errText(err))

			return
		}

		if *c.omni {
			c.join.emit(joinPhaseDone, c.join.req.hostname+" appeared in Omni: allocate it there")
			c.join.listener.OnDone("", "")

			return
		}

		c.join.emit(joinPhaseDone, c.join.req.hostname+" joined as "+c.join.node)
		c.join.listener.OnDone(c.join.node, "")
	}()

	return &JoinRun{cancel: cancel}
}

func (c *cloudCreateRun) run(ctx context.Context, timeout time.Duration) error {
	if c.specErr != nil {
		return c.specErr
	}

	if isTalosDemoContext(c.join.config, c.join.context) {
		return errDemoUnavailable
	}

	_, cfgCtx, err := resolveContext(c.join.config, c.join.context)
	if err != nil {
		return err
	}

	*c.omni = isOmni(cfgCtx)

	userData, err := c.provider.prepare(ctx, *c.omni)
	if err != nil {
		return err
	}

	s, release, err := c.session()
	if err != nil {
		return err
	}

	defer release()

	if !*c.omni {
		if err := c.join.checkHostname(ctx, s); err != nil {
			return err
		}
	}

	if err := c.provider.create(ctx, userData); err != nil {
		return c.cleanup(c.ended(ctx, err, timeout))
	}

	if err := c.provider.waitRunning(ctx); err != nil {
		return c.cleanup(c.ended(ctx, err, timeout))
	}

	if *c.omni {
		err = c.waitOmni(ctx, s)
	} else {
		err = c.join.followInstalled(ctx, s)
	}

	if err != nil {
		return c.cleanup(c.ended(ctx, err, timeout))
	}

	return nil
}

// session is the run's session. On Omni it is only signed: Omni's API serves a cluster with no
// machine yet, whose member listing refuses; plain Talos needs the members for the host name check.
func (c *cloudCreateRun) session() (*session, func(), error) {
	if *c.omni {
		return acquireSigned(c.join.config, c.join.context)
	}

	return acquireSession(c.join.config, c.join.context)
}

// waitOmni polls Omni until a machine with the machine's name appears: Talos's cloud platforms
// take the host name from the provider, and Omni's machine status reports it.
func (c *cloudCreateRun) waitOmni(ctx context.Context, s *session) error {
	hostname := c.join.req.hostname

	c.join.emit(cloudPhaseOmni, "waiting for "+hostname+" to appear in Omni")

	list := c.omniHostnames
	if list == nil {
		list = omniMachineHostnames
	}

	for {
		hostnames, err := list(ctx, s)
		if err == nil && slices.ContainsFunc(hostnames, func(n string) bool { return strings.EqualFold(n, hostname) }) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.poll):
		}
	}
}

// ended turns the run's context ending into what happened to the machine.
func (c *cloudCreateRun) ended(ctx context.Context, err error, timeout time.Duration) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s did not come up within %s", c.join.req.hostname, timeout)
	case ctx.Err() != nil:
		return errors.New("stopped")
	}

	return err
}

// cleanup deletes what this run created and adds the outcome to why. The context is its own:
// the run's has often ended.
func (c *cloudCreateRun) cleanup(why error) error {
	ctx, cancel := context.WithTimeout(context.Background(), cloudCleanupTimeout)
	defer cancel()

	p := c.provider

	if p.machineID() == "" {
		p.findUnanswered(ctx, why)
	}

	id := p.machineID()
	if id == "" {
		return why
	}

	word := p.machineWord()
	named := strings.ToUpper(word[:1]) + word[1:] + " " + id

	if c.join.req.role == joinRoleControlPlane && c.join.answered {
		return fmt.Errorf("%w. %s is kept: it answered as a control plane and may already be an etcd member; remove the member before deleting it at %s", why, named, p.label())
	}

	c.join.emit(cloudPhaseCleanup, "deleting "+word+" "+id)

	var err error

	for try := range cloudCleanupTries {
		if err = p.deleteMachine(ctx); err == nil || !p.transient(err) {
			break
		}

		if try < cloudCleanupTries-1 {
			select {
			case <-ctx.Done():
			case <-time.After(c.poll):
			}
		}
	}

	if err != nil {
		// A delete the provider accepted but did not finish says so itself: friendlyError would
		// call its deadline a network timeout.
		reason := friendlyError(err)
		if _, ok := errors.AsType[unfinishedDelete](err); ok {
			reason = err.Error()
		}

		return fmt.Errorf("%w. %s could not be deleted (%s) and is still billed: delete it at %s", why, named, reason, p.label())
	}

	return fmt.Errorf("%w. %s was deleted", why, named)
}

// unfinishedDelete is a delete the provider accepted but had not finished in time
// (azureUnfinishedDelete, gcpUnfinishedDelete).
type unfinishedDelete interface {
	error
	unfinishedDelete()
}

// cloudNodeUserData is the node's user data. No activity entry of its own: the run's says it all.
func cloudNodeUserData(j *nodeJoin, omni bool, spec joinConfigRequest, editsJSON string) (cloudUserData, error) {
	if !omni {
		if privacy.isEnabled() {
			return cloudUserData{}, errJoinPrivacy
		}

		return talosCloudUserData(j.config, j.context, spec.template, spec.role, spec.hostname, spec.installDisk, editsJSON)
	}

	if privacy.isEnabled() {
		return cloudUserData{}, errCloudOmniPrivacy
	}

	// Signed only: the join config comes from Omni's API, which also serves a cluster with no
	// machine yet (its cloud images then become a warning).
	s, release, err := acquireSigned(j.config, j.context)
	if err != nil {
		return cloudUserData{}, err
	}

	defer release()

	parent := callParent(j.config, j.context)

	ctx, cancel := context.WithTimeout(parent, configPreviewTimeout)
	defer cancel()

	data, err := omniCloudUserData(ctx, s, spec.template, "", "", "", omniCreateLabels(spec))

	return scopedResult(parent, data, err)
}

// cloudJoinRequest is a create spec's new member, with its labelsJson (the JSON
// NodeCloudUserDataOmni takes, Omni's own prefix refused). A spec that did not decode keeps
// its own error.
// cloudOptionsJSON answers a provider's Options call: the demo's samples on the demo context,
// else read within timeout, given the cluster's Talos version (for the warning when no image
// carries it; "" without a cluster: no warning).
func cloudOptionsJSON[T any](configYAML, contextName string, timeout time.Duration, demo func() T, read func(ctx context.Context, version string) (T, error)) (string, error) {
	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demo())
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	version := ""
	if strings.TrimSpace(configYAML) != "" {
		if media, err := cloudBootMedia(configYAML, contextName, ""); err == nil {
			version = media.TalosVersion
		}
	}

	options, err := read(ctx, version)
	if err != nil {
		return "", err
	}

	return toJSON(options)
}

// cloudNodeSpec is the part of every provider's create request that describes the new node.
type cloudNodeSpec struct {
	Hostname     string `json:"hostname"`
	Role         string `json:"role"`
	TemplateNode string `json:"templateNode"`
	InstallDisk  string `json:"installDisk"`
	ExtraEdits   string `json:"extraEditsJSON"`
	LabelsJSON   string `json:"labelsJson"`
}

// newCloudJoin starts every Start<Provider>Create: it decodes specJSON into spec (the provider's
// request), unmasks the context and the template node, and builds the node's join. It returns
// the unmasked template node for spec, and the request's error ("invalid <machine> request"),
// which the run reports before any write.
func newCloudJoin(configYAML, contextName, specJSON string, spec any, machine string, listener JoinListener) (join *nodeJoin, template string, specErr error) {
	var node cloudNodeSpec

	specErr = json.Unmarshal([]byte(specJSON), spec)
	_ = json.Unmarshal([]byte(specJSON), &node) //nolint:errcheck // the same JSON: specErr has its error

	template = strings.TrimSpace(node.TemplateNode)
	contextName, template = unmaskTarget(configYAML, contextName, template)

	if specErr != nil {
		specErr = errors.New("invalid " + machine + " request")
	}

	req, specErr := cloudJoinRequest(template, node.Role, node.Hostname, node.InstallDisk, node.LabelsJSON, specErr)

	join = &nodeJoin{
		config: configYAML, context: contextName, listener: maskedJoinListener{listener}, interval: upgradePollInterval,
		req:       req,
		editsJSON: node.ExtraEdits,
	}

	return join, template, specErr
}

func cloudJoinRequest(template, role, hostname, installDisk, labelsJSON string, specErr error) (joinConfigRequest, error) {
	req := joinConfigRequest{template: template, role: role, hostname: hostname, installDisk: installDisk}
	if specErr != nil {
		return req, specErr
	}

	labels, err := parseOmniLabels(labelsJSON)
	if err != nil {
		return req, err
	}

	req.labels = labels

	return req, nil
}

// omniCreateLabels are the labels signed into the created machine's join token: the chosen
// ones, plus the card's role as ichor/role when no label gives one, so a machine class matching
// on it picks the machine.
func omniCreateLabels(spec joinConfigRequest) map[string]string {
	role := strings.TrimSpace(spec.role)
	if _, given := spec.labels[omniRoleLabel]; given || (role != joinRoleWorker && role != joinRoleControlPlane) {
		return spec.labels
	}

	labels := make(map[string]string, len(spec.labels)+1)
	maps.Copy(labels, spec.labels)
	labels[omniRoleLabel] = role

	return labels
}
