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

// Creating a Hetzner server for a new node: the provider's half of cloudCreateRun.

const (
	hetznerStatusRunning = "running"
	hetznerStatusError   = "error"

	// The run's phases, as the snapshot run names them too.
	hetznerPhaseCreating = cloudPhaseCreating
	hetznerPhaseStarting = cloudPhaseStarting
	hetznerPhaseCleanup  = cloudPhaseCleanup
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
		spec: spec, specErr: specErr, tokenJSON: tokenJSON, poll: cloudPollInterval,
	}

	return h.start(cloudCreateTimeout)
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
	action    hetznerAction
	// omniHostnames lists the machines' host names (the Omni API; replaced in tests).
	omniHostnames func(context.Context, *session) ([]string, error)
}

// run is the shared run, with this provider's half.
func (h *hetznerCreate) run() *cloudCreateRun {
	return &cloudCreateRun{provider: h, join: h.join, specErr: h.specErr, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}
}

func (h *hetznerCreate) start(timeout time.Duration) *JoinRun { return h.run().start(timeout) }

func (h *hetznerCreate) waitOmni(ctx context.Context, s *session) error {
	return h.run().waitOmni(ctx, s)
}

func (h *hetznerCreate) label() string { return "Hetzner" }

func (h *hetznerCreate) machineWord() string { return "server" }

func (h *hetznerCreate) transient(err error) bool { return hetznerTransient(err) }

func (h *hetznerCreate) machineID() string {
	if h.serverID == 0 {
		return ""
	}

	return strconv.FormatInt(h.serverID, 10)
}

func (h *hetznerCreate) deleteMachine(ctx context.Context) error {
	return h.client.do(ctx, http.MethodDelete, "/servers/"+h.machineID(), nil, nil)
}

func (h *hetznerCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s type=%s location=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.ServerType, h.spec.Location)
	if h.serverID != 0 {
		params += " server=" + strconv.FormatInt(h.serverID, 10)
	}

	return auditAction{Action: "hetzner-server-create", Node: h.join.node, Params: params}
}

// prepare checks everything that can be refused before a write and makes the user data.
func (h *hetznerCreate) prepare(ctx context.Context, omni bool) (string, error) {
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

	data, err := cloudNodeUserData(h.join, omni, h.join.req, h.spec.ExtraEdits)
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

// create sends POST /servers, once. A server that may exist without an answer is found by
// the run's label afterwards (cleanup).
func (h *hetznerCreate) create(ctx context.Context, userData string) error {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
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
			return errHetznerReadOnly
		}

		return err
	}

	h.serverID = resp.Server.ID
	h.action = resp.Action
	h.join.applied = true

	return nil
}

// hetznerClusterLabel names the cluster on the server without showing it to the provider.
func hetznerClusterLabel(contextName string) string {
	sum := sha256.Sum256([]byte(contextName))

	return hex.EncodeToString(sum[:6])
}

// waitRunning follows the create action, then the server until it runs and has an address. A
// provider hiccup is polled through: only a failed action or the run's end stops it.
func (h *hetznerCreate) waitRunning(ctx context.Context) error {
	action := h.action

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
