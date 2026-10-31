package ichorgo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Creating a billable server for a new node. The user data (Omni's join config, or a new
// member's config) goes with POST /servers, so the node never sits in maintenance mode on a
// public address. The run follows the server until it is running, then the node until it joins
// (plain Talos) or appears in Omni. A run that fails or is stopped deletes the one server it
// created, except a control plane that already answered as a cluster member: deleting it could
// leave a dead etcd member behind, so the person decides.

const (
	// hetznerCreateTimeout bounds the run: the server's start, then a join (joinTimeout).
	hetznerCreateTimeout  = joinTimeout + 5*time.Minute
	hetznerPollInterval   = 3 * time.Second
	hetznerCleanupTimeout = 45 * time.Second
	hetznerCleanupTries   = 3

	hetznerStatusRunning = "running"
	hetznerStatusError   = "error"

	// Phases of the server, before the join's.
	hetznerPhaseCreating = "creating"
	hetznerPhaseStarting = "starting"
	hetznerPhaseOmni     = "omni"
	hetznerPhaseCleanup  = "cleanup"
)

// hetznerCreateSpec is StartHetznerCreate's specJSON.
type hetznerCreateSpec struct {
	ServerType   string  `json:"serverType"`
	Location     string  `json:"location"`
	SnapshotID   int64   `json:"snapshotId"`
	Hostname     string  `json:"hostname"`
	Role         string  `json:"role"`
	TemplateNode string  `json:"templateNode"`
	InstallDisk  string  `json:"installDisk"`
	ExtraEdits   string  `json:"extraEditsJSON"`
	SSHKeyIDs    []int64 `json:"sshKeyIds"`
}

// hetznerServer is the part of a server the run reads.
type hetznerServer struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	PublicNet struct {
		IPv4 *struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
		IPv6 *struct {
			IP string `json:"ip"` // a network: 2001:db8::/64
		} `json:"ipv6"`
	} `json:"public_net"`
}

// address is the server's public address, the first of its IPv6 network without an IPv4.
func (s hetznerServer) address() string {
	if s.PublicNet.IPv4 != nil && s.PublicNet.IPv4.IP != "" {
		return s.PublicNet.IPv4.IP
	}

	if s.PublicNet.IPv6 != nil {
		if p, err := netip.ParsePrefix(s.PublicNet.IPv6.IP); err == nil {
			return p.Masked().Addr().Next().String()
		}
	}

	return ""
}

type hetznerAction struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// StartHetznerCreate creates a Hetzner server from one of the project's snapshots with the new
// node's user data, and follows it: creating → starting → (plain Talos) rebooting → booted →
// joining → registering → done, or (Omni) omni → done. specJSON: {"serverType","location",
// "snapshotId","hostname","role","templateNode","installDisk","extraEditsJSON","sshKeyIds"}.
// tokenJSON: {"token"}, a Read & Write token (a read-only one is refused, as is a snapshot of
// another architecture, or user data over 32 KiB, all before any write). OnDone gets the node's
// address to add to the context ("" on Omni, which keeps its own list). A failed or cancelled
// run deletes the one server it created (see the file's comment for the exception). Refused on
// the demo and in privacy mode; recorded in the activity log as hetzner-server-create, never
// with the config or the token. Billable: the app confirms the cost and the hostname first.
func StartHetznerCreate(configYAML, contextName, tokenJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec hetznerCreateSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)

	template := strings.TrimSpace(spec.TemplateNode)
	contextName, template = unmaskTarget(configYAML, contextName, template)
	spec.TemplateNode = template

	listener = maskedJoinListener{listener}

	if specErr != nil {
		specErr = errors.New("invalid server request")
	}

	h := &hetznerCreate{
		join: &nodeJoin{
			config: configYAML, context: contextName, listener: listener, interval: upgradePollInterval,
			req: joinConfigRequest{
				template: spec.TemplateNode, role: spec.Role, hostname: spec.Hostname, installDisk: spec.InstallDisk,
			},
			editsJSON: spec.ExtraEdits,
		},
		spec: spec, specErr: specErr, tokenJSON: tokenJSON, poll: hetznerPollInterval,
	}

	return h.start(hetznerCreateTimeout)
}

// hetznerCreate is one run of StartHetznerCreate.
type hetznerCreate struct {
	join      *nodeJoin
	spec      hetznerCreateSpec
	specErr   error
	tokenJSON string
	poll      time.Duration
	client    hetznerClient
	omni      bool
	runID     string
	serverID  int64 // the server this run created, 0 before
	// omniHostnames lists the machines' host names (the Omni API; replaced in tests).
	omniHostnames func(context.Context, *session) ([]string, error)
}

func (h *hetznerCreate) start(timeout time.Duration) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { h.join.listener.OnDone("", msg) })
		defer holdCalls(h.join.config, h.join.context)()

		err := recordedRun(h.join.config, h.join.context, h.auditAction, func() error { return h.run(ctx, timeout) })
		if err != nil {
			h.join.listener.OnDone("", errText(err))

			return
		}

		if h.omni {
			h.join.emit(joinPhaseDone, h.spec.Hostname+" appeared in Omni: allocate it there")
			h.join.listener.OnDone("", "")

			return
		}

		h.join.emit(joinPhaseDone, h.spec.Hostname+" joined as "+h.join.node)
		h.join.listener.OnDone(h.join.node, "")
	}()

	return &JoinRun{cancel: cancel}
}

func (h *hetznerCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s type=%s location=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.ServerType, h.spec.Location)
	if h.serverID != 0 {
		params += " server=" + strconv.FormatInt(h.serverID, 10)
	}

	return auditAction{Action: "hetzner-server-create", Node: h.join.node, Params: params}
}

func (h *hetznerCreate) run(ctx context.Context, timeout time.Duration) error {
	if h.specErr != nil {
		return h.specErr
	}

	if isTalosDemoContext(h.join.config, h.join.context) {
		return errDemoUnavailable
	}

	_, cfgCtx, err := resolveContext(h.join.config, h.join.context)
	if err != nil {
		return err
	}

	h.omni = isOmni(cfgCtx)

	userData, err := h.prepare(ctx)
	if err != nil {
		return err
	}

	s, release, err := acquireSession(h.join.config, h.join.context)
	if err != nil {
		return err
	}

	defer release()

	if !h.omni {
		if err := h.join.checkHostname(ctx, s); err != nil {
			return err
		}
	}

	action, err := h.create(ctx, userData)
	if err != nil {
		return h.cleanup(h.ended(ctx, err, timeout))
	}

	if err := h.waitRunning(ctx, action); err != nil {
		return h.cleanup(h.ended(ctx, err, timeout))
	}

	if h.omni {
		err = h.waitOmni(ctx, s)
	} else {
		err = h.join.followInstalled(ctx, s)
	}

	if err != nil {
		return h.cleanup(h.ended(ctx, err, timeout))
	}

	return nil
}

// prepare checks everything that can be refused before a write and makes the user data.
func (h *hetznerCreate) prepare(ctx context.Context) (string, error) {
	h.spec.Hostname = strings.TrimSpace(h.spec.Hostname)
	h.join.req.hostname = h.spec.Hostname

	if !validHostname(h.spec.Hostname) || strings.Contains(h.spec.Hostname, ".") {
		return "", fmt.Errorf("%q is not a host name (letters, digits and dashes)", h.spec.Hostname)
	}

	if h.spec.SnapshotID <= 0 || h.spec.ServerType == "" || h.spec.Location == "" {
		return "", errors.New("pick a server type, a location and a snapshot")
	}

	client, err := newHetznerClient(h.tokenJSON)
	if err != nil {
		return "", err
	}

	h.client = client

	readOnly, err := client.readOnly(ctx)
	if err != nil {
		return "", err
	}

	if readOnly {
		return "", errHetznerReadOnly
	}

	if err := h.checkServerChoice(ctx); err != nil {
		return "", err
	}

	data, err := h.userData()
	if err != nil {
		return "", err
	}

	if err := checkUserDataSize(data); err != nil {
		return "", err
	}

	return data.UserData, nil
}

func checkUserDataSize(d cloudUserData) error {
	if d.Size > cloudUserDataLimit {
		return fmt.Errorf("the user data is %d bytes, over Hetzner's 32 KiB: remove extra edits, or the config's docs and examples on the template", d.Size)
	}

	return nil
}

// checkServerChoice refuses a snapshot of another architecture than the server type, and a
// location the type has no price in.
func (h *hetznerCreate) checkServerChoice(ctx context.Context) error {
	types, err := hetznerList[hetznerAPIType](ctx, h.client, hetznerTypePath(h.spec.ServerType), "server_types")
	if err != nil {
		return err
	}

	i := slices.IndexFunc(types, func(t hetznerAPIType) bool { return t.Name == h.spec.ServerType })
	if i < 0 {
		return fmt.Errorf("no server type %q at Hetzner", h.spec.ServerType)
	}

	if !slices.ContainsFunc(types[i].Prices, func(p hetznerAPIPrice) bool { return p.Location == h.spec.Location }) {
		return fmt.Errorf("server type %s is not sold in %s", h.spec.ServerType, h.spec.Location)
	}

	var image struct {
		Image hetznerAPIImage `json:"image"`
	}

	if err := h.client.do(ctx, http.MethodGet, hetznerImagePath(h.spec.SnapshotID), nil, &image); err != nil {
		return err
	}

	if image.Image.Architecture != types[i].Architecture {
		return fmt.Errorf("snapshot %d is for %s, server type %s for %s: pick another", h.spec.SnapshotID,
			hetznerTalosArch(image.Image.Architecture), h.spec.ServerType, hetznerTalosArch(types[i].Architecture))
	}

	return nil
}

// userData is the node's user data. No activity entry of its own: the run's says it all.
func (h *hetznerCreate) userData() (cloudUserData, error) {
	if !h.omni {
		if privacy.isEnabled() {
			return cloudUserData{}, errJoinPrivacy
		}

		return talosCloudUserData(h.join.config, h.join.context, h.spec.TemplateNode, h.spec.Role, h.spec.Hostname, h.spec.InstallDisk, h.spec.ExtraEdits)
	}

	if privacy.isEnabled() {
		return cloudUserData{}, errCloudOmniPrivacy
	}

	return withSession(h.join.config, h.join.context, configPreviewTimeout, func(ctx context.Context, s *session) (cloudUserData, error) {
		return omniCloudUserData(ctx, s, h.spec.TemplateNode, "", "", "")
	})
}

// create sends POST /servers, once. A server that may exist without an answer is found by
// the run's label afterwards (cleanup).
func (h *hetznerCreate) create(ctx context.Context, userData string) (hetznerAction, error) {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return hetznerAction{}, err
	}

	h.runID = hex.EncodeToString(id)

	body := map[string]any{
		"name": h.spec.Hostname, "server_type": h.spec.ServerType, "location": h.spec.Location,
		"image": strconv.FormatInt(h.spec.SnapshotID, 10), "user_data": userData,
		"labels": map[string]string{"ichor-run": h.runID, "ichor-cluster": hetznerClusterLabel(h.join.context)},
	}

	if len(h.spec.SSHKeyIDs) > 0 {
		body["ssh_keys"] = h.spec.SSHKeyIDs
	}

	h.join.emit(hetznerPhaseCreating, "creating "+h.spec.Hostname+" ("+h.spec.ServerType+" in "+h.spec.Location+")")

	var resp struct {
		Server hetznerServer `json:"server"`
		Action hetznerAction `json:"action"`
	}

	if err := h.client.do(ctx, http.MethodPost, "/servers", body, &resp); err != nil {
		var e *hetznerError
		if errors.As(err, &e) && e.status == http.StatusForbidden && e.code == hetznerCodeForbidden {
			return hetznerAction{}, errHetznerReadOnly
		}

		return hetznerAction{}, err
	}

	h.serverID = resp.Server.ID
	h.join.applied = true

	return resp.Action, nil
}

// hetznerClusterLabel names the cluster on the server without showing it to the provider.
func hetznerClusterLabel(contextName string) string {
	sum := sha256.Sum256([]byte(contextName))

	return hex.EncodeToString(sum[:6])
}

// waitRunning follows the create action, then the server until it runs and has an address. A
// provider hiccup is polled through: only a failed action or the run's end stops it.
func (h *hetznerCreate) waitRunning(ctx context.Context, action hetznerAction) error {
	h.join.emit(hetznerPhaseStarting, "waiting for server "+strconv.FormatInt(h.serverID, 10)+" to start")

	for done := action.ID == 0; ; {
		if !done {
			var a struct {
				Action hetznerAction `json:"action"`
			}

			err := h.client.do(ctx, http.MethodGet, "/actions/"+strconv.FormatInt(action.ID, 10), nil, &a)

			switch {
			case err != nil && !hetznerTransient(err):
				return err
			case err == nil && a.Action.Status == hetznerStatusError:
				if a.Action.Error != nil {
					return errors.New("the server could not be created: " + a.Action.Error.Message)
				}

				return errors.New("the server could not be created")
			case err == nil && a.Action.Status == "success":
				done = true
			}
		}

		if done {
			var s struct {
				Server hetznerServer `json:"server"`
			}

			err := h.client.do(ctx, http.MethodGet, "/servers/"+strconv.FormatInt(h.serverID, 10), nil, &s)

			switch {
			case err != nil && !hetznerTransient(err):
				return err
			case err == nil && s.Server.Status == hetznerStatusRunning && s.Server.address() != "":
				h.join.node = normalizeEndpoint(s.Server.address())
				h.join.emit(hetznerPhaseStarting, "server "+strconv.FormatInt(h.serverID, 10)+" is running at "+h.join.node)

				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

// hetznerTransient: worth polling again (rate limit, a server error, no answer).
func hetznerTransient(err error) bool {
	var e *hetznerError
	if errors.As(err, &e) {
		return e.status == http.StatusTooManyRequests || e.status >= http.StatusInternalServerError
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// waitOmni polls Omni until a machine with the server's name appears: Talos's hcloud platform
// takes its host name from the server, and Omni's machine status reports it.
func (h *hetznerCreate) waitOmni(ctx context.Context, s *session) error {
	h.join.emit(hetznerPhaseOmni, "waiting for "+h.spec.Hostname+" to appear in Omni")

	list := h.omniHostnames
	if list == nil {
		list = omniMachineHostnames
	}

	for {
		hostnames, err := list(ctx, s)
		if err == nil && slices.ContainsFunc(hostnames, func(n string) bool { return strings.EqualFold(n, h.spec.Hostname) }) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

// ended turns the run's context ending into what happened to the server.
func (h *hetznerCreate) ended(ctx context.Context, err error, timeout time.Duration) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s did not come up within %s", h.spec.Hostname, timeout)
	case ctx.Err() != nil:
		return errors.New("stopped")
	}

	return err
}

// cleanup deletes what this run created and adds the outcome to why. The context is its own:
// the run's has often ended.
func (h *hetznerCreate) cleanup(why error) error {
	ctx, cancel := context.WithTimeout(context.Background(), hetznerCleanupTimeout)
	defer cancel()

	if h.serverID == 0 {
		h.findUnanswered(ctx, why)
	}

	if h.serverID == 0 {
		return why
	}

	id := strconv.FormatInt(h.serverID, 10)

	if h.spec.Role == joinRoleControlPlane && h.join.answered {
		return fmt.Errorf("%w. Server %s is kept: it answered as a control plane and may already be an etcd member; remove the member before deleting it at Hetzner", why, id)
	}

	h.join.emit(hetznerPhaseCleanup, "deleting server "+id)

	var err error

	for try := range hetznerCleanupTries {
		if err = h.client.do(ctx, http.MethodDelete, "/servers/"+id, nil, nil); err == nil || !hetznerTransient(err) {
			break
		}

		if try < hetznerCleanupTries-1 {
			select {
			case <-ctx.Done():
			case <-time.After(h.poll):
			}
		}
	}

	if err != nil {
		return fmt.Errorf("%w. Server %s could not be deleted (%s) and is still billed: delete it at Hetzner", why, id, friendlyError(err))
	}

	return fmt.Errorf("%w. Server %s was deleted", why, id)
}

// findUnanswered looks for the server of a POST with no clear answer, by the run's label. A
// refusal (4xx) created nothing, and nothing is looked up.
func (h *hetznerCreate) findUnanswered(ctx context.Context, why error) {
	var e *hetznerError
	if h.runID == "" || (errors.As(why, &e) && e.status < http.StatusInternalServerError) {
		return
	}

	servers, err := hetznerList[hetznerServer](ctx, h.client, "/servers?label_selector=ichor-run%3D"+h.runID, "servers")
	if err == nil && len(servers) == 1 {
		h.serverID = servers[0].ID
	}
}
