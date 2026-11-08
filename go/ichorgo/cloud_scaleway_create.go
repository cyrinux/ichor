package ichorgo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Creating a Scaleway Instance for a new node: the provider's half of cloudCreateRun. Talos's
// scaleway platform reads its config from the metadata service's user_data/cloud-init, and the
// 4.5 GB image leaves no room for EPHEMERAL: the Instance gets a second volume, and (plain Talos)
// a VolumeConfig moving EPHEMERAL there, as the Talos guide does by hand.

const (
	// scwUserDataLimit: Scaleway documents none; 32 KiB, as Hetzner's, until a live run says more.
	scwUserDataLimit = 32 << 10

	scwPollInterval      = 5 * time.Second
	scwDefaultEphemeral  = 25
	scwMinEphemeral      = 2
	scwMaxEphemeral      = 1000
	scwEphemeralMaxGuide = 40 // the guide's maxSize, GB

	scwStateRunning      = "running"
	scwStateStopped      = "stopped"
	scwStateStoppedPlace = "stopped in place"
	scwStateLocked       = "locked"

	scwTagRun = "ichor-run="
)

// scwCreateSpec is StartScalewayCreate's specJSON.
type scwCreateSpec struct {
	Zone            string `json:"zone"`
	Type            string `json:"type"`
	ImageID         string `json:"imageId"`
	SecurityGroupID string `json:"securityGroupId"`
	Hostname        string `json:"hostname"`
	Role            string `json:"role"`
	TemplateNode    string `json:"templateNode"`
	InstallDisk     string `json:"installDisk"`
	ExtraEdits      string `json:"extraEditsJSON"`
	LabelsJSON      string `json:"labelsJson"`
	EphemeralGB     int    `json:"ephemeralGB"`
}

// scwServer is the part of a server the run reads.
type scwServer struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	PublicIPs []struct {
		Address string `json:"address"`
		Family  string `json:"family"`
	} `json:"public_ips"`
	Volumes map[string]struct {
		ID         string `json:"id"`
		VolumeType string `json:"volume_type"`
	} `json:"volumes"`
}

// address is the server's public IPv4.
func (s scwServer) address() string {
	for _, ip := range s.PublicIPs {
		if ip.Family == "inet" && ip.Address != "" {
			return ip.Address
		}
	}

	return ""
}

// scwVolume is a volume of the run's server, deleted with it or after it.
type scwVolume struct {
	id, kind string
	gone     bool
}

// StartScalewayCreate creates a Scaleway Instance from a Talos image of the project with the new
// node's config as its cloud-init user data and a second volume for EPHEMERAL, powers it on and
// follows it: creating → starting → (plain Talos) rebooting → booted → joining → registering →
// done, or (Omni) omni → done. specJSON: {"zone","type","imageId","securityGroupId" (optional,
// the project's default otherwise),"hostname","role","templateNode","installDisk" (/dev/vda, the
// image's disk),"extraEditsJSON","labelsJson","ephemeralGB" (0 for 25)}. credJSON: as
// ScalewayTest's; the spec's zone wins. A type of another architecture than the image, an image of
// another project, a key that may not create and user data over 32 KiB are refused before any
// write. The data volume is local storage when the type has room, else Block Storage. On Omni the
// VolumeConfig is not sent (Omni owns the config): the run says to add it as a patch. OnDone gets
// the node's address to add to the context ("" on Omni). A failed or cancelled run deletes what it
// created: the server with its volumes, then its flexible IP (see cloudCreateRun for the
// exception). Refused on the demo and in privacy mode; recorded in the activity log as
// scaleway-instance-create, never with the config or the key. Billable: the app confirms the cost
// and the hostname first.
func StartScalewayCreate(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec scwCreateSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)

	template := strings.TrimSpace(spec.TemplateNode)
	contextName, template = unmaskTarget(configYAML, contextName, template)
	spec.TemplateNode = template

	listener = maskedJoinListener{listener}

	if specErr != nil {
		specErr = errors.New("invalid Instance request")
	}

	req, specErr := cloudJoinRequest(spec.TemplateNode, spec.Role, spec.Hostname, spec.InstallDisk, spec.LabelsJSON, specErr)

	h := &scwCreate{
		join: &nodeJoin{
			config: configYAML, context: contextName, listener: listener, interval: upgradePollInterval,
			req:       req,
			editsJSON: spec.ExtraEdits,
		},
		spec: spec, specErr: specErr, credJSON: credJSON, poll: scwPollInterval,
	}

	return h.start(cloudCreateTimeout)
}

// scwCreate is one run of StartScalewayCreate.
type scwCreate struct {
	join     *nodeJoin
	spec     scwCreateSpec
	specErr  error
	credJSON string
	poll     time.Duration
	client   *scwClient
	omni     bool

	dataType string // the EPHEMERAL volume's type
	dataSize int64  // its size, bytes
	runID    string

	ipID, ipAddress string
	ipUnclear       bool
	ipGone          bool

	serverID      string
	serverUnclear bool
	serverGone    bool
	// keptVolumes: the server went with DELETE (it never ran), which leaves its volumes.
	keptVolumes bool
	volumes     []*scwVolume
}

func (h *scwCreate) start(timeout time.Duration) *JoinRun {
	run := &cloudCreateRun{provider: h, join: h.join, specErr: h.specErr, poll: h.poll, omni: &h.omni}

	return run.start(timeout)
}

func (h *scwCreate) label() string { return "Scaleway" }

func (h *scwCreate) machineWord() string {
	if h.serverID == "" && h.ipID != "" {
		return "flexible IP"
	}

	return "instance"
}

func (h *scwCreate) machineID() string {
	if h.serverID != "" {
		return h.serverID
	}

	return h.ipID
}

func (h *scwCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s type=%s zone=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.Type, h.spec.Zone)
	if h.serverID != "" {
		params += " instance=" + h.serverID
	}

	return auditAction{Action: "scaleway-instance-create", Node: h.join.node, Params: params}
}

func (h *scwCreate) prepare(ctx context.Context, omni bool) (string, error) {
	h.spec.Hostname = strings.TrimSpace(h.spec.Hostname)
	h.join.req.hostname = h.spec.Hostname

	if !validHostname(h.spec.Hostname) || strings.Contains(h.spec.Hostname, ".") {
		return "", fmt.Errorf("%q is not a host name (letters, digits and dashes)", h.spec.Hostname)
	}

	if h.spec.Zone == "" || h.spec.Type == "" || h.spec.ImageID == "" {
		return "", errors.New("pick a zone, a server type and an image")
	}

	if h.spec.EphemeralGB == 0 {
		h.spec.EphemeralGB = scwDefaultEphemeral
	}

	if h.spec.EphemeralGB < scwMinEphemeral || h.spec.EphemeralGB > scwMaxEphemeral {
		return "", fmt.Errorf("the EPHEMERAL volume takes %d to %d GB", scwMinEphemeral, scwMaxEphemeral)
	}

	client, err := newScwClient(h.credJSON)
	if err != nil {
		return "", err
	}

	if err := client.setZone(h.spec.Zone); err != nil {
		return "", err
	}

	h.client = client

	can, checked, err := client.canCreate(ctx)
	if err != nil {
		return "", err
	}

	if checked && !can {
		return "", errScwNoCreate
	}

	match, err := h.checkChoice(ctx)
	if err != nil {
		return "", err
	}

	edits := h.spec.ExtraEdits
	if !omni {
		if edits, err = scwEphemeralEdits(edits, match, h.spec.EphemeralGB); err != nil {
			return "", err
		}
	}

	data, err := cloudNodeUserData(h.join, omni, h.join.req, edits)
	if err != nil {
		return "", err
	}

	if data.Size > scwUserDataLimit {
		return "", fmt.Errorf("the user data is %d bytes, over the 32 KiB taken here: remove extra edits, or the config's docs and examples on the template", data.Size)
	}

	return data.UserData, nil
}

// checkChoice refuses a type the zone does not sell, an image of another project or of another
// architecture, and picks the EPHEMERAL volume's type: local when the type has room for it beside
// the image's root, Block Storage otherwise. It returns the volume's disk selector.
func (h *scwCreate) checkChoice(ctx context.Context) (string, error) {
	types, err := h.client.serverTypes(ctx)
	if err != nil {
		return "", err
	}

	t, ok := types[h.spec.Type]
	if !ok || t.EndOfService {
		return "", fmt.Errorf("%s cannot be ordered in %s: pick another type", h.spec.Type, h.spec.Zone)
	}

	var image struct {
		Image scwAPIImage `json:"image"`
	}

	if err := h.client.do(ctx, http.MethodGet, h.client.path("/images/"+url.PathEscape(h.spec.ImageID)), nil, &image); err != nil {
		if scwStatus(err) == http.StatusNotFound {
			return "", fmt.Errorf("this image is not in %s: pick another", h.spec.Zone)
		}

		return "", err
	}

	if image.Image.Project != "" && image.Image.Project != h.client.project {
		return "", errors.New("this image belongs to another project: pick one of the key's project")
	}

	if image.Image.Arch != t.Arch {
		return "", fmt.Errorf("the image is for %s and %s is %s: pick a matching type or image", image.Image.Arch, h.spec.Type, t.Arch)
	}

	return h.pickDataVolume(t, image.Image), nil
}

// pickDataVolume sets the data volume's type and size and returns its disk selector. A local one
// fills the type's minimum local storage; the guide's /dev/vdb holds only when both volumes are
// local, so any other layout selects the disk Talos does not boot from.
func (h *scwCreate) pickDataVolume(t scwAPIType, image scwAPIImage) string {
	size := int64(h.spec.EphemeralGB) * scwGB

	rootLocal := int64(0)
	if image.RootVolume.VolumeType == scwVolumeLocal {
		rootLocal = image.RootVolume.Size
	}

	limits := t.VolumesConstraint

	if limits.MaxSize == 0 || rootLocal+size > limits.MaxSize {
		h.dataType, h.dataSize = scwVolumeBlock, size

		return "!system_disk"
	}

	h.dataType, h.dataSize = scwVolumeLocal, max(size, limits.MinSize-rootLocal)

	if rootLocal > 0 {
		return "disk.dev_path == '/dev/vdb'"
	}

	return "!system_disk"
}

// scwEphemeralEdits puts the VolumeConfig moving EPHEMERAL to the data volume first in the
// person's edits, so one of theirs still wins; it replaces a template's own.
func scwEphemeralEdits(editsJSON, match string, sizeGB int) (string, error) {
	var edits []configEdit

	if strings.TrimSpace(editsJSON) != "" {
		if err := json.Unmarshal([]byte(editsJSON), &edits); err != nil {
			return "", fmt.Errorf("invalid edits: %w", err)
		}
	}

	doc := fmt.Sprintf(`apiVersion: v1alpha1
kind: VolumeConfig
name: EPHEMERAL
provisioning:
  diskSelector:
    match: %s
  minSize: 2GB
  maxSize: %dGB
  grow: false
`, match, max(sizeGB, scwEphemeralMaxGuide))

	ephemeral := configEdit{Doc: -1, Path: []string{}, Op: configOpAddDoc, Type: configTypeYAML, Value: doc}

	out, err := json.Marshal(append([]configEdit{ephemeral}, edits...))

	return string(out), err
}

// create makes the flexible IP, then the server (created stopped), writes the user data and
// powers it on. Both carry the run's tag: one created without an answer is found by it.
func (h *scwCreate) create(ctx context.Context, userData string) error {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	h.runID = hex.EncodeToString(id)
	tags := []string{scwTagRun + h.runID, "ichor=node"}

	h.join.emit(cloudPhaseCreating, "creating "+h.spec.Hostname+" ("+h.spec.Type+" in "+h.spec.Zone+")")

	if h.omni {
		h.join.emit(cloudPhaseCreating, "on Omni, add the Talos guide's EPHEMERAL VolumeConfig as a config patch for "+h.spec.Hostname+": its second volume is attached")
	}

	if err := h.createIP(ctx, tags); err != nil {
		return err
	}

	if err := h.createServer(ctx, tags); err != nil {
		return err
	}

	h.join.applied = true

	err := h.client.send(ctx, http.MethodPatch, h.serverPath("/user_data/cloud-init"), "text/plain", strings.NewReader(userData), nil)
	if err != nil {
		return err
	}

	return h.client.do(ctx, http.MethodPost, h.serverPath("/action"), map[string]string{"action": "poweron"}, nil)
}

func (h *scwCreate) createIP(ctx context.Context, tags []string) error {
	var resp struct {
		IP struct {
			ID      string `json:"id"`
			Address string `json:"address"`
		} `json:"ip"`
	}

	body := map[string]any{"project": h.client.project, "type": "routed_ipv4", "tags": tags}

	if err := h.client.do(ctx, http.MethodPost, h.client.path("/ips"), body, &resp); err != nil {
		h.ipUnclear = !h.refused(err)

		return err
	}

	h.ipID, h.ipAddress = resp.IP.ID, resp.IP.Address

	return nil
}

func (h *scwCreate) createServer(ctx context.Context, tags []string) error {
	body := map[string]any{
		"name": h.spec.Hostname, "commercial_type": h.spec.Type, "image": h.spec.ImageID, "project": h.client.project,
		"tags": tags, "routed_ip_enabled": true, "public_ips": []string{h.ipID}, "boot_type": "local",
		"volumes": map[string]any{
			"1": map[string]any{"name": h.spec.Hostname + "-ephemeral", "size": h.dataSize, "volume_type": h.dataType},
		},
	}

	if h.spec.SecurityGroupID != "" {
		body["security_group"] = h.spec.SecurityGroupID
	}

	var resp struct {
		Server scwServer `json:"server"`
	}

	if err := h.client.do(ctx, http.MethodPost, h.client.path("/servers"), body, &resp); err != nil {
		if h.refused(err) {
			return err
		}

		h.serverUnclear = true
		h.lookup(ctx)

		return fmt.Errorf("%w. An Instance named %s may exist without an answer: it is looked for by the run's tag", err, h.spec.Hostname)
	}

	if resp.Server.ID == "" {
		h.serverUnclear = true

		return fmt.Errorf("scaleway answered without an Instance id: check the project for %s", h.spec.Hostname)
	}

	h.setServer(resp.Server)

	return nil
}

func (h *scwCreate) setServer(s scwServer) {
	h.serverID = s.ID

	for _, key := range slices.Sorted(maps.Keys(s.Volumes)) {
		h.volumes = append(h.volumes, &scwVolume{id: s.Volumes[key].ID, kind: s.Volumes[key].VolumeType})
	}
}

func (h *scwCreate) serverPath(rest string) string {
	return h.client.path("/servers/" + url.PathEscape(h.serverID) + rest)
}

// refused: a 4xx other than a rate limit, which created nothing.
func (h *scwCreate) refused(err error) bool {
	status := scwStatus(err)

	return status >= http.StatusBadRequest && status < http.StatusInternalServerError && status != http.StatusTooManyRequests
}

// waitRunning follows the server until it runs with a public address. A provider hiccup is
// polled through: only a locked server or the run's end stops it.
func (h *scwCreate) waitRunning(ctx context.Context) error {
	h.join.emit(cloudPhaseStarting, "waiting for Instance "+h.serverID+" to start")

	last := ""

	for {
		var resp struct {
			Server scwServer `json:"server"`
		}

		err := h.client.do(ctx, http.MethodGet, h.serverPath(""), nil, &resp)
		s := resp.Server

		switch {
		case err != nil && !h.transient(err):
			return err
		case err == nil && s.State == scwStateLocked:
			return errors.New("scaleway locked the Instance: see the console")
		case err == nil && s.State == scwStateRunning && (s.address() != "" || h.ipAddress != ""):
			address := s.address()
			if address == "" {
				address = h.ipAddress
			}

			h.join.node = normalizeEndpoint(address)
			h.join.emit(cloudPhaseStarting, "Instance "+h.serverID+" is running at "+h.join.node)

			return nil
		case err == nil && s.State != last:
			last = s.State
			h.join.emit(cloudPhaseStarting, "Instance "+h.serverID+": "+s.State)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

// transient: worth polling again (rate limit, a server error, no answer).
func (h *scwCreate) transient(err error) bool {
	if status := scwStatus(err); status != 0 {
		return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (h *scwCreate) findUnanswered(ctx context.Context, _ error) {
	h.lookup(ctx)
}

// lookup finds, by the run's tag, the IP and the server of a create with no clear answer.
func (h *scwCreate) lookup(ctx context.Context) {
	if h.runID == "" || h.client == nil {
		return
	}

	tag := "tags=" + url.QueryEscape(scwTagRun+h.runID) + "&" + h.client.projectQuery()

	if h.ipUnclear && h.ipID == "" {
		var resp struct {
			IPs []struct {
				ID      string `json:"id"`
				Address string `json:"address"`
			} `json:"ips"`
		}

		if err := h.client.do(ctx, http.MethodGet, h.client.path("/ips?"+tag), nil, &resp); err == nil && len(resp.IPs) == 1 {
			h.ipID, h.ipAddress, h.ipUnclear = resp.IPs[0].ID, resp.IPs[0].Address, false
		}
	}

	if h.serverUnclear && h.serverID == "" {
		var resp struct {
			Servers []scwServer `json:"servers"`
		}

		if err := h.client.do(ctx, http.MethodGet, h.client.path("/servers?"+tag), nil, &resp); err == nil && len(resp.Servers) == 1 {
			h.setServer(resp.Servers[0])
			h.serverUnclear = false
		}
	}
}
