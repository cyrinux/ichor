package ichorgo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The steps of StartAWSImage after the helper is created, its cleanup and its estimate.

const (
	// awsImageMarker starts the helper's last console line: -OK, or -FAIL: and the reason.
	awsImageMarker     = "ICHOR-IMAGE"
	awsImageMarkerOK   = awsImageMarker + "-OK"
	awsImageMarkerFail = awsImageMarker + "-FAIL"
	awsImageTailLines  = 5
)

// awsImageScript is the helper's user data. It writes the image to the one disk that is not the
// root's and has the data volume's size (never a guessed device name: on Nitro /dev/sdf is an
// NVMe device), prints a marker on the serial console, and powers off either way. The marker is
// built at run time, so that the script itself never shows it.
func awsImageScript(imageURL string) string {
	return `#!/bin/bash
# Ichor: write a Talos image to the empty data volume, then power off.
set -euo pipefail
m=` + awsImageMarker + `
say() { for t in /dev/ttyS0 /dev/console; do echo "$*" > "$t" 2>/dev/null || true; done; }
end() { sync; shutdown -h now; }
trap 'say "$m-FAIL: line $LINENO exited $?"; end' ERR
root=$(lsblk -no PKNAME "$(findmnt -no SOURCE /)")
disk=$(lsblk -dbnpo NAME,SIZE,TYPE | awk -v root="/dev/$root" -v size=` + strconv.FormatInt(int64(awsImageVolumeGiB)<<30, 10) + ` '$3 == "disk" && $2 == size && $1 != root { print $1; exit }')
if [ -z "$disk" ]; then say "$m-FAIL: no empty ` + strconv.Itoa(awsImageVolumeGiB) + ` GiB disk next to the root disk"; end; exit 1; fi
say "ichor: writing the image to $disk"
curl -fsSL --retry 3 '` + imageURL + `' | xz -dc | dd of="$disk" bs=4M conv=sparse status=none
sync
say "$m-OK"
end
`
}

// waitStopped follows the helper until it has powered off; one that does not within stopWait is
// stopped, and the run fails. It keeps the data volume's id.
func (r *awsImageRun) waitStopped(ctx context.Context) error {
	deadline := time.Now().Add(r.stopWait)
	last := ""

	for {
		var answer awsInstances

		err := r.client.ec2(ctx, "DescribeInstances", url.Values{"InstanceId.1": {r.instanceID}}, &answer)
		all := answer.all()

		var e *awsError

		switch {
		case err != nil && !awsTransient(err) && (!errors.As(err, &e) || e.code != awsCodeNotFoundYet):
			return err
		case err == nil && len(all) == 1:
			done, err := r.stepState(all[0], &last)
			if done || err != nil {
				return err
			}
		}

		if time.Now().After(deadline) {
			r.emit(awsPhaseWriting, "the helper did not power off: stopping it")
			_ = r.client.ec2(ctx, "StopInstances", url.Values{"InstanceId.1": {r.instanceID}}, nil) //nolint:errcheck // the run fails anyway

			return fmt.Errorf("instance %s did not finish writing the image within %s", r.instanceID, r.stopWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.poll):
		}
	}
}

// stepState reads one answer about the helper: done once it is stopped.
func (r *awsImageRun) stepState(i awsInstance, last *string) (bool, error) {
	switch i.State {
	case awsStateStopped:
		for _, d := range i.Devices {
			if d.Name == awsImageDataDisk {
				r.volumeID = d.VolumeID
			}
		}

		if r.volumeID == "" {
			return true, fmt.Errorf("instance %s has no data volume at %s", r.instanceID, awsImageDataDisk)
		}

		return true, nil
	case awsStatePending, awsStateRunning, awsStateStopping:
		if i.State != *last {
			*last = i.State
			r.emit(awsPhaseWriting, "instance "+r.instanceID+": "+i.State)
		}

		return false, nil
	}

	return true, fmt.Errorf("instance %s is %s: %s", r.instanceID, i.State, cmpOr(i.StateReason, "EC2 gave no reason"))
}

// checkConsole reads the helper's serial console for its marker. The console may lag the stop
// by a few seconds: it is read again, up to awsImageConsoleTries times. No OK marker, no snapshot:
// a volume of zeros would register an AMI that does not boot.
func (r *awsImageRun) checkConsole(ctx context.Context) error {
	var tail []string

	for try := range awsImageConsoleTries {
		var answer struct {
			Output string `xml:"output"`
		}

		if err := r.client.ec2(ctx, "GetConsoleOutput", url.Values{"InstanceId.1": {r.instanceID}}, &answer); err != nil && !awsTransient(err) {
			return err
		}

		data, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(answer.Output)) //nolint:errcheck // an unreadable console has no marker

		ok, failure, lines := awsConsoleVerdict(string(data))

		switch {
		case ok:
			return nil
		case failure != "":
			return fmt.Errorf("the helper could not write the image: %s", failure)
		}

		tail = lines

		if try < awsImageConsoleTries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.poll):
			}
		}
	}

	for _, line := range tail {
		r.emit(awsPhaseWriting, line)
	}

	return fmt.Errorf("the helper's console shows no result, so the image may not be written: %s", cmpOr(strings.Join(tail, " / "), "the console is empty"))
}

// awsConsoleVerdict reads the helper's last marker: ok, or the failure's reason, and the last
// lines of the console when there is none.
func awsConsoleVerdict(console string) (ok bool, failure string, tail []string) {
	lines := strings.Split(strings.ReplaceAll(console, "\r", ""), "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])

		switch {
		case strings.Contains(line, awsImageMarkerOK):
			return true, "", nil
		case strings.Contains(line, awsImageMarkerFail):
			_, reason, _ := strings.Cut(line, awsImageMarkerFail)

			return false, cmpOr(strings.TrimSpace(strings.TrimPrefix(reason, ":")), "no reason given"), nil
		}
	}

	for i := len(lines) - 1; i >= 0 && len(tail) < awsImageTailLines; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			tail = append([]string{line}, tail...)
		}
	}

	return false, "", tail
}

func (r *awsImageRun) imageTags() [][2]string {
	return [][2]string{
		{"Name", r.name}, {awsTagRun, r.runID}, {"os", "talos"}, {awsTagTalosVersion, r.target.version},
		{awsTagArch, r.target.arch}, {awsTagSchematic, r.target.schematic},
	}
}

func (r *awsImageRun) description() string {
	schematic := r.target.schematic

	return fmt.Sprintf("Talos %s %s %s %s", r.target.version, cloudPlatformAWS, r.target.arch, schematic[:min(awsSchematicShown, len(schematic))])
}

// createSnapshot snapshots the data volume and waits for the snapshot to complete.
func (r *awsImageRun) createSnapshot(ctx context.Context) error {
	params := url.Values{"VolumeId": {r.volumeID}, "Description": {r.description()}}
	awsSetTags(params, "TagSpecification.1", "snapshot", r.imageTags())

	var resp struct {
		ID string `xml:"snapshotId"`
	}

	r.snapshotPosted = true

	if err := r.client.ec2(ctx, "CreateSnapshot", params, &resp); err != nil {
		return err
	}

	if r.snapshotID = resp.ID; r.snapshotID == "" {
		return errors.New("EC2 answered without a snapshot id")
	}

	deadline := time.Now().Add(r.snapshotWait)
	last := ""

	for {
		var answer struct {
			Items []struct {
				Status   string `xml:"status"`
				Progress string `xml:"progress"`
				Message  string `xml:"statusMessage"`
			} `xml:"snapshotSet>item"`
		}

		err := r.client.ec2(ctx, "DescribeSnapshots", url.Values{"SnapshotId.1": {r.snapshotID}}, &answer)

		switch {
		case err != nil && !awsTransient(err):
			return err
		case err == nil && len(answer.Items) == 1 && answer.Items[0].Status == awsSnapCompleted:
			return nil
		case err == nil && len(answer.Items) == 1 && answer.Items[0].Status == awsSnapError:
			return fmt.Errorf("snapshot %s failed: %s", r.snapshotID, cmpOr(answer.Items[0].Message, "EC2 gave no reason"))
		case err == nil && len(answer.Items) == 1 && answer.Items[0].Progress != last:
			last = answer.Items[0].Progress
			r.emit(awsPhaseSnapshot, "snapshot "+r.snapshotID+": "+cmpOr(last, "pending"))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("snapshot %s was not complete within %s", r.snapshotID, r.snapshotWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.poll):
		}
	}
}

// register makes the AMI from the snapshot, as the Talos guide registers it.
func (r *awsImageRun) register(ctx context.Context) error {
	awsArch := awsArchX86
	if r.target.arch == bootArchARM64 {
		awsArch = awsArchARM
	}

	params := url.Values{
		"Name": {r.name}, "Description": {r.description() + " schematic " + r.target.schematic},
		"Architecture": {awsArch}, "RootDeviceName": {awsImageRootDisk}, "VirtualizationType": {"hvm"}, "EnaSupport": {"true"},
		"BlockDeviceMapping.1.DeviceName":              {awsImageRootDisk},
		"BlockDeviceMapping.1.Ebs.SnapshotId":          {r.snapshotID},
		"BlockDeviceMapping.1.Ebs.VolumeType":          {"gp3"},
		"BlockDeviceMapping.1.Ebs.DeleteOnTermination": {"true"},
	}

	if r.bootMode != "" {
		params.Set("BootMode", r.bootMode)
	}

	awsSetTags(params, "TagSpecification.1", "image", r.imageTags())

	var resp struct {
		ID string `xml:"imageId"`
	}

	r.registerPosted = true

	if err := r.client.ec2(ctx, "RegisterImage", params, &resp); err != nil {
		return err
	}

	if r.imageID = resp.ID; r.imageID == "" {
		return errors.New("EC2 answered without an AMI id")
	}

	r.result = awsImageResult{
		ImageID: r.imageID, SnapshotID: r.snapshotID, Name: r.name, Arch: r.target.arch,
		TalosVersion: r.target.version, Schematic: r.target.schematic, SizeGB: awsImageVolumeGiB,
	}

	return nil
}

// cleanup removes what this run created and adds the outcome to why: always the helper (its
// volumes go with it), and on a failure an unfinished AMI and snapshot. Its context is its own:
// the run's has often ended. An AMI that is ready stays, with its snapshot.
func (r *awsImageRun) cleanup(why error) error {
	if r.client == nil {
		return why
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsImageCleanupTimeout)
	defer cancel()

	r.findUnanswered(ctx)

	var deleted, left []string

	remove := func(what, action string, params url.Values) {
		if err := r.deleteRetrying(ctx, action, params); err != nil {
			left = append(left, what+" ("+friendlyError(err)+")")
		} else {
			deleted = append(deleted, what)
		}
	}

	if r.instanceID != "" || (why != nil && (r.imageID != "" || r.snapshotID != "")) {
		r.emit(awsPhaseCleanup, "terminating the throwaway instance")
	}

	if why != nil && r.imageID != "" {
		remove("unfinished AMI "+r.imageID, "DeregisterImage", url.Values{"ImageId": {r.imageID}})
	}

	if why != nil && r.snapshotID != "" {
		remove("unfinished snapshot "+r.snapshotID, "DeleteSnapshot", url.Values{"SnapshotId": {r.snapshotID}})
	}

	if r.instanceID != "" {
		remove("instance "+r.instanceID+" (ichor-ami-"+r.runID+")", "TerminateInstances", url.Values{"InstanceId.1": {r.instanceID}})
	}

	return awsCleanupOutcome(why, r.imageID, r.spec.Region, deleted, left)
}

func awsCleanupOutcome(why error, imageID, region string, deleted, left []string) error {
	stillThere := ""
	if len(left) > 0 {
		stillThere = "Could not be deleted, and still billed: " + strings.Join(left, ", ") + " in " + region + ". Delete it in the AWS console"
	}

	switch {
	case why == nil && stillThere == "":
		return nil
	case why == nil:
		return fmt.Errorf("AMI %s is ready. %s", imageID, stillThere)
	case len(deleted) > 0 && stillThere != "":
		return fmt.Errorf("%w. Deleted: %s. %s", why, strings.Join(deleted, ", "), stillThere)
	case len(deleted) > 0:
		return fmt.Errorf("%w. Deleted: %s", why, strings.Join(deleted, ", "))
	case stillThere != "":
		return fmt.Errorf("%w. %s", why, stillThere)
	}

	return why
}

// findUnanswered looks up what a create with no clear answer may have made: the helper by its
// client token, the snapshot and the AMI by the run's tag.
func (r *awsImageRun) findUnanswered(ctx context.Context) {
	tagged := url.Values{"Owner.1": {"self"}, "Filter.1.Name": {"tag:" + awsTagRun}, "Filter.1.Value.1": {r.runID}}

	if r.runPosted && r.instanceID == "" {
		var answer awsInstances

		params := url.Values{"Filter.1.Name": {"client-token"}, "Filter.1.Value.1": {awsClientTokenPrefix + r.runID}}
		if r.client.ec2(ctx, "DescribeInstances", params, &answer) == nil {
			for _, i := range answer.all() {
				if i.State != "terminated" {
					r.instanceID = i.ID
				}
			}
		}
	}

	if r.snapshotPosted && r.snapshotID == "" {
		var answer struct {
			IDs []string `xml:"snapshotSet>item>snapshotId"`
		}

		if r.client.ec2(ctx, "DescribeSnapshots", tagged, &answer) == nil && len(answer.IDs) == 1 {
			r.snapshotID = answer.IDs[0]
		}
	}

	if r.registerPosted && r.imageID == "" {
		var answer struct {
			IDs []string `xml:"imagesSet>item>imageId"`
		}

		if r.client.ec2(ctx, "DescribeImages", tagged, &answer) == nil && len(answer.IDs) == 1 {
			r.imageID = answer.IDs[0]
		}
	}
}

// deleteRetrying sends one delete until it holds: a snapshot still pending, or an AMI still using
// it, is tried again. What is already gone counts as deleted.
func (r *awsImageRun) deleteRetrying(ctx context.Context, action string, params url.Values) error {
	var err error

	for try := range awsImageCleanupTries {
		err = r.client.ec2(ctx, action, params, nil)

		var e *awsError

		switch {
		case err == nil:
			return nil
		case errors.As(err, &e) && strings.HasSuffix(e.code, ".NotFound"):
			return nil
		case !(awsTransient(err) || errors.As(err, &e) && (e.code == "IncorrectState" || e.code == "InvalidSnapshot.InUse")):
			return err
		}

		if try < awsImageCleanupTries-1 {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(r.poll):
			}
		}
	}

	return err
}

type awsImageCost struct {
	InstanceType         string `json:"instanceType"`
	Hourly               string `json:"hourly"`
	Currency             string `json:"currency"`
	SnapshotMonthlyPerGB string `json:"snapshotMonthlyPerGB"`
	Minutes              int    `json:"minutes"`
	Credential           string `json:"credential,omitempty"`
}

// AWSImageEstimate is the cost line of an AMI run: the helper's type and hourly price (it runs
// about ten minutes), and the snapshot's storage price per GB-month, billed until it is deleted.
// A price the Price List does not give is "". Creates nothing. JSON: {"instanceType","hourly",
// "currency","snapshotMonthlyPerGB","minutes"}. region "" for the keys'.
func AWSImageEstimate(credJSON, arch, region string) (out string, err error) {
	defer maskResult(&out, &err)

	arch, err = normalizeBootArch(arch)
	if err != nil {
		return "", err
	}

	if arch == "" {
		return "", errors.New("pick an architecture (amd64, arm64)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsOptionsTimeout)
	defer cancel()

	c, updated, err := newAWSClientOnline(ctx, credJSON)
	if err != nil {
		return "", err
	}

	if region != "" {
		if err := c.setRegion(region); err != nil {
			return "", err
		}
	}

	cost := awsImageCost{InstanceType: awsImageType(arch), Minutes: awsImageMinutes, Credential: updated}
	cost.Hourly = c.pricing(ctx, cost.InstanceType)
	cost.SnapshotMonthlyPerGB = awsProductPrice(c.products(ctx, 10, [2]string{"productFamily", "Storage Snapshot"}, [2]string{"regionCode", c.region}),
		"GB-Mo", func(usage string) bool { return strings.HasSuffix(usage, "EBS:SnapshotUsage") })

	if cost.Hourly != "" || cost.SnapshotMonthlyPerGB != "" {
		cost.Currency = "USD"
	}

	return toJSON(cost)
}
