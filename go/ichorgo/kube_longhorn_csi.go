package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// The CSI VolumeSnapshots kept as Longhorn snapshots. Longhorn's CSI driver names what it
// keeps snap://<volume>/<snapshot> (encodeSnapshotID in longhorn-manager's
// csi/controller_server.go; bak:// and the older bs:// for a backup, bi:// for a backing
// image): deleting such a Longhorn snapshot leaves its VolumeSnapshot looking ready with
// nothing to restore from.

const (
	lhCSIDriver         = "driver.longhorn.io"
	lhCSISnapshotPrefix = "snap://"
	csiContentsPath     = "/apis/" + groupSnapshot + "/v1/volumesnapshotcontents"
)

type csiSnapshotContentObject struct {
	Spec struct {
		Driver string `json:"driver"`
		Ref    struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"volumeSnapshotRef"`
		Source struct {
			SnapshotHandle string `json:"snapshotHandle"` // a pre-provisioned one
		} `json:"source"`
	} `json:"spec"`
	Status struct {
		SnapshotHandle string `json:"snapshotHandle"`
	} `json:"status"`
}

// lhCSIOtherPrefixes are the Longhorn handles that keep no snapshot.
var lhCSIOtherPrefixes = []string{"bak://", "bs://", "bi://"}

// longhornCSIRefs maps "volume/snapshot" to the VolumeSnapshot ("namespace/name") each
// Longhorn snapshot backs. Nil when a Longhorn handle has a shape not known here: which
// snapshots are kept is then unknown, and the sheet warns rather than wrongly saying none.
func longhornCSIRefs(contents []csiSnapshotContentObject) map[string]string {
	refs := map[string]string{}

	for _, c := range contents {
		handle := c.Status.SnapshotHandle
		if handle == "" {
			handle = c.Spec.Source.SnapshotHandle
		}

		if c.Spec.Driver != lhCSIDriver || handle == "" {
			continue // not Longhorn's, or not taken yet
		}

		key, ok := strings.CutPrefix(handle, lhCSISnapshotPrefix)
		if !ok {
			if slices.ContainsFunc(lhCSIOtherPrefixes, func(p string) bool { return strings.HasPrefix(handle, p) }) {
				continue
			}

			return nil
		}

		refs[key] = c.Spec.Ref.Namespace + "/" + c.Spec.Ref.Name
	}

	return refs
}

// readLonghornCSIRefs: nil when the VolumeSnapshotContents cannot be read (no CSI snapshots
// installed, or the account may not list them), so it is not known which snapshots are kept.
func readLonghornCSIRefs(ctx context.Context, k *kubeClient) map[string]string {
	var contents kubeList[csiSnapshotContentObject]
	if err := getList(ctx, k, csiContentsPath, &contents); err != nil {
		return nil
	}

	return longhornCSIRefs(contents.Items)
}

// errLonghornCSISnapshot refuses deleting the Longhorn snapshot a VolumeSnapshot keeps.
func errLonghornCSISnapshot(snapshot, ref string) error {
	return fmt.Errorf("snapshot %s keeps the VolumeSnapshot %s: delete that VolumeSnapshot instead", snapshot, ref)
}
