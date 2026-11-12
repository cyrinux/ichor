package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The steps of StartAzureImage after the helper runs, its cleanup and its estimate.

// azureImageScript is the helper's Run Command script. It writes the image to the data disk at
// LUN 0, found through the Azure udev links, else by its size next to the root disk (never a
// guessed /dev/sdc), and prints a marker: -OK, or -FAIL: and the reason. The marker is built at
// run time, so that the script itself never shows it. Run Command returns at most 4 KB of output:
// the script prints only the markers and one line.
func azureImageScript(imageURL string) string {
	return `#!/bin/bash
# Ichor: write a Talos image to the empty data disk.
set -euo pipefail
m=` + awsImageMarker + `
trap 'echo "$m-FAIL: line $LINENO exited $?"' ERR
root=$(lsblk -no PKNAME "$(findmnt -no SOURCE /)")
disk=""
for link in /dev/disk/azure/scsi1/lun0 /dev/disk/azure/data/by-lun/0; do
  if [ -e "$link" ]; then disk=$(readlink -f "$link"); break; fi
done
if [ -z "$disk" ]; then
  resource=$(readlink -f /dev/disk/azure/resource 2>/dev/null || true)
  disk=$(lsblk -dbnpo NAME,SIZE,TYPE | awk -v root="/dev/$root" -v other="$resource" -v size=` +
		strconv.FormatInt(int64(azureImageDiskGB)<<30, 10) + ` '$3 == "disk" && $2 == size && $1 != root && $1 != other { print $1; exit }')
fi
if [ -z "$disk" ] || [ "$disk" = "/dev/$root" ]; then echo "$m-FAIL: no empty ` + strconv.Itoa(azureImageDiskGB) + ` GiB data disk at LUN 0"; exit 1; fi
echo "ichor: writing the image to $disk"
curl -fsSL --retry 3 '` + imageURL + `' | xz -dc | dd of="$disk" bs=4M conv=sparse status=none
sync
echo "$m-OK"
`
}

// azureRunCommandOutput is Run Command's answer: in its operation's properties, or in the
// result its Location gives.
type azureRunCommandOutput struct {
	Value []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"value"`
}

func (o azureRunCommandOutput) text() string {
	lines := make([]string, 0, len(o.Value))
	for _, v := range o.Value {
		lines = append(lines, v.Message)
	}

	return strings.Join(lines, "\n")
}

type azureRunCommandOperation struct {
	azureOperation
	Properties struct {
		Output azureRunCommandOutput `json:"output"`
	} `json:"properties"`
}

// write runs the script on the helper and reads its marker. No OK marker, no image: a disk of
// zeros would make an image that does not boot.
func (r *azureImageRun) write(ctx context.Context) error {
	output, err := r.runCommand(ctx)
	if err != nil {
		return err
	}

	ok, failure, tail := awsConsoleVerdict(output)

	switch {
	case ok:
		return nil
	case failure != "":
		return fmt.Errorf("the helper could not write the image: %s", failure)
	}

	for _, line := range tail {
		r.emit(azureImagePhaseWriting, line)
	}

	return fmt.Errorf("the helper's Run Command shows no result, so the image may not be written: %s", cmpOr(strings.Join(tail, " / "), "its output is empty"))
}

// runCommand sends the script, tried again while the VM agent is not ready (a conflict, a
// failed operation or a provider hiccup), and returns its output.
func (r *azureImageRun) runCommand(ctx context.Context) (string, error) {
	body := azureJSON{"commandId": "RunShellScript", "script": []string{azureImageScript(cloudImageURL(r.target, cloudPlatformAzure))}}

	var err error

	for try := range azureImageRunTries {
		var output string

		output, err = r.runCommandOnce(ctx, body)
		if err == nil {
			return output, nil
		}

		var e *azureError
		if ctx.Err() != nil || !(azureTransient(err) || errors.As(err, &e) && (e.status == http.StatusConflict || e.action == "operation")) {
			return "", err
		}

		if try < azureImageRunTries-1 {
			r.emit(azureImagePhaseWriting, "Run Command is not ready yet ("+friendlyError(err)+"): trying again")

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(r.retryWait):
			}
		}
	}

	return "", err
}

func (r *azureImageRun) runCommandOnce(ctx context.Context, body azureJSON) (string, error) {
	var direct azureRunCommandOutput

	status, headers, err := r.client.do(ctx, http.MethodPost, r.vmPath()+"/runCommand", url.Values{"api-version": {azureComputeVersion}}, body, &direct)
	if err != nil {
		return "", err
	}

	async, location := headers.Get("Azure-AsyncOperation"), headers.Get("Location")
	if status == http.StatusOK || (async == "" && location == "") {
		return direct.text(), nil
	}

	deadline := time.Now().Add(r.writeWait)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(r.poll):
		}

		done, output, err := r.pollRunCommand(ctx, async, location)
		if done || err != nil {
			return output, err
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("VM %s did not finish writing the image within %s", r.helper(), r.writeWait)
		}
	}
}

// pollRunCommand reads Run Command's operation once: done with the output when it has ended.
func (r *azureImageRun) pollRunCommand(ctx context.Context, async, location string) (bool, string, error) {
	if async == "" {
		var result azureRunCommandOutput

		status, _, err := r.client.do(ctx, http.MethodGet, location, nil, nil, &result)

		switch {
		case err != nil && !azureTransient(err):
			return true, "", err
		case err != nil || status == http.StatusAccepted:
			return false, "", nil
		}

		return true, result.text(), nil
	}

	var op azureRunCommandOperation

	status, _, err := r.client.do(ctx, http.MethodGet, async, nil, nil, &op)

	switch {
	case err != nil && !azureTransient(err):
		return true, "", err
	case err != nil:
		return false, "", nil
	case op.Status == "Failed" || op.Status == "Canceled":
		return true, "", &azureError{action: "operation", code: op.Error.Code, message: r.client.scrub(cmp.Or(op.Error.Message, op.Status)), status: status}
	case op.Status != azureOpSucceeded:
		return false, "", nil
	case len(op.Properties.Output.Value) > 0 || location == "":
		return true, op.Properties.Output.text(), nil
	}

	var result azureRunCommandOutput

	_, _, err = r.client.do(ctx, http.MethodGet, location, nil, nil, &result)

	return true, result.text(), err
}

// deallocate stops the helper and releases its host, so that its data disk is consistent and
// can be read by the image.
func (r *azureImageRun) deallocate(ctx context.Context) error {
	_, headers, err := r.client.do(ctx, http.MethodPost, r.vmPath()+"/deallocate", url.Values{"api-version": {azureComputeVersion}}, nil, nil)
	if err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, r.deallocateWait)
	defer cancel()

	if err := r.client.wait(waitCtx, headers, r.poll); err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("VM %s was not stopped within %s", r.helper(), r.deallocateWait)
		}

		return err
	}

	return nil
}

func (r *azureImageRun) imageTags() map[string]string {
	return map[string]string{
		"Name": r.name, azureTagRun: r.runID, "os": "talos", azureTagTalosVersion: r.target.version,
		azureTagArch: r.target.arch, azureTagSchematic: r.target.schematic,
	}
}

// makeImage makes the managed image from the data disk and waits until it is ready.
func (r *azureImageRun) makeImage(ctx context.Context) error {
	generation := azureImageGeneration(r.target.arch)
	body := azureJSON{
		"location": r.spec.Location, "tags": r.imageTags(),
		"properties": azureJSON{
			"hyperVGeneration": generation,
			"storageProfile": azureJSON{
				"osDisk": azureJSON{
					"osType": "Linux", "osState": "Generalized", "managedDisk": azureJSON{"id": r.diskPath()}, "storageAccountType": "Standard_LRS",
				},
				"zoneResilient": false,
			},
		},
	}

	waitCtx, cancel := context.WithTimeout(ctx, r.imageWait)
	defer cancel()

	r.imagePosted = true

	id, err := r.put(waitCtx, r.imagePath(), azureComputeVersion, body)
	if err == nil {
		err = r.waitImage(waitCtx)
	}

	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("image %s was not ready within %s", r.name, r.imageWait)
		}

		return err
	}

	r.result = azureImageResult{
		ImageID: id, Name: r.name, Arch: r.target.arch, TalosVersion: r.target.version, Schematic: r.target.schematic,
		Generation: generation, SizeGB: azureImageDiskGB,
	}

	return nil
}

// waitImage reads the image until its provisioning has ended.
func (r *azureImageRun) waitImage(ctx context.Context) error {
	for {
		var image struct {
			Properties struct {
				ProvisioningState string `json:"provisioningState"`
			} `json:"properties"`
		}

		err := r.client.get(ctx, r.imagePath(), azureComputeVersion, &image)
		state := image.Properties.ProvisioningState

		switch {
		case err != nil && !azureTransient(err) && !azureNotFound(err):
			return err
		case err == nil && (state == "" || strings.EqualFold(state, azureOpSucceeded)):
			return nil
		case err == nil && strings.EqualFold(state, "Failed"):
			return fmt.Errorf("image %s failed", r.name)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.poll):
		}
	}
}

// cleanup removes what this run created and adds the outcome to why: always the helper (its OS
// disk, NIC and public IP go with it) and its data disk, and on a failure an unfinished image. Its
// context is its own: the run's has often ended. An image that is ready stays.
func (r *azureImageRun) cleanup(why error) error {
	if r.client == nil || !r.ipPosted {
		return why
	}

	ctx, cancel := context.WithTimeout(context.Background(), azureImageCleanupTimeout)
	defer cancel()

	r.emit(cloudPhaseCleanup, "deleting the throwaway VM "+r.helper()+" and its data disk")

	var deleted, left []string

	remove := func(what, path, version string, extra url.Values) {
		query := url.Values{"api-version": {version}}
		for k, v := range extra {
			query[k] = v
		}

		existed, err := r.deleteRetrying(ctx, path, query)

		switch {
		case err != nil:
			left = append(left, what+" "+r.client.scrub(path)+" ("+friendlyError(err)+")")
		case existed:
			deleted = append(deleted, what)
		}
	}

	if why != nil && r.imagePosted {
		remove("unfinished image "+r.name, r.imagePath(), azureComputeVersion, nil)
	}

	if r.vmPosted {
		remove("VM "+r.helper()+" (with its OS disk, NIC and public IP)", r.vmPath(), azureComputeVersion, url.Values{"forceDeletion": {"true"}})
	}

	// Made before a VM that never came, or left by a VM delete that failed.
	if r.nicPosted {
		remove("network interface "+r.helper()+"-nic", r.nicPath(), azureNetworkVersion, nil)
	}

	remove("public IP address "+r.helper()+"-ip", r.ipPath(), azureNetworkVersion, nil)

	if r.vmPosted {
		remove("data disk "+r.helper()+"-data", r.diskPath(), azureComputeVersion, nil)
	}

	return azureCleanupOutcome(why, r.name, r.spec.ResourceGroup, deleted, left)
}

// deleteRetrying sends one delete until it holds: a conflict (a disk still attached while its VM
// is deleted) or a provider hiccup is tried again. existed is false when it was already gone.
func (r *azureImageRun) deleteRetrying(ctx context.Context, path string, query url.Values) (bool, error) {
	var (
		existed bool
		err     error
	)

	for try := range azureImageCleanupTries {
		existed, err = azureDeleteResource(ctx, r.client, r.poll, path, query)

		var e *azureError
		if err == nil || !(azureTransient(err) || errors.As(err, &e) && e.status == http.StatusConflict) {
			return existed, err
		}

		if try < azureImageCleanupTries-1 {
			select {
			case <-ctx.Done():
				return existed, err
			case <-time.After(r.poll):
			}
		}
	}

	return existed, err
}

func azureCleanupOutcome(why error, name, resourceGroup string, deleted, left []string) error {
	stillThere := ""
	if len(left) > 0 {
		stillThere = "Could not be deleted, and still billed: " + strings.Join(left, ", ") + " in resource group " + resourceGroup +
			". Delete it in the Azure portal"
	}

	return cloudCleanupOutcome(why, "image "+name+" is ready", stillThere, deleted)
}

type azureImageCost struct {
	Size      string `json:"size"`
	Hourly    string `json:"hourly"`
	Currency  string `json:"currency"`
	ImageNote string `json:"imageNote"`
	Minutes   int    `json:"minutes"`
}

// AzureImageEstimate is the cost line of an image run: the helper's size and its hourly price
// (it runs about ten minutes), and a note on the image's storage, billed until it is deleted. An
// hourly price the Retail Prices API does not give (or a sovereign cloud) is "". Creates
// nothing; credJSON is read for its "environment" only. JSON: {"size","hourly","currency",
// "imageNote","minutes"}.
func AzureImageEstimate(credJSON, arch, location string) (out string, err error) {
	defer maskResult(&out, &err)

	arch, err = normalizeBootArch(arch)
	if err != nil {
		return "", err
	}

	if arch == "" {
		return "", errors.New("pick an architecture (amd64, arm64)")
	}

	c, err := parseAzureCredential(credJSON)
	if err != nil {
		return "", err
	}

	if location = strings.ToLower(strings.TrimSpace(location)); !azureLocationPattern.MatchString(location) {
		return "", fmt.Errorf("%q is not an Azure location (westeurope)", location)
	}

	cost := azureImageCost{Size: azureImageSize(arch), ImageNote: azureImageNote, Minutes: azureImageMinutes}

	if azureARMHosts[c.Environment] == azureARMHosts[""] {
		cost.Hourly = azurePricing(context.Background(), location, cost.Size)
	}

	if cost.Hourly != "" {
		cost.Currency = "USD"
	}

	return toJSON(cost)
}
