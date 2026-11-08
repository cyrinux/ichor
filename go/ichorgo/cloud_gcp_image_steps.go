package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The steps of StartGCPImage after the helper is created, its cleanup and its estimate.

// gcpImageSerialTail is how much of the helper's serial console is read: the end, where the
// marker is (a negative start counts from the last byte written).
const gcpImageSerialTail = 64 << 10

// gcpImageScript is the helper's startup script. It writes the archive's disk.raw to the data
// disk, found by its deviceName (never a guessed /dev/sdb), prints a marker on the serial console,
// and powers off either way. The marker is built at run time, so that the script itself never
// shows it. Debian 12 has curl and GNU tar (which unpacks the sparse disk.raw): nothing is
// installed.
func gcpImageScript(imageURL string) string {
	return `#!/bin/bash
# Ichor: write a Talos image to the empty data disk, then power off.
set -euo pipefail
m=` + awsImageMarker + `
say() { for t in /dev/ttyS0 /dev/console; do echo "$*" > "$t" 2>/dev/null || true; done; }
end() { sync; shutdown -h now; }
trap 'say "$m-FAIL: line $LINENO exited $?"; end' ERR
disk=/dev/disk/by-id/google-` + gcpImageDevice + `
if [ ! -b "$disk" ]; then say "$m-FAIL: no data disk at $disk"; end; exit 1; fi
say "ichor: writing the image to $disk"
curl -fsSL --retry 3 '` + imageURL + `' | tar -xzO disk.raw | dd of="$disk" bs=4M conv=sparse status=none
sync
say "$m-OK"
end
`
}

// waitStopped follows the helper until it has powered off (TERMINATED); one that does not within
// writeWait is stopped, and the run fails. A provider hiccup is polled through, as is a helper
// Compute Engine does not list yet.
func (r *gcpImageRun) waitStopped(ctx context.Context) error {
	start := time.Now()
	deadline := start.Add(r.writeWait)
	last := ""

	for {
		var instance gcpAPIInstance

		err := r.client.get(ctx, r.instanceURL(), &instance)

		switch {
		case gcpNotFound(err) && time.Since(start) < gcpNotFoundGrace:
		case err != nil && !gcpTransient(err):
			return err
		case err == nil && instance.Status == gcpStatusStopped:
			return nil
		case err == nil && (instance.Status == "SUSPENDED" || instance.Status == "SUSPENDING"):
			return fmt.Errorf("VM %s is %s, not powered off", r.helper(), strings.ToLower(instance.Status))
		case err == nil && instance.Status != last:
			last = instance.Status
			r.emit(hetznerPhaseWriting, "VM "+r.helper()+": "+strings.ToLower(last))
		}

		if time.Now().After(deadline) {
			r.emit(hetznerPhaseWriting, "the helper did not power off: stopping it")
			_, _ = r.client.do(ctx, http.MethodPost, r.instanceURL()+"/stop", nil, nil) //nolint:errcheck // the run fails anyway

			return fmt.Errorf("VM %s did not finish writing the image within %s", r.helper(), r.writeWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.poll):
		}
	}
}

// checkConsole reads the end of the helper's serial console for its marker. The console may lag
// the power-off by a few seconds: it is read again, up to gcpImageConsoleTries times. No OK
// marker, no image: a disk of zeros would make an image that does not boot.
func (r *gcpImageRun) checkConsole(ctx context.Context) error {
	var tail []string

	for try := range gcpImageConsoleTries {
		var serial struct {
			Contents string `json:"contents"`
		}

		err := r.client.get(ctx, r.instanceURL()+"/serialPort?port=1&start=-"+strconv.Itoa(gcpImageSerialTail), &serial)
		if err != nil && !gcpTransient(err) {
			return err
		}

		ok, failure, lines := awsConsoleVerdict(serial.Contents)

		switch {
		case ok:
			return nil
		case failure != "":
			return fmt.Errorf("the helper could not write the image: %s", r.client.scrub(failure))
		}

		tail = lines

		if try < gcpImageConsoleTries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.poll):
			}
		}
	}

	for i, line := range tail {
		tail[i] = r.client.scrub(line)
		r.emit(hetznerPhaseWriting, tail[i])
	}

	return fmt.Errorf("the helper's serial console shows no result, so the image may not be written: %s", cmpOr(strings.Join(tail, " / "), "the console is empty"))
}

// imageBody is the image's insert, from the data disk, as the Talos guide registers it
// (VIRTIO_SCSI_MULTIQUEUE), with UEFI_COMPATIBLE on Arm (which boots only that) and gVNIC.
func (r *gcpImageRun) imageBody() gcpJSON {
	arch, features := gcpArchX86, []gcpJSON{{"type": gcpFeatureMultiqueue}, {"type": gcpFeatureGVNIC}}
	if r.target.arch == bootArchARM64 {
		arch = gcpArchARM
		features = append(features, gcpJSON{"type": gcpFeatureUEFI})
	}

	schematic := r.target.schematic

	return gcpJSON{
		"name": r.name, "sourceDisk": "zones/" + r.spec.Zone + "/disks/" + r.dataDisk(), "architecture": arch, "guestOsFeatures": features,
		"labels": map[string]string{
			"os": "talos", gcpLabelTalosVersion: gcpLabelOfVersion(r.target.version), gcpLabelArch: r.target.arch,
			gcpLabelSchematic: schematic[:min(gcpSchematicLabel, len(schematic))], gcpLabelRun: r.runID,
		},
		"description": fmt.Sprintf("Talos %s %s %s schematic %s", r.target.version, cloudPlatformGCP, r.target.arch, schematic),
		"family":      "talos-" + r.target.arch,
	}
}

// register makes the image from the data disk (the helper is powered off, so no forceCreate) and
// waits for it.
func (r *gcpImageRun) register(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, r.imageWait)
	defer cancel()

	operation, err := r.insert(waitCtx, r.client.compute(r.spec.Project, "/global/images"), r.imageBody(), &r.imagePosted)
	if err == nil {
		err = r.client.waitOperation(waitCtx, r.spec.Project, "", operation, r.poll)
	}

	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("image %s was not ready within %s", r.name, r.imageWait)
		}

		return err
	}

	var image gcpAPIImage
	if err := r.client.get(ctx, r.imageURL(), &image); err != nil {
		return err
	}

	if image.Status != gcpImageReady {
		return fmt.Errorf("image %s is %s, not ready", r.name, strings.ToLower(image.Status))
	}

	r.result = gcpImageResult{
		Image: r.name, SelfLink: image.SelfLink, Arch: r.target.arch, TalosVersion: r.target.version, Schematic: r.target.schematic,
		SizeGB: int(image.DiskSizeGb),
	}

	return nil
}

// cleanup removes what this run created and adds the outcome to why: always the helper (its boot
// disk goes with it) and its data disk, and on a failure an unfinished image. Its context is its
// own: the run's has often ended. An image that is ready stays.
func (r *gcpImageRun) cleanup(why error) error {
	if r.client == nil || !r.helperPosted {
		return why
	}

	ctx, cancel := context.WithTimeout(context.Background(), gcpImageCleanupTimeout)
	defer cancel()

	r.emit(cloudPhaseCleanup, "deleting the throwaway VM "+r.helper()+" and its data disk")

	var deleted, left []string

	remove := func(what, u, zone string) {
		existed, err := r.deleteRetrying(ctx, u, zone)

		switch {
		case err != nil:
			left = append(left, what+" ("+friendlyError(err)+")")
		case existed:
			deleted = append(deleted, what)
		}
	}

	if why != nil && r.imagePosted && r.ownImage(ctx) {
		remove("unfinished image "+r.name, r.imageURL(), "")
	}

	remove("VM "+r.helper()+" (with its boot disk)", r.instanceURL(), r.spec.Zone)
	remove("data disk "+r.dataDisk(), r.diskURL(), r.spec.Zone)

	return gcpImageCleanupOutcome(why, r.name, r.spec.Zone, deleted, left)
}

// ownImage: the image of the run's name carries the run's label (an insert without a clear answer
// may have made it; one of another run is never deleted). An image that cannot be read counts as
// the run's, so that its delete is tried and reported.
func (r *gcpImageRun) ownImage(ctx context.Context) bool {
	var image gcpAPIImage

	err := r.client.get(ctx, r.imageURL(), &image)

	return err != nil || image.Labels[gcpLabelRun] == r.runID
}

// deleteRetrying sends one delete and follows it for gcpDeleteWait, again while the resource is in
// use (a disk an image or a VM still holds) or Google hiccups. existed is false when it was already
// gone. A delete still running past gcpDeleteWait is an error: it may still be billed.
func (r *gcpImageRun) deleteRetrying(ctx context.Context, u, zone string) (bool, error) {
	var err error

	for try := range gcpImageCleanupTries {
		var op gcpOperation

		_, err = r.client.do(ctx, http.MethodDelete, u, nil, &op)
		if gcpNotFound(err) {
			return false, nil
		}

		if err == nil {
			waitCtx, cancel := context.WithTimeout(ctx, gcpDeleteWait)
			err = r.client.waitOperation(waitCtx, r.spec.Project, zone, op.Name, r.poll)

			cancel()

			if err == nil {
				return true, nil
			}

			if errors.Is(err, context.DeadlineExceeded) {
				return true, fmt.Errorf("the delete had not finished within %s", gcpDeleteWait)
			}
		}

		if (!gcpInUse(err) && !gcpTransient(err)) || try == gcpImageCleanupTries-1 {
			break
		}

		select {
		case <-ctx.Done():
			return true, err
		case <-time.After(r.poll):
		}
	}

	return true, err
}

// gcpInUse: Google refused a delete because another resource still uses it.
func gcpInUse(err error) bool {
	var e *gcpError

	return errors.As(err, &e) && slices.Contains([]string{"resourceInUseByAnotherResource", "RESOURCE_IN_USE_BY_ANOTHER_RESOURCE"}, e.reason)
}

func gcpImageCleanupOutcome(why error, name, zone string, deleted, left []string) error {
	stillThere := ""
	if len(left) > 0 {
		stillThere = "Could not be deleted, and still billed: " + strings.Join(left, ", ") + " in zone " + zone +
			". Delete it in the Google Cloud console"
	}

	switch {
	case why == nil && stillThere == "":
		return nil
	case why == nil:
		return fmt.Errorf("image %s is ready. %s", name, stillThere)
	case len(deleted) > 0 && stillThere != "":
		return fmt.Errorf("%w. Deleted: %s. %s", why, strings.Join(deleted, ", "), stillThere)
	case len(deleted) > 0:
		return fmt.Errorf("%w. Deleted: %s", why, strings.Join(deleted, ", "))
	case stillThere != "":
		return fmt.Errorf("%w. %s", why, stillThere)
	}

	return why
}

type gcpImageCost struct {
	MachineType       string `json:"machineType"`
	Hourly            string `json:"hourly"`
	Currency          string `json:"currency"`
	DiskMonthlyPerGB  string `json:"diskMonthlyPerGB"`
	ImageMonthlyPerGB string `json:"imageMonthlyPerGB"`
	Minutes           int    `json:"minutes"`
}

// GCPImageEstimate is the cost line of an image run: the helper's machine type and hourly price
// (it runs about ten minutes), the data disk's price per GB-month (a balanced persistent disk, for
// those minutes) and the image's storage price per GB-month, billed until it is deleted. A price
// the Cloud Billing Catalog does not give is "". Creates nothing. JSON: {"machineType","hourly",
// "currency","diskMonthlyPerGB","imageMonthlyPerGB","minutes"}. zone "" for the credential's.
func GCPImageEstimate(credJSON, arch, zone string) (out string, err error) {
	defer maskResult(&out, &err)

	arch, err = normalizeBootArch(arch)
	if err != nil {
		return "", err
	}

	if arch == "" {
		return "", errors.New("pick an architecture (amd64, arm64)")
	}

	c, err := newGCPClient(credJSON)
	if err != nil {
		return "", err
	}

	if zone = strings.ToLower(strings.TrimSpace(zone)); zone == "" {
		zone = c.zone
	}

	if !gcpZonePattern.MatchString(zone) {
		return "", fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", zone)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gcpOptionsTimeout)
	defer cancel()

	cost := gcpImageCost{MachineType: gcpImageType(arch), Minutes: gcpImageMinutes}
	t, ok := c.machineType(ctx, zone, cost.MachineType)
	skus := c.catalog(ctx)

	if ok {
		cost.Hourly = gcpHourly(skus, zone, cost.MachineType, t)
	}

	cost.DiskMonthlyPerGB = gcpStorageRate(skus, "Balanced PD Capacity", gcpRegionOf(zone))
	cost.ImageMonthlyPerGB = gcpStorageRate(skus, "Storage Image", "")

	if cost.Hourly != "" || cost.DiskMonthlyPerGB != "" || cost.ImageMonthlyPerGB != "" {
		cost.Currency = "USD"
	}

	return toJSON(cost)
}

// gcpStorageRate is the on-demand price of the first storage SKU whose description starts with
// prefix, in region when one is given; "" without one.
func gcpStorageRate(skus []gcpSKU, prefix, region string) string {
	for _, s := range skus {
		if s.Category.ResourceFamily != "Storage" || !strings.HasPrefix(s.Description, prefix) ||
			(region != "" && !slices.Contains(s.ServiceRegions, region)) {
			continue
		}

		if rate, ok := s.rate(); ok {
			return strconv.FormatFloat(rate, 'f', 4, 64)
		}
	}

	return ""
}
