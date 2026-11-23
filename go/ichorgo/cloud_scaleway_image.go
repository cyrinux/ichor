package ichorgo

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
)

// A Talos image of the cluster's schematic at Scaleway, made from the phone. The Talos guide's
// route (download, convert, upload to Object Storage, import a snapshot) needs a laptop, so the
// run drives a throwaway helper Instance instead, as the AWS run does: Ubuntu with an empty Block
// Storage volume, whose cloud-init script downloads the Image Factory's scaleway image, writes it
// to that volume, leaves its verdict in its own user data and powers off. The app then snapshots
// the volume (Block Storage API), creates the image from the snapshot and deletes the helper,
// its volumes and its IP. The phone moves no image bytes. Billable: one small Instance for a few
// minutes, then the snapshot's storage. Every path ends in cleanup.
//
// Every volume of the helper is Block Storage (sbs_volume): every type the marketplace's
// instance_sbs image boots can attach one, an arm64 type has no local storage at all, and the
// image is then made from a Block Storage snapshot (its root volume reads sbs_snapshot). One path
// for every helper type, instead of the create's pickDataVolume choice.

const (
	scwImageTimeout        = 45 * time.Minute
	scwImageStopWait       = 20 * time.Minute
	scwImageSnapshotWait   = 15 * time.Minute
	scwImageCleanupTimeout = 4 * time.Minute
	scwImageAuditAction    = "scaleway-image-create"
	scwImageMinutes        = 10

	// scwImageVolumeGB holds the unpacked scaleway-<arch>.raw (Talos makes it at its minimum raw
	// disk size, under 2 GB).
	scwImageVolumeGB = 10
	// scwImageMinRAMGB: Ubuntu, curl and zstd in less are not worth the risk.
	scwImageMinRAMGB = 2

	scwImageHelperLabel = "ubuntu_noble"
	scwImageHelperKind  = "instance_sbs"
	scwImageNameMax     = 255

	scwSchematicShown = 12
	scwPhaseRegister  = "register"
)

var errScwNoImageRight = errors.New("this API key may not run the helper Instance: attach the printed policy to its application")

// scwImageSpec is StartScalewayImage's specJSON. The image URL is never in it: it comes from the
// template's schematic and version.
type scwImageSpec struct {
	TemplateNode string `json:"templateNode"`
	Arch         string `json:"arch"`
	Zone         string `json:"zone"`
	Type         string `json:"type"`
	Name         string `json:"name"`
}

type scwImageResult struct {
	ImageID      string `json:"imageId"`
	SnapshotID   string `json:"snapshotId"`
	Name         string `json:"name"`
	Arch         string `json:"arch"`
	TalosVersion string `json:"talosVersion"`
	Schematic    string `json:"schematic"`
	SizeGB       int    `json:"sizeGB"`
}

// StartScalewayImage makes a Talos image for the template's version and schematic in the key's
// project and zone: creating → writing → snapshot → register → cleanup → done. specJSON:
// {"templateNode","arch","zone","type","name"}; templateNode "" takes the first member that
// answers, arch "" the template's, zone "" the key's, type "" the cheapest type of the
// architecture with 2 GB of memory that boots the helper image, name ""
// talos-<version>-scaleway-<arch>-<schematic12>. credJSON: as ScalewayTest's. OnDone's first
// argument is the result JSON: {"imageId","snapshotId","name","arch","talosVersion","schematic",
// "sizeGB"}. Refused before any write: a key that may not create, a template whose schematic
// cannot be read, an architecture without a helper image or type in the zone, a name an image of
// the project already has. Cancel, or any failure, deletes the helper with its volumes and IP and
// an unfinished snapshot or image, and the message names what could not be deleted. Nothing of
// the cluster is written. Refused on the demo; recorded in the activity log as
// scaleway-image-create. Billable: the app shows ScalewayImageEstimate first.
func StartScalewayImage(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec scwImageSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)
	if specErr != nil {
		specErr = errors.New("invalid image request")
	}

	contextName, spec.TemplateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(spec.TemplateNode))

	listener = maskedJoinListener{listener}

	r := &scwImageRun{
		config: configYAML, context: contextName, credJSON: credJSON, spec: spec, specErr: specErr,
		listener: listener, poll: scwPollInterval, stopWait: scwImageStopWait, snapshotWait: scwImageSnapshotWait,
	}

	return r.start(scwImageTimeout)
}

// scwImageRun is one run of StartScalewayImage.
type scwImageRun struct {
	config, context string
	credJSON        string
	spec            scwImageSpec
	specErr         error
	listener        JoinListener
	poll            time.Duration
	stopWait        time.Duration
	snapshotWait    time.Duration

	client      *scwClient
	target      bootTarget
	helperImage string
	serverType  string
	runID       string
	name        string
	result      scwImageResult

	// helper is the create's server, IP and delete steps, reused for the throwaway Instance.
	helper *scwCreate
	// What this run created ("" before), and the calls that may have created it without an answer.
	volumeID, snapshotID, imageID string
	snapshotPosted, imagePosted   bool
}

func (r *scwImageRun) start(timeout time.Duration) *JoinRun {
	run := func(ctx context.Context) error { return r.run(ctx, timeout) }
	done := func() (any, string) { return r.result, "image " + r.result.ImageID + " is ready" }

	return startCloudImageRun(r.config, r.context, r.listener, r.auditAction, timeout, run, done, r.emit)
}

func (r *scwImageRun) auditAction() auditAction {
	params := fmt.Sprintf("arch=%s zone=%s version=%s", cmp.Or(r.target.arch, r.spec.Arch), r.spec.Zone, r.target.version)
	if r.imageID != "" {
		params += " image=" + r.imageID
	}

	return auditAction{Action: scwImageAuditAction, Node: r.spec.TemplateNode, Params: params}
}

func (r *scwImageRun) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, r.listener.OnProgress)
}

func (r *scwImageRun) run(ctx context.Context, timeout time.Duration) error {
	if r.specErr != nil {
		return r.specErr
	}

	if isTalosDemoContext(r.config, r.context) {
		return errDemoUnavailable
	}

	if err := r.prepare(ctx); err != nil {
		return err
	}

	err := r.make(ctx)

	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("the image was not ready within %s", timeout)
	case ctx.Err() != nil:
		err = errors.New("stopped")
	}

	return r.cleanup(err)
}

// prepare checks everything that can be refused before a write.
func (r *scwImageRun) prepare(ctx context.Context) error {
	arch, err := normalizeBootArch(r.spec.Arch)
	if err != nil {
		return err
	}

	if r.client, err = newScwClient(r.credJSON); err != nil {
		return err
	}

	if zone := strings.TrimSpace(r.spec.Zone); zone != "" {
		if err := r.client.setZone(zone); err != nil {
			return err
		}
	}

	r.spec.Zone = r.client.zone

	can, checked, err := r.client.canCreate(ctx)
	if err != nil {
		return err
	}

	if checked && !can {
		return errScwNoImageRight
	}

	media, err := cloudImageMedia(r.config, r.context, r.spec.TemplateNode)
	if err != nil {
		return err
	}

	for _, warning := range media.Warnings {
		r.emit(cloudPhaseCreating, warning)
	}

	r.target = media.target()
	r.target.secureBoot = false
	r.target.arch = cmp.Or(arch, r.target.arch)
	r.spec.TemplateNode = media.TemplateNode

	var compatible []string

	if r.helperImage, compatible, err = r.client.helperImage(ctx, r.target.arch); err != nil {
		return err
	}

	if r.serverType, _, err = r.client.helperType(ctx, r.target.arch, strings.TrimSpace(r.spec.Type), compatible); err != nil {
		return err
	}

	if err := r.checkName(ctx); err != nil {
		return err
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	r.runID = hex.EncodeToString(id)

	return nil
}

// scwArch is Scaleway's name of a boot architecture.
func scwArch(arch string) string {
	if arch == bootArchARM64 {
		return "arm64"
	}

	return "x86_64"
}

// helperImage is the marketplace's Ubuntu image for Block Storage of the architecture in the
// client's zone, and the types it boots. No id is hard-coded: the marketplace's ids differ per
// zone and per release.
func (c *scwClient) helperImage(ctx context.Context, arch string) (string, []string, error) {
	query := url.Values{
		"image_label": {scwImageHelperLabel}, "zone": {c.zone}, "type": {scwImageHelperKind},
		"arch": {scwArch(arch)}, "page_size": {"100"},
	}

	var resp struct {
		LocalImages []struct {
			ID         string   `json:"id"`
			Arch       string   `json:"arch"`
			Zone       string   `json:"zone"`
			Compatible []string `json:"compatible_commercial_types"`
		} `json:"local_images"`
	}

	if err := c.do(ctx, http.MethodGet, "/marketplace/v2/local-images?"+query.Encode(), nil, &resp); err != nil {
		return "", nil, err
	}

	for _, i := range resp.LocalImages {
		if i.Arch == scwArch(arch) && (i.Zone == "" || i.Zone == c.zone) && i.ID != "" {
			return i.ID, i.Compatible, nil
		}
	}

	return "", nil, fmt.Errorf("no Ubuntu image for %s in %s to write the image with: pick another zone", arch, c.zone)
}

// helperType is name when it fits the architecture and the helper image, else the cheapest type
// of the architecture with scwImageMinRAMGB of memory that boots the helper image; with its
// hourly price.
func (c *scwClient) helperType(ctx context.Context, arch, name string, compatible []string) (string, string, error) {
	types, err := c.serverTypes(ctx)
	if err != nil {
		return "", "", err
	}

	want := scwArch(arch)

	for _, t := range scwTypes(types) {
		fits := t.Arch == want && slices.Contains(compatible, t.Name)

		switch {
		case name != "" && t.Name == name && !fits:
			return "", "", fmt.Errorf("%s cannot run the %s helper in %s: pick a %s type", name, want, c.zone, want)
		case name != "" && t.Name == name:
			return t.Name, t.Hourly, nil
		case name == "" && fits && t.RAMGB >= scwImageMinRAMGB:
			return t.Name, t.Hourly, nil
		}
	}

	if name != "" {
		return "", "", fmt.Errorf("%s cannot be ordered in %s: pick another type", name, c.zone)
	}

	return "", "", fmt.Errorf("no %s type in %s can run the helper: pick another zone", want, c.zone)
}

// checkName refuses a name the project's images already have, or one that is not a name. A
// second image of the same version and schematic needs a name of its own.
func (r *scwImageRun) checkName(ctx context.Context) error {
	schematic := r.target.schematic
	r.name = strings.TrimSpace(r.spec.Name)

	if r.name == "" {
		r.name = fmt.Sprintf("talos-%s-%s-%s-%s", r.target.version, cloudPlatformScaleway, r.target.arch, schematic[:min(scwSchematicShown, len(schematic))])
	}

	if len(r.name) > scwImageNameMax || strings.ContainsFunc(r.name, unicode.IsControl) {
		return fmt.Errorf("%q is not an image name (at most %d characters)", r.name, scwImageNameMax)
	}

	var taken *scwAPIImage

	err := scwEachPage(ctx, r.client, r.client.path("/images?"+r.client.projectQuery()+"&name="+url.QueryEscape(r.name)), func(resp struct {
		Images []scwAPIImage `json:"images"`
	},
	) int {
		for _, i := range resp.Images {
			if i.Name == r.name && taken == nil {
				taken = &i
			}
		}

		return len(resp.Images)
	})

	switch {
	case err != nil:
		return err
	case taken != nil && strings.TrimSpace(r.spec.Name) == "":
		return fmt.Errorf("an image for this version and schematic exists: %s. Pick it, or give the new one a name", taken.ID)
	case taken != nil:
		return fmt.Errorf("image %s is already named %q: pick another name", taken.ID, r.name)
	}

	return nil
}

// make runs the steps up to the ready image; cleanup follows, whatever it returns.
func (r *scwImageRun) make(ctx context.Context) error {
	helperName := "ichor-image-" + r.runID

	r.emit(cloudPhaseCreating, "creating the throwaway Instance "+helperName+" ("+r.serverType+" in "+r.spec.Zone+")")

	r.helper = &scwCreate{
		spec:   scwCreateSpec{Zone: r.spec.Zone, Type: r.serverType, ImageID: r.helperImage, Hostname: helperName},
		client: r.client, poll: r.poll, runID: r.runID,
		dataType: scwVolumeBlock, dataSize: scwImageVolumeGB * scwGB, dataName: helperName + "-data",
	}

	tags := []string{scwTagRun + r.runID, "ichor=image"}

	if err := r.helper.createIP(ctx, tags); err != nil {
		return err
	}

	if err := r.helper.createServer(ctx, tags); err != nil {
		return err
	}

	imageURL := cloudImageURL(r.target, cloudPlatformScaleway)

	script := strings.NewReader(scwImageScript(imageURL))
	if err := r.client.send(ctx, http.MethodPatch, r.helper.serverPath("/user_data/cloud-init"), "text/plain", script, nil); err != nil {
		return err
	}

	if err := r.client.do(ctx, http.MethodPost, r.helper.serverPath("/action"), map[string]string{"action": "poweron"}, nil); err != nil {
		return err
	}

	r.emit(hetznerPhaseWriting, "Instance "+r.helper.serverID+" writes "+imageURL)

	if err := r.waitWritten(ctx); err != nil {
		return err
	}

	if err := r.powerOff(ctx); err != nil {
		return err
	}

	r.emit(hetznerPhaseSnapshot, "taking the snapshot of volume "+r.volumeID)

	if err := r.createSnapshot(ctx); err != nil {
		return err
	}

	r.emit(scwPhaseRegister, "creating the image "+r.name)

	return r.register(ctx)
}

// ScalewayImageEstimate is the cost line of an image run: the helper's type and hourly price
// before tax (it runs about ten minutes) and the snapshot's size. Scaleway's API gives no snapshot
// price: note says so. Creates nothing. JSON: {"type","hourly","minutes","currency","snapshotGB",
// "note"}. zone "" for the key's.
func ScalewayImageEstimate(credJSON, arch, zone string) (out string, err error) {
	defer maskResult(&out, &err)

	arch, err = normalizeBootArch(arch)
	if err != nil {
		return "", err
	}

	if arch == "" {
		return "", errors.New("pick an architecture (amd64, arm64)")
	}

	c, err := newScwClient(credJSON)
	if err != nil {
		return "", err
	}

	if zone = strings.TrimSpace(zone); zone != "" {
		if err := c.setZone(zone); err != nil {
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), scwOptionsTimeout)
	defer cancel()

	_, compatible, err := c.helperImage(ctx, arch)
	if err != nil {
		return "", err
	}

	name, hourly, err := c.helperType(ctx, arch, "", compatible)
	if err != nil {
		return "", err
	}

	return toJSON(scwImageCost{
		Type: name, Hourly: hourly, Minutes: scwImageMinutes, Currency: "EUR", SnapshotGB: scwImageVolumeGB,
		Note: fmt.Sprintf("Scaleway's API gives no snapshot price: the %d GB Block Storage snapshot is billed per GB-month until it is deleted", scwImageVolumeGB),
	})
}

type scwImageCost struct {
	Type       string `json:"type"`
	Hourly     string `json:"hourly"`
	Minutes    int    `json:"minutes"`
	Currency   string `json:"currency"`
	SnapshotGB int    `json:"snapshotGB"`
	Note       string `json:"note"`
}
