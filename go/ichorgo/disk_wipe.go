package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	storageapi "github.com/siderolabs/talos/pkg/machinery/api/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	diskWipeFast   = "fast"
	diskWipeZeroes = "zeroes"

	// diskWipeZeroesTimeout bounds a wipe that writes zeroes over the whole disk: hours on a
	// large one, longer than a phone should wait, so only the probe offers it.
	diskWipeZeroesTimeout = 12 * time.Hour
)

var errDiskWipeUnsupported = errors.New("this node's Talos cannot wipe one disk (needs " + featureMinVersion("diskWipe") + ")")

// NodeDiskWipe wipes one disk of node like `talosctl wipe disk DEVICE --method M` (os:admin),
// the node staying up. device is a disk name as NodeDisks lists it (sdb, nvme1n1); method is
// "fast" (the default when empty: the partition table and filesystem signatures) or "zeroes"
// (the whole disk, hours on a large one: the apps offer only fast). Refused before the call:
// a node not in the context, a device that is not one of the node's disks, the system disk, a
// read-only disk, and a disk with a partition that holds a Talos volume or is mounted. Talos
// itself then refuses a disk a volume uses or one that backs another device.
func NodeDiskWipe(configYAML, contextName, node, device, method string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)
	device = strings.TrimPrefix(strings.TrimSpace(device), "/dev/")

	defer recordAction(&err, configYAML, contextName, auditAction{
		Action: "disk-wipe", Node: node, Params: fmt.Sprintf("device=%s,method=%s", device, cmpOr(method, diskWipeFast)),
	})

	wipeMethod, timeout, err := parseDiskWipeMethod(method)
	if err != nil {
		return err
	}

	_, err = withNodeSession(configYAML, contextName, node, timeout, func(ctx context.Context, s *session) (struct{}, error) {
		// The disks are read in the same call: a kernel name can change across reboots.
		list, err := readNodeDisks(ctx, s, node)
		if err != nil {
			return struct{}{}, err
		}

		if !list.Supported {
			return struct{}{}, errDiskWipeUnsupported
		}

		if err := wipeableDisk(list.Disks, device); err != nil {
			return struct{}{}, err
		}

		err = s.client.BlockDeviceWipe(ctx, &storageapi.BlockDeviceWipeRequest{
			Devices: []*storageapi.BlockDeviceWipeDescriptor{{Device: device, Method: wipeMethod}},
		})

		switch {
		case status.Code(err) == codes.Unimplemented:
			return struct{}{}, errDiskWipeUnsupported
		case err != nil:
			return struct{}{}, s.friendlyErr(node, err)
		}

		return struct{}{}, nil
	})

	return err
}

func parseDiskWipeMethod(method string) (storageapi.BlockDeviceWipeDescriptor_Method, time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", diskWipeFast:
		return storageapi.BlockDeviceWipeDescriptor_FAST, callTimeout, nil
	case diskWipeZeroes:
		return storageapi.BlockDeviceWipeDescriptor_ZEROES, diskWipeZeroesTimeout, nil
	default:
		return 0, 0, fmt.Errorf("unknown wipe method %q (fast, zeroes)", method)
	}
}

// wipeableDisk refuses device unless it is one of disks that may be wiped alone: not the
// system disk, not read-only, no partition holding a Talos volume or mounted.
func wipeableDisk(disks []diskLayout, device string) error {
	if device == "" {
		return errors.New("no disk named")
	}

	i := slices.IndexFunc(disks, func(d diskLayout) bool { return d.Name == device })
	if i < 0 {
		return fmt.Errorf("%s is not one of this node's disks", device)
	}

	d := disks[i]

	switch {
	case d.SystemDisk:
		return fmt.Errorf("%s is the system disk: only a node reset wipes it", device)
	case d.Readonly:
		return fmt.Errorf("%s is read-only", device)
	}

	for _, p := range d.Partitions {
		if p.VolumeID != "" {
			return fmt.Errorf("this disk holds the volume %s: remove the volume first", p.VolumeID)
		}

		if p.MountedOn != "" {
			return fmt.Errorf("%s is mounted on %s: unmount it first", p.DevPath, p.MountedOn)
		}
	}

	return nil
}
