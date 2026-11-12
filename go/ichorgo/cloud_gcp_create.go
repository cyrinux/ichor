package ichorgo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Creating a Compute Engine VM for a new node: the provider's half of cloudCreateRun.

const (
	// gcpUserDataLimit is Compute Engine's: one metadata value of at most 256 KiB.
	gcpUserDataLimit = 256 << 10

	gcpPollInterval = 5 * time.Second
	// gcpNotFoundGrace: Compute Engine may answer 404 for an instance it is making, for a moment.
	gcpNotFoundGrace = 30 * time.Second
	// gcpInsertTries: an insert without a clear answer is sent again with the same requestId,
	// which Google makes a no-op when the first one went through.
	gcpInsertTries = 3
	// gcpSerialTail is how much of the serial console a failed boot's message quotes.
	gcpSerialTail = 2 << 10

	gcpDefaultDiskGB = 20
	gcpMinDiskGB     = 10
	gcpMaxDiskGB     = 65536

	gcpStatusRunning = "RUNNING"
)

// gcpDeleteWait bounds the wait for an accepted delete. Past it the delete is reported as
// unconfirmed, never as done (a test shortens it).
var gcpDeleteWait = 5 * time.Minute

// gcpCreateSpec is StartGCPCreate's specJSON.
type gcpCreateSpec struct {
	Project           string `json:"project"`
	Zone              string `json:"zone"`
	MachineType       string `json:"machineType"`
	Image             string `json:"image"`
	Subnetwork        string `json:"subnetwork"`
	AssociatePublicIP bool   `json:"associatePublicIp"`
	DiskGB            int    `json:"diskGB"`
	Hostname          string `json:"hostname"`
	Role              string `json:"role"`
	TemplateNode      string `json:"templateNode"`
	InstallDisk       string `json:"installDisk"`
	ExtraEdits        string `json:"extraEditsJSON"`
	LabelsJSON        string `json:"labelsJson"`
}

// gcpAPIInstance is the part of an instance the run reads.
type gcpAPIInstance struct {
	Status            string            `json:"status"`
	StatusMessage     string            `json:"statusMessage"`
	Labels            map[string]string `json:"labels"`
	NetworkInterfaces []struct {
		NetworkIP     string `json:"networkIP"`
		AccessConfigs []struct {
			Type  string `json:"type"`
			NatIP string `json:"natIP"`
		} `json:"accessConfigs"`
	} `json:"networkInterfaces"`
}

// addresses are the instance's external (ONE_TO_ONE_NAT) and internal IPv4.
func (i gcpAPIInstance) addresses() (public, private string) {
	if len(i.NetworkInterfaces) == 0 {
		return "", ""
	}

	n := i.NetworkInterfaces[0]
	for _, a := range n.AccessConfigs {
		if a.Type == "ONE_TO_ONE_NAT" && a.NatIP != "" {
			public = a.NatIP
		}
	}

	return public, n.NetworkIP
}

// StartGCPCreate creates a Compute Engine VM from a Talos image the project holds, with the new
// node's config as the user-data metadata value, and follows it: creating → starting → (plain
// Talos) rebooting → booted → joining → registering → done, or (Omni) omni → done. specJSON:
// {"project","zone","machineType","image","subnetwork","associatePublicIp","diskGB","hostname",
// "role","templateNode","installDisk","extraEditsJSON","labelsJson"} (project "" for the credential's; image
// a name or a link of the project; subnetwork a link, or a name of the zone's region; diskGB 0
// for 20). credJSON: as GCPTest's. Refused before any write: an image the machine type cannot
// boot (architecture, UEFI for Arm), a subnet of another region, an instance of the host's name
// in the zone, user data over 256 KiB, then a key whose testIamPermissions lacks a permission the
// run uses. The run makes one instance named after the host name (Talos takes the host name from
// it), with its boot disk (autoDelete), labelled ichor-run and ichor-cluster (a hash of the
// context name) and tagged ichor-node; one insert, with a requestId that makes a retry a no-op.
// An insert with no clear answer is found by name and kept only when its ichor-run label is the
// run's. OnDone gets the node's address to add to the context ("" on Omni). A failed or stopped
// run deletes the instance (see cloudCreateRun for the exception); a delete Google has not
// finished in five minutes is reported as still billed. Refused on the demo and in privacy mode;
// recorded in the activity log as gcp-instance-create, never with the config or the key.
// Billable: the app confirms the cost and the hostname first.
func StartGCPCreate(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec gcpCreateSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)
	contextName, spec.TemplateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(spec.TemplateNode))
	listener = maskedJoinListener{listener}

	join, specErr := newCloudJoin(configYAML, contextName, spec.TemplateNode, specJSON, specErr, "VM", listener)

	h := &gcpCreate{
		join: join,
		spec: spec, specErr: specErr, credJSON: credJSON, poll: gcpPollInterval,
	}

	return h.start(cloudCreateTimeout)
}

// gcpCreate is one run of StartGCPCreate.
type gcpCreate struct {
	join      *nodeJoin
	spec      gcpCreateSpec
	specErr   error
	credJSON  string
	poll      time.Duration
	client    *gcpClient
	omni      bool
	runID     string
	requestID string
	// imageName, subnet*: the spec's image and subnet, read by checkSpec.
	imageName                               string
	subnetProject, subnetRegion, subnetName string
	created                                 bool   // the insert was accepted: the instance is this run's
	unclear                                 bool   // the insert got no clear answer: looked up by name and label
	operation                               string // the insert's zone operation
	insertedAt                              time.Time
	noRoute                                 bool // no public address and no Cloud NAT on the subnet's network
	// omniHostnames lists the machines' host names (the Omni API; replaced in tests).
	omniHostnames func(context.Context, *session) ([]string, error)
}

func (h *gcpCreate) start(timeout time.Duration) *JoinRun {
	run := &cloudCreateRun{provider: h, join: h.join, specErr: h.specErr, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}

	return run.start(timeout)
}

func (h *gcpCreate) label() string { return "Google Cloud (zone " + h.spec.Zone + ")" }

func (h *gcpCreate) machineWord() string { return "instance" }

// machineID is the instance's path with the project hidden, "" before the insert is accepted.
func (h *gcpCreate) machineID() string {
	if !h.created {
		return ""
	}

	return "projects/…/zones/" + h.spec.Zone + "/instances/" + h.spec.Hostname
}

func (h *gcpCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s machineType=%s zone=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.MachineType, h.spec.Zone)
	if h.created {
		params += " instance=" + h.machineID()
	}

	return auditAction{Action: "gcp-instance-create", Node: h.join.node, Params: params}
}

func (h *gcpCreate) instanceURL() string {
	return h.client.compute(h.spec.Project, "/zones/"+h.spec.Zone+"/instances/"+h.spec.Hostname)
}

// prepare checks everything that can be refused before a write and makes the user data.
func (h *gcpCreate) prepare(ctx context.Context, omni bool) (string, error) {
	h.spec.Hostname = strings.TrimSpace(h.spec.Hostname)
	h.join.req.hostname = h.spec.Hostname

	name := h.spec.Hostname
	if !validHostname(name) || !gcpNamePattern.MatchString(name) {
		return "", fmt.Errorf("%q is not a host name Compute Engine takes (lowercase letters, digits and dashes, starting with a letter, at most 63)", name)
	}

	client, err := newGCPClient(h.credJSON)
	if err != nil {
		return "", err
	}

	h.client = client

	if err := h.checkSpec(); err != nil {
		return "", err
	}

	if err := client.mint(ctx); err != nil {
		return "", err
	}

	if err := h.checkChoice(ctx); err != nil {
		return "", err
	}

	data, err := cloudNodeUserData(h.join, omni, h.join.req, h.spec.ExtraEdits)
	if err != nil {
		return "", err
	}

	if data.Size > gcpUserDataLimit {
		return "", fmt.Errorf("the user data is %d bytes, over the 256 KiB Compute Engine takes: remove extra edits, or the config's docs and examples on the template", data.Size)
	}

	if err := h.checkPermissions(ctx); err != nil {
		return "", err
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}

	h.runID = hex.EncodeToString(id)

	if h.requestID, err = gcpRequestID(); err != nil {
		return "", err
	}

	return data.UserData, nil
}

// gcpRequestID is a random UUID (version 4), the insert's requestId.
func gcpRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80

	h := hex.EncodeToString(b)

	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// refused: Google answered no, and made nothing (a 4xx other than a rate limit).
func (h *gcpCreate) refused(err error) bool {
	var e *gcpError

	return errors.As(err, &e) && e.status >= 400 && e.status < 500 && !h.transient(err)
}

type gcpJSON = map[string]any

// insertBody is the instance's insert: the node's config as the user-data metadata value, the
// name the host name Talos reads from the metadata server, the boot disk deleted with it.
func (h *gcpCreate) insertBody(userData string) gcpJSON {
	s := h.spec

	subnet := "regions/" + h.subnetRegion + "/subnetworks/" + h.subnetName
	if h.subnetProject != s.Project {
		subnet = "projects/" + h.subnetProject + "/" + subnet
	}

	nic := gcpJSON{"subnetwork": subnet}
	if s.AssociatePublicIP {
		nic["accessConfigs"] = []gcpJSON{{"type": "ONE_TO_ONE_NAT", "name": "External NAT"}}
	}

	return gcpJSON{
		"name": s.Hostname, "machineType": "zones/" + s.Zone + "/machineTypes/" + s.MachineType,
		"disks": []gcpJSON{{
			"boot": true, "autoDelete": true,
			"initializeParams": gcpJSON{
				"sourceImage": "projects/" + s.Project + "/global/images/" + h.imageName, "diskSizeGb": strconv.Itoa(s.DiskGB),
				"diskType": "zones/" + s.Zone + "/diskTypes/pd-balanced",
			},
		}},
		"networkInterfaces": []gcpJSON{nic},
		"metadata":          gcpJSON{"items": []gcpJSON{{"key": "user-data", "value": userData}}},
		"labels":            map[string]string{gcpLabelRun: h.runID, gcpLabelCluster: hetznerClusterLabel(h.join.context)},
		"tags":              gcpJSON{"items": []string{gcpNetworkTag}},
	}
}

// create sends the one insert, again with the same requestId while it gets no clear answer.
func (h *gcpCreate) create(ctx context.Context, userData string) error {
	line := "creating " + h.spec.Hostname + " (" + h.spec.MachineType + " in " + h.spec.Zone + ")"
	if h.noRoute {
		line += ": no public address and no Cloud NAT on the subnet's network, so the node may not reach the Image Factory or Omni"
	}

	h.join.emit(cloudPhaseCreating, line)

	u := h.client.compute(h.spec.Project, "/zones/"+h.spec.Zone+"/instances") + "?" + url.Values{"requestId": {h.requestID}}.Encode()
	body := h.insertBody(userData)

	var err error

	for try := range gcpInsertTries {
		var op gcpOperation

		if _, err = h.client.do(ctx, http.MethodPost, u, body, &op); err == nil {
			h.created, h.unclear, h.operation, h.insertedAt = true, false, op.Name, time.Now()
			h.join.applied = true

			return nil
		}

		h.unclear = h.unclear || !h.refused(err)

		if !h.transient(err) || try == gcpInsertTries-1 {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}

	return err
}

// waitRunning follows the insert's operation, then the instance until it runs with an address:
// its external one when asked, else its internal one. A provider hiccup is polled through, as is
// an instance Compute Engine does not list yet (for gcpNotFoundGrace): only an instance that
// stops or the run's end stops it.
func (h *gcpCreate) waitRunning(ctx context.Context) error {
	h.join.emit(cloudPhaseStarting, "waiting for instance "+h.spec.Hostname+" to start")

	if err := h.client.waitOperation(ctx, h.spec.Project, h.spec.Zone, h.operation, h.poll); err != nil {
		return err
	}

	last := ""

	for {
		var instance gcpAPIInstance

		err := h.client.get(ctx, h.instanceURL(), &instance)

		switch {
		case gcpNotFound(err) && time.Since(h.insertedAt) < gcpNotFoundGrace:
		case err != nil && !h.transient(err):
			return err
		case err == nil && instance.Status == gcpStatusRunning:
			if h.reachedRunning(instance) {
				return nil
			}
		case err == nil && gcpStopped(instance.Status):
			return h.stopped(ctx, instance)
		case err == nil && instance.Status != last:
			last = instance.Status
			h.join.emit(cloudPhaseStarting, "instance "+h.spec.Hostname+": "+strings.ToLower(last))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

// gcpStopped: the instance went down before it ran.
func gcpStopped(status string) bool {
	switch status {
	case "STOPPING", "STOPPED", "SUSPENDING", "SUSPENDED", "TERMINATED":
		return true
	}

	return false
}

// stopped is the error of an instance that went down, with the end of its serial console when
// it can be read.
func (h *gcpCreate) stopped(ctx context.Context, instance gcpAPIInstance) error {
	msg := fmt.Sprintf("instance %s is %s", h.spec.Hostname, strings.ToLower(instance.Status))
	if instance.StatusMessage != "" {
		msg += ": " + h.client.scrub(instance.StatusMessage)
	}

	var serial struct {
		Contents string `json:"contents"`
	}

	if err := h.client.get(ctx, h.instanceURL()+"/serialPort?port=1", &serial); err == nil && strings.TrimSpace(serial.Contents) != "" {
		tail := serial.Contents
		if len(tail) > gcpSerialTail {
			tail = tail[len(tail)-gcpSerialTail:]
			if i := strings.IndexByte(tail, '\n'); i >= 0 {
				tail = tail[i+1:]
			}
		}

		msg += "; its serial console ends with: " + h.client.scrub(strings.TrimSpace(tail))
	}

	return errors.New(msg)
}

// reachedRunning reads the instance's addresses: false while an external one is asked and not
// given yet.
func (h *gcpCreate) reachedRunning(instance gcpAPIInstance) bool {
	public, private := instance.addresses()

	if h.spec.AssociatePublicIP {
		if public == "" {
			return false
		}

		h.join.node = normalizeEndpoint(public)
		h.join.emit(cloudPhaseStarting, "instance "+h.spec.Hostname+" is running at "+h.join.node)

		return true
	}

	if private == "" {
		return false
	}

	h.join.node = normalizeEndpoint(private)
	h.join.emit(cloudPhaseStarting, "instance "+h.spec.Hostname+" is running with no public address: following it at its private address "+
		h.join.node+", which needs a route from this device (a VPN into the VPC network)")

	return true
}

// transient: worth trying again (rate limit, a server error, no answer).
func (h *gcpCreate) transient(err error) bool { return gcpTransient(err) }

// findUnanswered looks for the instance of an insert with no clear answer: by its name (unique in
// the zone), kept only when its ichor-run label is this run's. A refusal (4xx) created nothing,
// and nothing is looked up.
func (h *gcpCreate) findUnanswered(ctx context.Context, _ error) {
	if h.runID == "" || h.client == nil || !h.unclear {
		return
	}

	for try := range cloudCleanupTries {
		var instance gcpAPIInstance

		err := h.client.get(ctx, h.instanceURL(), &instance)
		if err == nil {
			h.created = instance.Labels[gcpLabelRun] == h.runID

			return
		}

		if (!gcpNotFound(err) && !h.transient(err)) || try == cloudCleanupTries-1 {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(h.poll):
		}
	}
}

// deleteMachine deletes the instance, which takes its boot disk (autoDelete), and follows the
// delete for gcpDeleteWait: an instance already gone is deleted; a delete still running then is
// an error naming the instance, so the run says it may still be billed instead of claiming it is
// gone.
func (h *gcpCreate) deleteMachine(ctx context.Context) error {
	var op gcpOperation

	_, err := h.client.do(ctx, http.MethodDelete, h.instanceURL(), nil, &op)

	switch {
	case gcpNotFound(err):
		return nil
	case err != nil:
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, gcpDeleteWait)
	defer cancel()

	err = h.client.waitOperation(waitCtx, h.spec.Project, h.spec.Zone, op.Name, h.poll)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return &gcpUnfinishedDelete{name: h.spec.Hostname, zone: h.spec.Zone}
	}

	return err
}

// gcpUnfinishedDelete is a delete Google accepted but had not finished in time (gcpDeleteWait,
// or the cleanup's own bound). It unwraps to a deadline so the cleanup does not retry it as a
// lost answer.
type gcpUnfinishedDelete struct{ name, zone string }

func (e *gcpUnfinishedDelete) Error() string {
	return fmt.Sprintf("the delete of instance %s in %s was accepted but had not finished: check in the Google Cloud console that it is gone", e.name, e.zone)
}

func (e *gcpUnfinishedDelete) Unwrap() error     { return context.DeadlineExceeded }
func (e *gcpUnfinishedDelete) unfinishedDelete() {}
