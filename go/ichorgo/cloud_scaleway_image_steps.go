package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The steps of StartScalewayImage after the helper is created, and its cleanup.
//
// The verdict: Scaleway has no API for an Instance's serial console output (the console is a
// websocket in the web console), but an Instance may write its own user data through the
// metadata service, from a privileged source port (the SDK's MetadataAPI.SetUserData: PATCH
// http://169.254.42.42/user_data/<key>, text/plain, local port < 1024). The script writes OK or
// FAIL: <reason> under scwImageVerdictKey, and the run reads it back with GET
// /servers/{id}/user_data/{key} (the SDK's GetServerUserData), before any snapshot.

const (
	scwImageVerdictKey  = "ichor-image"
	scwImageVerdictOK   = "OK"
	scwImageVerdictFail = "FAIL"
	scwMetadataEndpoint = "http://169.254.42.42"

	scwSnapshotAvailable = "available"
	scwSnapshotError     = "error"
	scwImageAvailable    = "available"
	scwImageError        = "error"
)

// scwImageScript is the helper's cloud-init user data. It writes the image to the one disk that
// is not the root's and has the data volume's size (never a guessed device name), leaves its
// verdict in its own user data, and powers off either way. An OS shutdown leaves a Scaleway
// Instance "running" for the API: the run powers it off itself once the verdict is there.
func scwImageScript(imageURL string) string {
	return `#!/bin/bash
# Ichor: write a Talos image to the empty data volume, then power off.
set -euo pipefail
verdict() { for i in 1 2 3 4 5; do curl -fsS --local-port 1-1023 -X PATCH -H 'Content-Type: text/plain' --data-binary "$1" '` +
		scwMetadataEndpoint + `/user_data/` + scwImageVerdictKey + `' && return 0; sleep 3; done; return 1; }
say() { for t in /dev/ttyS0 /dev/console; do echo "$*" > "$t" 2>/dev/null || true; done; }
end() { sync; shutdown -h now; }
trap 'rc=$?; verdict "` + scwImageVerdictFail + `: line $LINENO exited $rc" || true; end' ERR
root=$(lsblk -no PKNAME "$(findmnt -no SOURCE /)")
disk=$(lsblk -dbnpo NAME,SIZE,TYPE | awk -v root="/dev/$root" -v size=` + strconv.FormatInt(scwImageVolumeGB*scwGB, 10) + ` '$3 == "disk" && $2 == size && $1 != root { print $1; exit }')
if [ -z "$disk" ]; then verdict "` + scwImageVerdictFail + `: no empty ` + strconv.Itoa(scwImageVolumeGB) + ` GB disk next to the root disk" || true; end; exit 1; fi
command -v zstd >/dev/null || { apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq zstd; }
say "ichor: writing the image to $disk"
curl -fsSL --retry 3 '` + imageURL + `' | zstd -dc | dd of="$disk" bs=4M conv=fsync status=none
sync
verdict ` + scwImageVerdictOK + `
end
`
}

// scwImageVerdict reads the helper's verdict: ok, or the failure's reason ("" while there is
// none).
func scwImageVerdict(text string) (ok bool, failure string) {
	text = strings.TrimSpace(text)

	switch {
	case text == scwImageVerdictOK:
		return true, ""
	case strings.HasPrefix(text, scwImageVerdictFail):
		return false, cmpOr(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, scwImageVerdictFail), ":")), "no reason given")
	case text != "":
		return false, "an unknown answer: " + text
	}

	return false, ""
}

// waitWritten follows the helper until its verdict is there; one that leaves none within
// stopWait fails the run (cleanup deletes it). It keeps the data volume's id.
func (r *scwImageRun) waitWritten(ctx context.Context) error {
	deadline := time.Now().Add(r.stopWait)
	last := ""

	for {
		done, err := r.readVerdict(ctx)
		if done || err != nil {
			return err
		}

		var resp struct {
			Server scwServer `json:"server"`
		}

		err = r.client.do(ctx, http.MethodGet, r.helper.serverPath(""), nil, &resp)
		s := resp.Server

		switch {
		case err != nil && !r.helper.transient(err):
			return err
		case err == nil && s.State == scwStateLocked:
			return errors.New("scaleway locked the helper Instance: see the console")
		case err == nil && s.State != last:
			last = s.State
			r.emit(hetznerPhaseWriting, "Instance "+r.helper.serverID+": "+s.State)
		}

		if err == nil {
			r.volumeID = s.Volumes["1"].ID
		}

		if err == nil && (s.State == scwStateStopped || s.State == scwStateStoppedPlace) {
			if done, err := r.readVerdict(ctx); done || err != nil {
				return err
			}

			return fmt.Errorf("instance %s powered off without writing its result, so the image may not be written", r.helper.serverID)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("instance %s did not finish writing the image within %s", r.helper.serverID, r.stopWait)
		}

		if err := scwWait(ctx, r.poll); err != nil {
			return err
		}
	}
}

// readVerdict reads the helper's verdict once: done with OK, an error with FAIL, neither while
// the key does not exist yet.
func (r *scwImageRun) readVerdict(ctx context.Context) (bool, error) {
	var text []byte

	err := r.client.do(ctx, http.MethodGet, r.helper.serverPath("/user_data/"+scwImageVerdictKey), nil, &text)

	switch {
	case scwStatus(err) == http.StatusNotFound:
		return false, nil
	case err != nil && r.helper.transient(err):
		return false, nil
	case err != nil:
		return false, err
	}

	ok, failure := scwImageVerdict(string(text))

	switch {
	case ok:
		return true, nil
	case failure != "":
		return false, fmt.Errorf("the helper could not write the image: %s", failure)
	}

	return false, nil
}

// powerOff stops the helper (its OS has halted, but the API still shows it running) and waits
// until it is stopped, so that the snapshot reads a volume nothing writes to.
func (r *scwImageRun) powerOff(ctx context.Context) error {
	r.emit(hetznerPhaseWriting, "the image is written: powering Instance "+r.helper.serverID+" off")

	asked := false

	for {
		var resp struct {
			Server scwServer `json:"server"`
		}

		err := r.client.do(ctx, http.MethodGet, r.helper.serverPath(""), nil, &resp)
		s := resp.Server

		switch {
		case err != nil && !r.helper.transient(err):
			return err
		case err != nil:
		case s.State == scwStateStopped || s.State == scwStateStoppedPlace:
			r.volumeID = s.Volumes["1"].ID
			if r.volumeID == "" {
				return fmt.Errorf("instance %s has no data volume", r.helper.serverID)
			}

			return nil
		case s.State == scwStateLocked:
			return errors.New("scaleway locked the helper Instance: see the console")
		case s.State == scwStateRunning && !asked:
			if err := r.client.do(ctx, http.MethodPost, r.helper.serverPath("/action"), map[string]string{"action": "poweroff"}, nil); err != nil {
				return err
			}

			asked = true
		}

		if err := scwWait(ctx, r.poll); err != nil {
			return err
		}
	}
}

// blockPath is a Block Storage API path in the client's zone.
func (c *scwClient) blockPath(rest string) string {
	return "/block/v1/zones/" + c.zone + rest
}

func (r *scwImageRun) imageTags() []string {
	schematic := r.target.schematic

	return []string{
		"talos", "ichor-managed", scwTagRun + r.runID, "schematic=" + schematic[:min(scwSchematicShown, len(schematic))],
		"version=" + r.target.version, "arch=" + r.target.arch,
	}
}

// createSnapshot snapshots the data volume through the Block Storage API and waits for it.
func (r *scwImageRun) createSnapshot(ctx context.Context) error {
	body := map[string]any{"volume_id": r.volumeID, "name": r.name, "project_id": r.client.project, "tags": r.imageTags()}

	var resp struct {
		ID string `json:"id"`
	}

	r.snapshotPosted = true

	if err := r.client.do(ctx, http.MethodPost, r.client.blockPath("/snapshots"), body, &resp); err != nil {
		return err
	}

	if r.snapshotID = resp.ID; r.snapshotID == "" {
		return errors.New("scaleway answered without a snapshot id")
	}

	return r.waitState(ctx, "snapshot "+r.snapshotID, r.client.blockPath("/snapshots/"+url.PathEscape(r.snapshotID)), r.snapshotWait,
		func(data []byte) (string, error) {
			var s struct {
				Status string `json:"status"`
			}

			return s.Status, json.Unmarshal(data, &s)
		}, scwSnapshotAvailable, scwSnapshotError, hetznerPhaseSnapshot)
}

// register creates the image from the snapshot and waits until it is available.
func (r *scwImageRun) register(ctx context.Context) error {
	body := map[string]any{
		"name": r.name, "root_volume": r.snapshotID, "arch": scwArch(r.target.arch), "project": r.client.project,
		"tags": r.imageTags(), "public": false,
	}

	var resp struct {
		Image scwAPIImage `json:"image"`
	}

	r.imagePosted = true

	if err := r.client.do(ctx, http.MethodPost, r.client.path("/images"), body, &resp); err != nil {
		return err
	}

	if r.imageID = resp.Image.ID; r.imageID == "" {
		return errors.New("scaleway answered without an image id")
	}

	err := r.waitState(ctx, "image "+r.imageID, r.client.path("/images/"+url.PathEscape(r.imageID)), r.snapshotWait,
		func(data []byte) (string, error) {
			var i struct {
				Image scwAPIImage `json:"image"`
			}

			return i.Image.State, json.Unmarshal(data, &i)
		}, scwImageAvailable, scwImageError, scwPhaseRegister)
	if err != nil {
		return err
	}

	r.result = scwImageResult{
		ImageID: r.imageID, SnapshotID: r.snapshotID, Name: r.name, Arch: r.target.arch,
		TalosVersion: r.target.version, Schematic: r.target.schematic, SizeGB: scwImageVolumeGB,
	}

	return nil
}

// waitState polls path until state reads ready; failed, or the wait running out, fails the run.
func (r *scwImageRun) waitState(ctx context.Context, what, path string, wait time.Duration, state func([]byte) (string, error), ready, failed, phase string) error {
	deadline := time.Now().Add(wait)
	last := ""

	for {
		var data []byte

		err := r.client.do(ctx, http.MethodGet, path, nil, &data)
		if err == nil {
			var s string
			if s, err = state(data); err == nil {
				switch {
				case s == ready:
					return nil
				case s == failed:
					return fmt.Errorf("%s failed at Scaleway", what)
				case s != last:
					last = s
					r.emit(phase, what+": "+s)
				}
			}
		}

		if err != nil && !r.helper.transient(err) {
			return err
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s was not ready within %s", what, wait)
		}

		if err := scwWait(ctx, r.poll); err != nil {
			return err
		}
	}
}

// cleanup removes what this run created and adds the outcome to why: always the helper with its
// volumes and IP, and on a failure an unfinished image and snapshot. Its context is its own: the
// run's has often ended. An image that is ready stays, with its snapshot.
func (r *scwImageRun) cleanup(why error) error {
	if r.helper == nil {
		return why
	}

	ctx, cancel := context.WithTimeout(context.Background(), scwImageCleanupTimeout)
	defer cancel()

	r.findUnanswered(ctx)

	var deleted, left []string

	remove := func(what, path string) {
		if err := r.helper.deleteGone(ctx, path); err != nil {
			left = append(left, what+" ("+friendlyError(err)+")")
		} else {
			deleted = append(deleted, what)
		}
	}

	r.emit(cloudPhaseCleanup, "deleting the throwaway Instance, its volumes and its IP")

	if why != nil && r.imageID != "" {
		remove("unfinished image "+r.imageID, r.client.path("/images/"+url.PathEscape(r.imageID)))
	}

	if why != nil && r.snapshotID != "" {
		remove("unfinished snapshot "+r.snapshotID, r.client.blockPath("/snapshots/"+url.PathEscape(r.snapshotID)))
	}

	made := r.helper.serverID != "" || r.helper.ipID != ""

	switch err := r.helper.deleteMachine(ctx); {
	case err != nil:
		left = append(left, err.Error())
	case made:
		deleted = append(deleted, r.helperWords())
	}

	return scwImageCleanupOutcome(why, r.imageID, deleted, left)
}

// helperWords names what the helper's delete removed.
func (r *scwImageRun) helperWords() string {
	switch {
	case r.helper.serverID == "":
		return "flexible IP " + r.helper.ipID
	case r.helper.ipID == "":
		return "Instance " + r.helper.serverID + " (" + r.helper.spec.Hostname + ") with its volumes"
	}

	return "Instance " + r.helper.serverID + " (" + r.helper.spec.Hostname + ") with its volumes and flexible IP"
}

func scwImageCleanupOutcome(why error, imageID string, deleted, left []string) error {
	stillThere := ""
	if len(left) > 0 {
		stillThere = "Could not be deleted, and still billed: " + strings.Join(left, ", ") + ". Delete it in the Scaleway console"
	}

	return cloudCleanupOutcome(why, "image "+imageID+" is ready", stillThere, deleted)
}

// findUnanswered looks up what a create with no clear answer may have made, by the run's tag:
// the helper and its IP (the create's lookup), the snapshot and the image. A POST is never sent
// again.
func (r *scwImageRun) findUnanswered(ctx context.Context) {
	r.helper.lookup(ctx)

	tag := "tags=" + url.QueryEscape(scwTagRun+r.runID)

	if r.snapshotPosted && r.snapshotID == "" {
		var resp struct {
			Snapshots []struct {
				ID string `json:"id"`
			} `json:"snapshots"`
		}

		path := r.client.blockPath("/snapshots?" + tag + "&project_id=" + url.QueryEscape(r.client.project))
		if r.client.do(ctx, http.MethodGet, path, nil, &resp) == nil && len(resp.Snapshots) == 1 {
			r.snapshotID = resp.Snapshots[0].ID
		}
	}

	if r.imagePosted && r.imageID == "" {
		var resp struct {
			Images []scwAPIImage `json:"images"`
		}

		if r.client.do(ctx, http.MethodGet, r.client.path("/images?"+tag+"&"+r.client.projectQuery()), nil, &resp) == nil && len(resp.Images) == 1 {
			r.imageID = resp.Images[0].ID
		}
	}
}
