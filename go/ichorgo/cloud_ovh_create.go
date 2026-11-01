package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Creating an OVHcloud Public Cloud instance for a new node: the provider's half of cloudCreateRun.

const (
	// ovhUserDataLimit is OpenStack's, Nova's 64 KiB of user data once encoded: a safe 48 KiB.
	ovhUserDataLimit = 48 << 10

	ovhPollInterval = 5 * time.Second

	ovhStatusActive = "ACTIVE"
	ovhStatusError  = "ERROR"
)

// ovhCreateSpec is StartOVHCreate's specJSON.
type ovhCreateSpec struct {
	Region       string `json:"region"`
	FlavorID     string `json:"flavorId"`
	ImageID      string `json:"imageId"`
	Hostname     string `json:"hostname"`
	Role         string `json:"role"`
	TemplateNode string `json:"templateNode"`
	InstallDisk  string `json:"installDisk"`
	ExtraEdits   string `json:"extraEditsJSON"`
	SSHKeyID     string `json:"sshKeyId"`
}

// ovhInstance is the part of an instance the run reads.
type ovhInstance struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	IPAddresses []struct {
		IP      string `json:"ip"`
		Version int    `json:"version"`
		Type    string `json:"type"`
	} `json:"ipAddresses"`
}

// address is the instance's public IPv4.
func (i ovhInstance) address() string {
	for _, a := range i.IPAddresses {
		if a.Type == "public" && a.Version == 4 && a.IP != "" {
			return a.IP
		}
	}

	return ""
}

// StartOVHCreate creates an OVHcloud Public Cloud instance from an image of the project with the
// new node's user data, and follows it: creating → starting → (plain Talos) rebooting → booted →
// joining → registering → done, or (Omni) omni → done. specJSON: {"region","flavorId","imageId",
// "hostname","role","templateNode","installDisk","extraEditsJSON","sshKeyId"}. credJSON: as
// OVHTest's (an account that may not create is refused, as is user data over 48 KiB, before any
// write). OnDone gets the node's address to add to the context ("" on Omni, which keeps its own
// list). A failed or cancelled run deletes the one instance it created (see cloudCreateRun for the
// exception). Refused on the demo and in privacy mode; recorded in the activity log as
// ovh-instance-create, never with the config or the account. Billable: the app confirms the cost
// and the hostname first.
func StartOVHCreate(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec ovhCreateSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)

	template := strings.TrimSpace(spec.TemplateNode)
	contextName, template = unmaskTarget(configYAML, contextName, template)
	spec.TemplateNode = template

	listener = maskedJoinListener{listener}

	if specErr != nil {
		specErr = errors.New("invalid instance request")
	}

	h := &ovhCreate{
		join: &nodeJoin{
			config: configYAML, context: contextName, listener: listener, interval: upgradePollInterval,
			req: joinConfigRequest{
				template: spec.TemplateNode, role: spec.Role, hostname: spec.Hostname, installDisk: spec.InstallDisk,
			},
			editsJSON: spec.ExtraEdits,
		},
		spec: spec, specErr: specErr, credJSON: credJSON, poll: ovhPollInterval,
	}

	return h.start(cloudCreateTimeout)
}

// ovhCreate is one run of StartOVHCreate.
type ovhCreate struct {
	join       *nodeJoin
	spec       ovhCreateSpec
	specErr    error
	credJSON   string
	poll       time.Duration
	client     *ovhClient
	omni       bool
	instanceID string // the instance this run created, "" before
}

func (h *ovhCreate) start(timeout time.Duration) *JoinRun {
	run := &cloudCreateRun{provider: h, join: h.join, specErr: h.specErr, poll: h.poll, omni: &h.omni}

	return run.start(timeout)
}

func (h *ovhCreate) label() string { return "OVHcloud" }

func (h *ovhCreate) machineWord() string { return "instance" }

func (h *ovhCreate) machineID() string { return h.instanceID }

func (h *ovhCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s flavor=%s region=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.FlavorID, h.spec.Region)
	if h.instanceID != "" {
		params += " instance=" + h.instanceID
	}

	return auditAction{Action: "ovh-instance-create", Node: h.join.node, Params: params}
}

func (h *ovhCreate) prepare(ctx context.Context, omni bool) (string, error) {
	h.spec.Hostname = strings.TrimSpace(h.spec.Hostname)
	h.join.req.hostname = h.spec.Hostname

	if !validHostname(h.spec.Hostname) || strings.Contains(h.spec.Hostname, ".") {
		return "", fmt.Errorf("%q is not a host name (letters, digits and dashes)", h.spec.Hostname)
	}

	if h.spec.Region == "" || h.spec.FlavorID == "" || h.spec.ImageID == "" {
		return "", errors.New("pick a region, a flavor and an image")
	}

	client, err := newOVHClient(h.credJSON)
	if err != nil {
		return "", err
	}

	h.client = client

	if _, err := client.pickProject(ctx); err != nil {
		return "", err
	}

	can, checked, err := client.canCreate(ctx)
	if err != nil {
		return "", err
	}

	if checked && !can {
		return "", errOVHNoCreate
	}

	if err := h.checkChoice(ctx); err != nil {
		return "", err
	}

	data, err := cloudNodeUserData(h.join, omni, h.join.req, h.spec.ExtraEdits)
	if err != nil {
		return "", err
	}

	if data.Size > ovhUserDataLimit {
		return "", fmt.Errorf("the user data is %d bytes, over the 48 KiB OpenStack safely takes: remove extra edits, or the config's docs and examples on the template", data.Size)
	}

	return data.UserData, nil
}

// checkChoice refuses a flavor or an image of another region.
func (h *ovhCreate) checkChoice(ctx context.Context) error {
	region := "?region=" + url.QueryEscape(h.spec.Region)

	var flavors []ovhAPIFlavor
	if err := h.client.do(ctx, http.MethodGet, h.client.path("/flavor"+region), nil, &flavors); err != nil {
		return err
	}

	if !slices.ContainsFunc(flavors, func(f ovhAPIFlavor) bool { return f.ID == h.spec.FlavorID && f.Available }) {
		return fmt.Errorf("this flavor cannot be ordered in %s: pick another", h.spec.Region)
	}

	var images []ovhAPIImage
	if err := h.client.do(ctx, http.MethodGet, h.client.path("/image"+region), nil, &images); err != nil {
		return err
	}

	if !slices.ContainsFunc(images, func(i ovhAPIImage) bool { return i.ID == h.spec.ImageID }) {
		return fmt.Errorf("this image is not in %s: pick another", h.spec.Region)
	}

	return nil
}

// create sends the POST, once. Instances have no labels, and another may carry the same name: a
// POST with no clear answer is not looked for, the error says to check the project.
func (h *ovhCreate) create(ctx context.Context, userData string) error {
	body := map[string]any{
		"name": h.spec.Hostname, "region": h.spec.Region, "flavorId": h.spec.FlavorID, "imageId": h.spec.ImageID,
		"userData": userData, "monthlyBilling": false,
	}

	if h.spec.SSHKeyID != "" {
		body["sshKeyId"] = h.spec.SSHKeyID
	}

	h.join.emit(cloudPhaseCreating, "creating "+h.spec.Hostname+" in "+h.spec.Region)

	var instance ovhInstance

	if err := h.client.do(ctx, http.MethodPost, h.client.path("/instance"), body, &instance); err != nil {
		var e *ovhError
		if errors.As(err, &e) && e.status < http.StatusInternalServerError {
			return err
		}

		return fmt.Errorf("%w. An instance named %s may exist without an answer: check the project at OVHcloud", err, h.spec.Hostname)
	}

	if instance.ID == "" {
		return fmt.Errorf("OVHcloud answered without an instance id: check the project for %s", h.spec.Hostname)
	}

	h.instanceID = instance.ID
	h.join.applied = true

	return nil
}

// waitRunning follows the instance until it is active and has a public address. A provider hiccup
// is polled through: only a failed instance or the run's end stops it.
func (h *ovhCreate) waitRunning(ctx context.Context) error {
	h.join.emit(cloudPhaseStarting, "waiting for instance "+h.instanceID+" to start")

	last := ""

	for {
		var i ovhInstance

		err := h.client.do(ctx, http.MethodGet, h.client.path("/instance/"+url.PathEscape(h.instanceID)), nil, &i)

		switch {
		case err != nil && !h.transient(err):
			return err
		case err == nil && i.Status == ovhStatusError:
			return errors.New("OVHcloud could not build the instance")
		case err == nil && i.Status == ovhStatusActive && i.address() != "":
			h.join.node = normalizeEndpoint(i.address())
			h.join.emit(cloudPhaseStarting, "instance "+h.instanceID+" is active at "+h.join.node)

			return nil
		case err == nil && i.Status != last:
			last = i.Status
			h.join.emit(cloudPhaseStarting, "instance "+h.instanceID+": "+i.Status)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

// transient: worth polling again (rate limit, a server error, no answer).
func (h *ovhCreate) transient(err error) bool {
	var e *ovhError
	if errors.As(err, &e) {
		return e.status == http.StatusTooManyRequests || e.status >= http.StatusInternalServerError
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (h *ovhCreate) findUnanswered(context.Context, error) {}

func (h *ovhCreate) deleteMachine(ctx context.Context) error {
	return h.client.do(ctx, http.MethodDelete, h.client.path("/instance/"+url.PathEscape(h.instanceID)), nil, nil)
}
