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
	"regexp"
	"strings"
	"time"
)

// S8 step 18: a Talos managed image for the cluster's schematic at Azure, made from the phone.
// Azure has no official Talos image, and the documented route (a blob upload, then az image
// create) needs a laptop, so the run drives a throwaway helper VM instead: Ubuntu with an empty
// managed data disk, and a script run through Run Command (no SSH, no storage account) that
// downloads the Image Factory's azure image and writes it to that disk. The app then deallocates
// the helper, makes a managed image from the data disk, and deletes the helper (its OS disk, NIC
// and public IP go with it) and the data disk. The phone moves no image bytes. Billable: one small
// VM for a few minutes, then the image's storage. Every path ends in cleanup.

const (
	azureImageTimeout        = time.Hour
	azureImageWriteWait      = 20 * time.Minute
	azureImageDeallocateWait = 5 * time.Minute
	azureImageCreateWait     = 15 * time.Minute
	// azureImageCleanupTimeout covers the helper's delete and the disk's, each up to azureDeleteWait.
	azureImageCleanupTimeout = 12 * time.Minute
	azureImageCleanupTries   = 5
	// Run Command needs the VM agent, which may be not ready for a minute after the boot.
	azureImageRunTries  = 5
	azureImageRunRetry  = 15 * time.Second
	azureImageAudit     = "azure-image-create"
	azureImageMinutes   = 10
	azureImageDiskGB    = 8
	azureImageOSDiskGB  = 30
	azureImageSizeAMD64 = "Standard_B2s"
	azureImageSizeARM64 = "Standard_B2pls_v2"
	azureImageUser      = "ichor"
	azureImageNote      = "the image is billed as a Standard LRS managed disk of its size per month, until it is deleted"

	// Phases; writing and snapshot (making the image) are the Hetzner snapshot's.
	azureImagePhaseWriting  = hetznerPhaseWriting
	azureImagePhaseSnapshot = hetznerPhaseSnapshot
)

var (
	// azureImageNamePattern is a managed image's name: 1 to 80 letters, digits, '.', '-' and '_',
	// starting with a letter or a digit and ending with a letter, a digit or '_'.
	azureImageNamePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,78}[A-Za-z0-9_])?$`)

	// azureImageActions are the image run's writes on top of a VM create's (azureCreateActions).
	azureImageActions = []string{
		"Microsoft.Compute/virtualMachines/runCommand/action", "Microsoft.Compute/virtualMachines/deallocate/action",
		"Microsoft.Compute/disks/write", "Microsoft.Compute/images/write", "Microsoft.Compute/images/delete",
	}
)

// azureImageSpec is StartAzureImage's specJSON. The image URL is never in it: it comes from the
// template's schematic and version.
type azureImageSpec struct {
	TemplateNode    string `json:"templateNode"`
	Arch            string `json:"arch"`
	ResourceGroup   string `json:"resourceGroup"`
	Location        string `json:"location"`
	SubnetID        string `json:"subnetId"`
	SecurityGroupID string `json:"securityGroupId"`
	Size            string `json:"size"`
	Name            string `json:"name"`
}

type azureImageResult struct {
	ImageID      string `json:"imageId"`
	Name         string `json:"name"`
	Arch         string `json:"arch"`
	TalosVersion string `json:"talosVersion"`
	Schematic    string `json:"schematic"`
	Generation   string `json:"generation"`
	SizeGB       int    `json:"sizeGB"`
}

// StartAzureImage makes a Talos managed image for the template's version and schematic in a
// resource group: creating → writing → snapshot (making the image) → cleanup → done. specJSON:
// {"templateNode","arch","resourceGroup","location","subnetId","securityGroupId","size","name"};
// templateNode "" takes the first member that answers, arch "" the template's, size ""
// Standard_B2s (amd64) or Standard_B2pls_v2 (arm64), securityGroupId "" none, name ""
// talos-<version>-azure-<arch>-<schematic12>. The helper gets a public address to reach the Image
// Factory. credJSON: as AzureTest's. OnDone's first argument is the result JSON, not an address:
// {"imageId","name","arch","talosVersion","schematic","generation","sizeGB"}. Refused before any
// write: a principal whose permissions lack a write the run makes, a template whose schematic
// cannot be read, a size of another architecture, a subnet or security group of another location,
// an image of this version and schematic already in the resource group (unless name is given), a
// name already taken. Cancel, or any failure, deletes the helper, its data disk and an unfinished
// image, and the message names what could not be. Nothing of the cluster is written. Refused on
// the demo; recorded in the activity log as azure-image-create. Billable: the app shows
// AzureImageEstimate first.
func StartAzureImage(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec azureImageSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)
	if specErr != nil {
		specErr = errors.New("invalid image request")
	}

	contextName, spec.TemplateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(spec.TemplateNode))

	listener = maskedJoinListener{listener}

	r := &azureImageRun{
		config: configYAML, context: contextName, credJSON: credJSON, spec: spec, specErr: specErr,
		listener: listener, poll: azurePollInterval, retryWait: azureImageRunRetry,
		writeWait: azureImageWriteWait, deallocateWait: azureImageDeallocateWait, imageWait: azureImageCreateWait,
	}

	return r.start(azureImageTimeout)
}

// azureImageRun is one run of StartAzureImage.
type azureImageRun struct {
	config, context string
	credJSON        string
	spec            azureImageSpec
	specErr         error
	listener        JoinListener
	poll, retryWait time.Duration
	writeWait       time.Duration
	deallocateWait  time.Duration
	imageWait       time.Duration

	client *azureClient
	target bootTarget
	size   string
	runID  string
	name   string
	result azureImageResult

	// The creates this run sent (a create without a clear answer may have made its resource).
	ipPosted, nicPosted, vmPosted, imagePosted bool
}

func (r *azureImageRun) start(timeout time.Duration) *JoinRun {
	run := func(ctx context.Context) error { return r.run(ctx, timeout) }
	done := func() (any, string) { return r.result, "image " + r.result.Name + " is ready" }

	return startCloudImageRun(r.config, r.context, r.listener, r.auditAction, timeout, run, done, r.emit)
}

func (r *azureImageRun) auditAction() auditAction {
	params := fmt.Sprintf("arch=%s location=%s version=%s", cmp.Or(r.target.arch, r.spec.Arch), r.spec.Location, r.target.version)
	if r.result.ImageID != "" {
		params += " image=" + r.client.scrub(r.result.ImageID)
	}

	return auditAction{Action: azureImageAudit, Node: r.spec.TemplateNode, Params: params}
}

func (r *azureImageRun) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, r.listener.OnProgress)
}

func (r *azureImageRun) run(ctx context.Context, timeout time.Duration) error {
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

func (r *azureImageRun) helper() string { return "ichor-image-" + r.runID }

func (r *azureImageRun) groupPath() string {
	return "/subscriptions/" + r.client.subscription + "/resourceGroups/" + url.PathEscape(r.spec.ResourceGroup) + "/providers/"
}

func (r *azureImageRun) vmPath() string {
	return r.groupPath() + "Microsoft.Compute/virtualMachines/" + r.helper()
}

func (r *azureImageRun) nicPath() string {
	return r.groupPath() + "Microsoft.Network/networkInterfaces/" + r.helper() + "-nic"
}

func (r *azureImageRun) ipPath() string {
	return r.groupPath() + "Microsoft.Network/publicIPAddresses/" + r.helper() + "-ip"
}

func (r *azureImageRun) diskPath() string {
	return r.groupPath() + "Microsoft.Compute/disks/" + r.helper() + "-data"
}

func (r *azureImageRun) imagePath() string {
	return r.groupPath() + "Microsoft.Compute/images/" + r.name
}

// checkSpec refuses a request that is incomplete or whose names would not fit in a path.
func (r *azureImageRun) checkSpec() error {
	s := &r.spec
	s.Location, s.ResourceGroup = strings.ToLower(strings.TrimSpace(s.Location)), strings.TrimSpace(s.ResourceGroup)
	s.SubnetID, s.SecurityGroupID, s.Size = strings.TrimSpace(s.SubnetID), strings.TrimSpace(s.SecurityGroupID), strings.TrimSpace(s.Size)

	switch {
	case s.ResourceGroup == "" || s.Location == "" || s.SubnetID == "":
		return errors.New("pick a resource group, a location and a subnet that reaches the internet")
	case !azureGroupPattern.MatchString(s.ResourceGroup) || strings.HasSuffix(s.ResourceGroup, "."):
		return fmt.Errorf("%q is not a resource group name", s.ResourceGroup)
	case !azureLocationPattern.MatchString(s.Location):
		return fmt.Errorf("%q is not an Azure location (westeurope)", s.Location)
	case s.Size != "" && !azureSizePattern.MatchString(s.Size):
		return fmt.Errorf("%q is not a VM size (Standard_B2s)", s.Size)
	}

	for _, id := range []string{s.SubnetID, s.SecurityGroupID} {
		if id != "" && (!azureIDPattern.MatchString(id) || strings.Contains(id, "..")) {
			return fmt.Errorf("%q is not an Azure resource id", id)
		}
	}

	if !strings.Contains(strings.ToLower(s.SubnetID), "/subnets/") {
		return fmt.Errorf("%q is not a subnet id", s.SubnetID)
	}

	return nil
}

// prepare checks everything that can be refused before a write, the principal's permissions last.
func (r *azureImageRun) prepare(ctx context.Context) error {
	arch, err := normalizeBootArch(r.spec.Arch)
	if err != nil {
		return err
	}

	if err := r.checkSpec(); err != nil {
		return err
	}

	if r.client, err = newAzureClient(r.credJSON); err != nil {
		return err
	}

	if err := r.client.mint(ctx); err != nil {
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
	r.size = cmp.Or(r.spec.Size, azureImageSize(r.target.arch))

	size, err := azureFindSize(ctx, r.client, r.spec.Location, r.size)
	if err != nil {
		return err
	}

	if size.Arch != r.target.arch {
		return fmt.Errorf("size %s is %s, the image %s: pick another size", r.size, size.Arch, r.target.arch)
	}

	if _, err := azureCheckNetwork(ctx, r.client, r.spec.SubnetID, r.spec.SecurityGroupID, r.spec.Location); err != nil {
		return err
	}

	if err := r.checkName(ctx); err != nil {
		return err
	}

	if err := r.checkPermissions(ctx); err != nil {
		return err
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	r.runID = hex.EncodeToString(id)

	return nil
}

// azureImageSize is the helper's default size: a small burstable of the architecture.
func azureImageSize(arch string) string {
	if arch == bootArchARM64 {
		return azureImageSizeARM64
	}

	return azureImageSizeAMD64
}

// azureImageGeneration is the image's Hyper-V generation: V1 on amd64 (the Talos guide's
// default; the Factory's VHD boots both ways), V2 on arm64 (the only one there).
func azureImageGeneration(arch string) string {
	if arch == bootArchARM64 {
		return azureGenV2
	}

	return azureGenV1
}

// checkName refuses a name Azure would, or one the resource group holds; without a name, also an
// image the app made for this version and schematic: a second one needs a name of its own.
func (r *azureImageRun) checkName(ctx context.Context) error {
	schematic := r.target.schematic
	r.name = strings.TrimSpace(r.spec.Name)

	if r.name == "" {
		r.name = fmt.Sprintf("talos-%s-azure-%s-%s", r.target.version, r.target.arch, schematic[:min(awsSchematicShown, len(schematic))])
	}

	if !azureImageNamePattern.MatchString(r.name) {
		return fmt.Errorf("%q is not an image name (1 to 80 letters, digits, '.', '-' and '_')", r.name)
	}

	images, err := azureList[azureAPIImage](ctx, r.client, r.groupPath()+"Microsoft.Compute/images", azureComputeVersion, nil)

	var e *azureError

	switch {
	case errors.As(err, &e) && e.code == azureCodeGroupNotFound:
		return fmt.Errorf("resource group %s does not exist: pick another", r.spec.ResourceGroup)
	case err != nil:
		return err
	}

	for _, i := range images {
		switch {
		case strings.EqualFold(i.Name, r.name):
			return fmt.Errorf("an image named %s exists in %s: pick another name", i.Name, r.spec.ResourceGroup)
		case r.spec.Name == "" && i.Tags[azureTagTalosVersion] == r.target.version && i.Tags[azureTagSchematic] == schematic &&
			i.managedArch() == r.target.arch:
			return fmt.Errorf("an image for Talos %s and this schematic exists: %s. Pick it, or give the new one a name", r.target.version, i.Name)
		}
	}

	return nil
}

// checkPermissions refuses a principal that may not make or delete what the run makes.
func (r *azureImageRun) checkPermissions(ctx context.Context) error {
	perms, err := r.client.permissions(ctx, r.spec.ResourceGroup)

	var e *azureError
	if errors.As(err, &e) && e.status == http.StatusForbidden {
		return errors.New("this service principal may not read its own permissions: add Microsoft.Authorization/permissions/read (paste the printed role)")
	}

	if err != nil {
		return err
	}

	actions := append(azureCreateActions(true), azureImageActions...)
	if r.spec.SecurityGroupID != "" {
		actions = append(actions, "Microsoft.Network/networkSecurityGroups/join/action")
	}

	if missing := missingAction(perms, actions); missing != "" {
		return fmt.Errorf("this service principal may not %s in resource group %s: paste the printed role", missing, r.spec.ResourceGroup)
	}

	return nil
}

// make runs the steps up to the image; cleanup follows, whatever it returns.
func (r *azureImageRun) make(ctx context.Context) error {
	r.emit(cloudPhaseCreating, "creating the throwaway VM "+r.helper()+" ("+r.size+" in "+r.spec.Location+")")

	if err := r.createHelper(ctx); err != nil {
		return err
	}

	if err := r.waitRunning(ctx); err != nil {
		return err
	}

	r.emit(azureImagePhaseWriting, "VM "+r.helper()+" writes "+cloudImageURL(r.target, cloudPlatformAzure))

	if err := r.write(ctx); err != nil {
		return err
	}

	r.emit(azureImagePhaseWriting, "stopping VM "+r.helper()+" so that its data disk can be read")

	if err := r.deallocate(ctx); err != nil {
		return err
	}

	r.emit(azureImagePhaseSnapshot, "making the image "+r.name+" from the data disk")

	return r.makeImage(ctx)
}

// put creates one resource and waits for its operation.
func (r *azureImageRun) put(ctx context.Context, path, version string, body any) (string, error) {
	var answer azureAPIRef

	_, headers, err := r.client.do(ctx, http.MethodPut, path, url.Values{"api-version": {version}}, body, &answer)
	if err != nil {
		return "", err
	}

	if err := r.client.wait(ctx, headers, r.poll); err != nil {
		return "", err
	}

	return cmp.Or(answer.ID, path), nil
}

// createHelper makes the public address, the NIC and the helper VM, each deleted with the VM,
// except the data disk (Detach: the image is made from it once the VM is deallocated).
func (r *azureImageRun) createHelper(ctx context.Context) error {
	tags := map[string]string{"Name": r.helper(), azureTagRun: r.runID}

	r.ipPosted = true

	ipID, err := r.put(ctx, r.ipPath(), azureNetworkVersion, azureJSON{
		"location": r.spec.Location, "tags": tags, "sku": azureJSON{"name": "Standard"},
		"properties": azureJSON{"publicIPAllocationMethod": "Static"},
	})
	if err != nil {
		return err
	}

	nic := azureJSON{"ipConfigurations": []azureJSON{{"name": "ipconfig1", "properties": azureJSON{
		"subnet": azureJSON{"id": r.spec.SubnetID}, "privateIPAllocationMethod": "Dynamic",
		"publicIPAddress": azureJSON{"id": ipID, "properties": azureJSON{"deleteOption": "Delete"}},
	}}}}
	if r.spec.SecurityGroupID != "" {
		nic["networkSecurityGroup"] = azureJSON{"id": r.spec.SecurityGroupID}
	}

	r.nicPosted = true

	nicID, err := r.put(ctx, r.nicPath(), azureNetworkVersion, azureJSON{"location": r.spec.Location, "tags": tags, "properties": nic})
	if err != nil {
		return err
	}

	key, err := azureThrowawayKey()
	if err != nil {
		return err
	}

	r.vmPosted = true

	_, _, err = r.client.do(ctx, http.MethodPut, r.vmPath(), url.Values{"api-version": {azureComputeVersion}}, r.vmBody(key, nicID, tags), nil)

	return err
}

// vmBody is the helper's create request: Canonical's Ubuntu 24.04 (its VM agent runs Run
// Command), and an empty data disk at LUN 0.
func (r *azureImageRun) vmBody(key, nicID string, tags map[string]string) azureJSON {
	sku := "server"
	if r.target.arch == bootArchARM64 {
		sku = "server-arm64"
	}

	return azureJSON{
		"location": r.spec.Location, "tags": tags,
		"properties": azureJSON{
			"hardwareProfile": azureJSON{"vmSize": r.size},
			"storageProfile": azureJSON{
				"imageReference": azureJSON{"publisher": "Canonical", "offer": "ubuntu-24_04-lts", "sku": sku, "version": "latest"},
				"osDisk": azureJSON{
					"createOption": "FromImage", "deleteOption": "Delete", "diskSizeGB": azureImageOSDiskGB,
					"managedDisk": azureJSON{"storageAccountType": "StandardSSD_LRS"},
				},
				"dataDisks": []azureJSON{{
					"lun": 0, "name": r.helper() + "-data", "createOption": "Empty", "diskSizeGB": azureImageDiskGB, "deleteOption": "Detach",
					"managedDisk": azureJSON{"storageAccountType": "StandardSSD_LRS"},
				}},
			},
			"osProfile": azureJSON{
				"computerName": r.helper(), "adminUsername": azureImageUser,
				"linuxConfiguration": azureJSON{
					"disablePasswordAuthentication": true,
					"ssh":                           azureJSON{"publicKeys": []azureJSON{{"path": "/home/" + azureImageUser + "/.ssh/authorized_keys", "keyData": key}}},
				},
			},
			"networkProfile": azureJSON{"networkInterfaces": []azureJSON{{"id": nicID, "properties": azureJSON{"primary": true, "deleteOption": "Delete"}}}},
		},
	}
}

// waitRunning follows the helper until it runs. A provider hiccup is polled through, as is a VM
// ARM does not list yet (for azureNotFoundGrace).
func (r *azureImageRun) waitRunning(ctx context.Context) error {
	last, missingSince := "", time.Time{}

	for {
		var vm azureAPIVM

		_, _, err := r.client.do(ctx, http.MethodGet, r.vmPath(), url.Values{"api-version": {azureComputeVersion}, "$expand": {"instanceView"}}, nil, &vm)
		if azureNotFound(err) && missingSince.IsZero() {
			missingSince = time.Now()
		}

		state := vm.Properties.ProvisioningState

		switch {
		case azureNotFound(err) && time.Since(missingSince) < azureNotFoundGrace:
		case err != nil && !azureTransient(err):
			return err
		case err == nil && strings.EqualFold(state, "Failed"):
			return fmt.Errorf("VM %s failed: %s", r.helper(), r.client.scrub(vm.failure()))
		case err == nil && strings.EqualFold(state, azureOpSucceeded) && vm.status("PowerState/running"):
			return nil
		case err == nil && state != last:
			last = state
			r.emit(cloudPhaseCreating, "VM "+r.helper()+": "+strings.ToLower(last))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.poll):
		}
	}
}
