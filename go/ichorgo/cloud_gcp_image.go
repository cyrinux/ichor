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
	"strings"
	"time"
)

// S8 step 24: a Talos image for the cluster's schematic at Google Cloud, made from the phone.
// There is no official Talos image on GCP, and the documented route (a bucket upload, then
// gcloud compute images create) needs a laptop; a Storage Transfer from the Image Factory cannot
// work either (the Factory serves no Range request). So the run drives a throwaway helper VM, as
// at AWS and Azure: Debian with an empty data disk, whose startup script downloads the Factory's
// gcp-<arch>.raw.tar.gz, writes its disk.raw to that disk and powers off. The app then makes an
// image from the data disk and deletes the helper (its boot disk goes with it) and the data disk.
// The phone moves no image bytes. Billable: one small VM for a few minutes, then the image's
// storage. Every path ends in cleanup.

const (
	gcpImageTimeout = time.Hour
	gcpImageWrite   = 20 * time.Minute
	gcpImageWait    = 15 * time.Minute
	// gcpImageCleanupTimeout covers the deletes of the image, the helper and the disk, each up to
	// gcpDeleteWait, and the disk's retries.
	gcpImageCleanupTimeout = 18 * time.Minute
	gcpImageCleanupTries   = 5
	gcpImageConsoleTries   = 3
	gcpImageAudit          = "gcp-image-create"
	gcpImageMinutes        = 10

	// gcpImageDiskGB holds the unpacked disk.raw: 4 GiB for amd64 v1.12.0, but 10.9 GiB for
	// v1.11.5, so 12. gcpImageBootGB is the Debian image's own size.
	gcpImageDiskGB = 12
	gcpImageBootGB = 10
	// gcpImageDevice is the data disk's deviceName: the helper finds it at
	// /dev/disk/by-id/google-<deviceName>, never at a guessed /dev/sdb.
	gcpImageDevice = "talos"

	gcpImageTypeAMD64 = "e2-small"
	gcpImageTypeARM64 = "t2a-standard-1"

	// gcpSchematicLabel is how much of the schematic fits the label the app reads back (the full
	// id is in the image's description).
	gcpSchematicLabel = 16
	gcpStatusStopped  = "TERMINATED"

	gcpFeatureMultiqueue = "VIRTIO_SCSI_MULTIQUEUE"
	gcpFeatureGVNIC      = "GVNIC"
)

// gcpImageSpec is StartGCPImage's specJSON. The image URL is never in it: it comes from the
// template's schematic and version.
type gcpImageSpec struct {
	TemplateNode      string `json:"templateNode"`
	Arch              string `json:"arch"`
	Project           string `json:"project"`
	Zone              string `json:"zone"`
	Subnetwork        string `json:"subnetwork"`
	AssociatePublicIP bool   `json:"associatePublicIp"`
	MachineType       string `json:"machineType"`
	Name              string `json:"name"`
}

type gcpImageResult struct {
	Image        string `json:"image"`
	SelfLink     string `json:"selfLink"`
	Arch         string `json:"arch"`
	TalosVersion string `json:"talosVersion"`
	Schematic    string `json:"schematic"`
	SizeGB       int    `json:"sizeGB"`
}

// StartGCPImage makes a Talos image for the template's version and schematic in a project:
// creating → writing → register → cleanup → done. specJSON: {"templateNode","arch","project",
// "zone","subnetwork","associatePublicIp","machineType","name"}; templateNode "" takes the first
// member that answers, arch "" the template's, project "" the credential's, zone "" the
// credential's, machineType "" e2-small (amd64) or t2a-standard-1 (arm64), name ""
// talos-<version>-gcp-<arch>-<schematic12>; subnetwork a link, or a name of the zone's region. The
// helper must reach the Image Factory: associatePublicIp, or Cloud NAT on the subnet's network.
// credJSON: as GCPTest's. OnDone's first argument is the result JSON, not an address: {"image",
// "selfLink","arch","talosVersion","schematic","sizeGB"}. Refused before any write: a template
// whose schematic cannot be read, a machine type the zone lacks or of another architecture, a
// subnet of another region or with no route out, an image of that name in the project, then a
// key whose testIamPermissions lacks a permission the run uses. Cancel, or any failure, deletes
// the helper, its data disk and an unfinished image, and the message names what could not be.
// Nothing of the cluster is written. Refused on the demo; recorded in the activity log as
// gcp-image-create. Billable: the app shows GCPImageEstimate first.
func StartGCPImage(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec gcpImageSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)
	if specErr != nil {
		specErr = errors.New("invalid image request")
	}

	contextName, spec.TemplateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(spec.TemplateNode))

	listener = maskedJoinListener{listener}

	r := &gcpImageRun{
		config: configYAML, context: contextName, credJSON: credJSON, spec: spec, specErr: specErr,
		listener: listener, poll: gcpPollInterval, writeWait: gcpImageWrite, imageWait: gcpImageWait,
	}

	return r.start(gcpImageTimeout)
}

// gcpImageRun is one run of StartGCPImage.
type gcpImageRun struct {
	config, context string
	credJSON        string
	spec            gcpImageSpec
	specErr         error
	listener        JoinListener
	poll            time.Duration
	writeWait       time.Duration
	imageWait       time.Duration

	client                                  *gcpClient
	target                                  bootTarget
	helperImage                             string // the Debian image's link
	subnetProject, subnetRegion, subnetName string
	runID, name                             string
	result                                  gcpImageResult

	// The inserts this run sent (an insert without a clear answer may have made its resource).
	helperPosted, imagePosted bool
}

func (r *gcpImageRun) start(timeout time.Duration) *JoinRun {
	run := func(ctx context.Context) error { return r.run(ctx, timeout) }
	done := func() (any, string) { return r.result, "image " + r.result.Image + " is ready" }

	return startCloudImageRun(r.config, r.context, r.listener, r.auditAction, timeout, run, done, r.emit)
}

func (r *gcpImageRun) auditAction() auditAction {
	params := fmt.Sprintf("arch=%s zone=%s version=%s", cmp.Or(r.target.arch, r.spec.Arch), r.spec.Zone, r.target.version)
	if r.result.Image != "" {
		params += " image=" + r.result.Image
	}

	return auditAction{Action: gcpImageAudit, Node: r.spec.TemplateNode, Params: params}
}

func (r *gcpImageRun) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, r.listener.OnProgress)
}

func (r *gcpImageRun) run(ctx context.Context, timeout time.Duration) error {
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

func (r *gcpImageRun) helper() string { return "ichor-image-" + r.runID }

func (r *gcpImageRun) dataDisk() string { return r.helper() + "-data" }

func (r *gcpImageRun) instanceURL() string {
	return r.client.compute(r.spec.Project, "/zones/"+r.spec.Zone+"/instances/"+r.helper())
}

func (r *gcpImageRun) diskURL() string {
	return r.client.compute(r.spec.Project, "/zones/"+r.spec.Zone+"/disks/"+r.dataDisk())
}

func (r *gcpImageRun) imageURL() string {
	return r.client.compute(r.spec.Project, "/global/images/"+r.name)
}

// checkSpec refuses a request that is incomplete or whose names would not fit in a path.
func (r *gcpImageRun) checkSpec() error {
	s := &r.spec
	s.Project = cmp.Or(strings.TrimSpace(s.Project), r.client.project)
	s.Zone = strings.ToLower(cmp.Or(strings.TrimSpace(s.Zone), r.client.zone))
	s.MachineType, s.Subnetwork = strings.ToLower(strings.TrimSpace(s.MachineType)), strings.TrimSpace(s.Subnetwork)

	switch {
	case s.Zone == "" || s.Subnetwork == "":
		return errors.New("pick a zone and a subnet that reaches the internet")
	case !gcpProjectPattern.MatchString(s.Project):
		return fmt.Errorf("%q is not a project ID", s.Project)
	case !gcpZonePattern.MatchString(s.Zone):
		return fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", s.Zone)
	case s.MachineType != "" && !gcpMachineTypePattern.MatchString(s.MachineType):
		return fmt.Errorf("%q is not a machine type (e2-small)", s.MachineType)
	}

	var err error

	r.subnetProject, r.subnetRegion, r.subnetName, err = gcpParseSubnet(s.Subnetwork, s.Project, s.Zone)

	return err
}

// prepare checks everything that can be refused before a write, the key's permissions last.
func (r *gcpImageRun) prepare(ctx context.Context) error {
	arch, err := normalizeBootArch(r.spec.Arch)
	if err != nil {
		return err
	}

	if r.client, err = newGCPClient(r.credJSON); err != nil {
		return err
	}

	if err := r.checkSpec(); err != nil {
		return err
	}

	if err := r.client.readProject(ctx); err != nil {
		return err
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
	r.spec.MachineType = cmp.Or(r.spec.MachineType, gcpImageType(r.target.arch))

	if err := r.checkChoice(ctx); err != nil {
		return err
	}

	if err := r.checkName(ctx); err != nil {
		return err
	}

	wanted := append(gcpCreatePermissions(r.spec.AssociatePublicIP), gcpImagePermissions()...)
	if err := gcpCheckRun(ctx, r.client, r.spec.Project, r.subnetProject, wanted, r.spec.AssociatePublicIP); err != nil {
		return err
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	r.runID = hex.EncodeToString(id)

	return nil
}

// gcpImageType is the helper's default machine type: a small one of the architecture.
func gcpImageType(arch string) string {
	if arch == bootArchARM64 {
		return gcpImageTypeARM64
	}

	return gcpImageTypeAMD64
}

// checkChoice refuses a machine type the zone lacks or of another architecture, a subnet of
// another region or with no route to the Image Factory, and reads the Debian helper image.
func (r *gcpImageRun) checkChoice(ctx context.Context) error {
	c, s := r.client, r.spec

	var machineType gcpAPIMachineType
	if err := c.get(ctx, c.compute(s.Project, "/zones/"+s.Zone+"/machineTypes/"+s.MachineType), &machineType); err != nil {
		if gcpNotFound(err) {
			return fmt.Errorf("no machine type %s in %s: pick another zone or machine type", s.MachineType, s.Zone)
		}

		return err
	}

	switch {
	case !machineType.usable():
		return fmt.Errorf("machine type %s is %s: pick another", s.MachineType, strings.ToLower(machineType.Deprecated.State))
	case machineType.arch() != r.target.arch:
		return fmt.Errorf("machine type %s is %s, the image %s: pick another", s.MachineType, machineType.arch(), r.target.arch)
	}

	noRoute, err := gcpCheckSubnet(ctx, c, r.subnetProject, r.subnetRegion, r.subnetName, s.Zone, s.AssociatePublicIP)
	if err != nil {
		return err
	}

	if noRoute {
		return fmt.Errorf("subnet %s has no Cloud NAT and the helper no public address, so it cannot reach the Image Factory: ask for a public address", r.subnetName)
	}

	family := "debian-12"
	if r.target.arch == bootArchARM64 {
		family = "debian-12-arm64"
	}

	var image gcpAPIImage
	if err := c.get(ctx, c.compute("debian-cloud", "/global/images/family/"+family), &image); err != nil {
		return fmt.Errorf("the Debian image of the helper (%s) cannot be read: %w", family, err)
	}

	r.helperImage = cmp.Or(image.SelfLink, "projects/debian-cloud/global/images/"+image.Name)

	return nil
}

// checkName refuses a name Compute Engine would, or one the project holds: without a name, the
// image of this version and schematic exists already, and a second one needs a name of its own.
func (r *gcpImageRun) checkName(ctx context.Context) error {
	schematic := r.target.schematic
	r.name = strings.TrimSpace(r.spec.Name)

	if r.name == "" {
		r.name = "talos-" + gcpLabelOfVersion(r.target.version) + "-gcp-" + r.target.arch + "-" + schematic[:min(awsSchematicShown, len(schematic))]
	}

	if !gcpNamePattern.MatchString(r.name) {
		return fmt.Errorf("%q is not an image name (lowercase letters, digits and dashes, starting with a letter, at most 63)", r.name)
	}

	err := r.client.get(ctx, r.imageURL(), nil)

	switch {
	case err == nil && r.spec.Name == "":
		return fmt.Errorf("an image for Talos %s and this schematic exists: %s. Pick it, or give the new one a name", r.target.version, r.name)
	case err == nil:
		return fmt.Errorf("an image named %s exists in project %s: pick another name", r.name, r.spec.Project)
	case !gcpNotFound(err):
		return err
	}

	return nil
}

// make runs the steps up to the image; cleanup follows, whatever it returns.
func (r *gcpImageRun) make(ctx context.Context) error {
	r.emit(cloudPhaseCreating, "creating the throwaway VM "+r.helper()+" ("+r.spec.MachineType+" in "+r.spec.Zone+")")

	operation, err := r.insert(ctx, r.client.compute(r.spec.Project, "/zones/"+r.spec.Zone+"/instances"), r.helperBody(), &r.helperPosted)
	if err != nil {
		return err
	}

	if err := r.client.waitOperation(ctx, r.spec.Project, r.spec.Zone, operation, r.poll); err != nil {
		return err
	}

	r.emit(hetznerPhaseWriting, "VM "+r.helper()+" writes "+cloudImageURL(r.target, cloudPlatformGCP))

	if err := r.waitStopped(ctx); err != nil {
		return err
	}

	if err := r.checkConsole(ctx); err != nil {
		return err
	}

	r.emit(awsPhaseRegister, "making the image "+r.name+" from the data disk")

	return r.register(ctx)
}

// insert sends one insert, again with the same requestId (a no-op once the first went through)
// while it gets no clear answer, and returns its operation. posted is set before the first send.
func (r *gcpImageRun) insert(ctx context.Context, u string, body gcpJSON, posted *bool) (string, error) {
	requestID, err := gcpRequestID()
	if err != nil {
		return "", err
	}

	u += "?" + url.Values{"requestId": {requestID}}.Encode()
	*posted = true

	for try := range gcpInsertTries {
		var op gcpOperation

		if _, err = r.client.do(ctx, http.MethodPost, u, body, &op); err == nil {
			return op.Name, nil
		}

		if !gcpTransient(err) || try == gcpInsertTries-1 {
			break
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(r.poll):
		}
	}

	return "", err
}

// helperBody is the helper's insert: Debian with a 10 GB boot disk deleted with it, and an empty
// data disk that outlives it (the image is made from it), the script as its startup script.
func (r *gcpImageRun) helperBody() gcpJSON {
	s := r.spec
	labels := map[string]string{gcpLabelRun: r.runID}

	subnet := "regions/" + r.subnetRegion + "/subnetworks/" + r.subnetName
	if r.subnetProject != s.Project {
		subnet = "projects/" + r.subnetProject + "/" + subnet
	}

	nic := gcpJSON{"subnetwork": subnet}
	if s.AssociatePublicIP {
		nic["accessConfigs"] = []gcpJSON{{"type": "ONE_TO_ONE_NAT", "name": "External NAT"}}
	}

	diskType := "zones/" + s.Zone + "/diskTypes/pd-balanced"

	return gcpJSON{
		"name": r.helper(), "machineType": "zones/" + s.Zone + "/machineTypes/" + s.MachineType,
		"disks": []gcpJSON{
			{
				"boot": true, "autoDelete": true,
				"initializeParams": gcpJSON{"sourceImage": r.helperImage, "diskSizeGb": fmt.Sprint(gcpImageBootGB), "diskType": diskType},
			},
			{
				"autoDelete": false, "deviceName": gcpImageDevice,
				"initializeParams": gcpJSON{"diskName": r.dataDisk(), "diskSizeGb": fmt.Sprint(gcpImageDiskGB), "diskType": diskType, "labels": labels},
			},
		},
		"networkInterfaces": []gcpJSON{nic},
		"metadata": gcpJSON{"items": []gcpJSON{
			{"key": "startup-script", "value": gcpImageScript(cloudImageURL(r.target, cloudPlatformGCP))},
			{"key": "serial-port-logging-enable", "value": "true"},
		}},
		"labels":     labels,
		"scheduling": gcpJSON{"automaticRestart": false},
	}
}
