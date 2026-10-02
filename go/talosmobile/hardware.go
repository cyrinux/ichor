package talosmobile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

type nodeHardware struct {
	System     *systemInfo       `json:"system"` // null when unavailable
	Processors []processorInfo   `json:"processors"`
	Memory     []memoryModule    `json:"memory"`
	Disks      []diskInfo        `json:"disks"`
	Extensions []extensionInfo   `json:"extensions"`
	Security   *securityInfo     `json:"security"` // null when unavailable
	Errors     map[string]string `json:"errors"`   // section -> error, for sections that failed
}

type systemInfo struct {
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
	Version      string `json:"version"`
	Serial       string `json:"serial"`
	UUID         string `json:"uuid"`
	SKU          string `json:"sku"`
	BIOSVersion  string `json:"biosVersion"`
}

type processorInfo struct {
	Socket       string `json:"socket"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Cores        uint32 `json:"cores"`
	Threads      uint32 `json:"threads"`
	MaxSpeedMHz  uint32 `json:"maxSpeedMhz"`
	BootSpeedMHz uint32 `json:"bootSpeedMhz"`
}

type memoryModule struct {
	Slot         string `json:"slot"` // device locator, e.g. DIMM 0
	Bank         string `json:"bank"`
	SizeMiB      uint32 `json:"sizeMib"`
	Type         string `json:"type"` // product name (the SMBIOS memory type is not exposed)
	SpeedMTs     uint32 `json:"speed"`
	Manufacturer string `json:"manufacturer"`
	Serial       string `json:"serial"`
}

type diskInfo struct {
	Name       string `json:"name"`    // sda, nvme0n1...
	DevPath    string `json:"devPath"` // /dev/sda
	Model      string `json:"model"`
	Serial     string `json:"serial"`
	Size       uint64 `json:"size"` // bytes
	Type       string `json:"type"` // ssd | hdd | nvme | sd | unknown
	WWID       string `json:"wwid"`
	BusPath    string `json:"busPath"`
	SystemDisk bool   `json:"systemDisk"`
	Readonly   bool   `json:"readonly"`
}

type extensionInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
}

type securityInfo struct {
	SecureBoot               bool   `json:"secureBoot"`
	BootedWithUKI            bool   `json:"bootedWithUki"`
	UKISigningKeyFingerprint string `json:"ukiSigningKeyFingerprint"`
	PCRSigningKeyFingerprint string `json:"pcrSigningKeyFingerprint"`
	SELinuxState             string `json:"selinuxState"`
	FIPSState                string `json:"fipsState"`
	ModuleSignatureEnforced  bool   `json:"moduleSignatureEnforced"`
}

const hardwareSections = 6

// NodeHardware returns node's SMBIOS system/CPU/memory info, disks, system extensions
// and security state (os:reader). Each section is best-effort: a failure leaves it empty
// and is recorded in errors; only a total failure is an error.
func NodeHardware(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeHardware", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		nodeCtx := withNode(ctx, node)
		st := s.client.COSI
		out := nodeHardware{
			Processors: []processorInfo{}, Memory: []memoryModule{}, Disks: []diskInfo{},
			Extensions: []extensionInfo{}, Errors: map[string]string{},
		}

		if si, err := safe.StateGetByID[*hardware.SystemInformation](nodeCtx, st, hardware.SystemInformationID); err != nil {
			out.Errors["system"] = s.friendly(node, err)
		} else {
			out.System = mapSystemInfo(si.TypedSpec())
		}

		if cpus, err := safe.StateListAll[*hardware.Processor](nodeCtx, st); err != nil {
			out.Errors["processors"] = s.friendly(node, err)
		} else {
			out.Processors = mapProcessors(safe.ToSlice(cpus, identity))
		}

		if mem, err := safe.StateListAll[*hardware.MemoryModule](nodeCtx, st); err != nil {
			out.Errors["memory"] = s.friendly(node, err)
		} else {
			out.Memory = mapMemoryModules(safe.ToSlice(mem, identity))
		}

		if disks, err := s.client.Disks(nodeCtx); err == nil {
			out.Disks = mapAPIDisks(first(disks.GetMessages()).GetDisks())
		} else if cosiDisks, cosiErr := safe.StateListAll[*block.Disk](nodeCtx, st); cosiErr == nil {
			// Newer Talos versions dropped the Disks API in favor of the block.Disk resource.
			out.Disks = mapBlockDisks(safe.ToSlice(cosiDisks, identity))
		} else {
			out.Errors["disks"] = s.friendly(node, err)
		}

		if exts, err := safe.StateListAll[*runtime.ExtensionStatus](nodeCtx, st); err != nil {
			out.Errors["extensions"] = s.friendly(node, err)
		} else {
			out.Extensions = mapExtensions(safe.ToSlice(exts, identity))
		}

		if sec, err := safe.StateGetByID[*runtime.SecurityState](nodeCtx, st, runtime.SecurityStateID); err != nil {
			out.Errors["security"] = s.friendly(node, err)
		} else {
			out.Security = mapSecurity(sec.TypedSpec())
		}

		if len(out.Errors) == hardwareSections {
			return "", fmt.Errorf("node %s: %s", node, out.Errors["system"])
		}

		return toJSON(out)
	})
}

func mapSystemInfo(spec *hardware.SystemInformationSpec) *systemInfo {
	return &systemInfo{
		Manufacturer: spec.Manufacturer,
		Product:      spec.ProductName,
		Version:      spec.Version,
		Serial:       spec.SerialNumber,
		UUID:         spec.UUID,
		SKU:          spec.SKUNumber,
		BIOSVersion:  spec.BIOSVersion,
	}
}

func mapProcessors(in []*hardware.Processor) []processorInfo {
	out := make([]processorInfo, 0, len(in))

	for _, p := range in {
		spec := p.TypedSpec()

		// Empty sockets show up as entries without a product name or cores.
		if spec.ProductName == "" && spec.CoreCount == 0 {
			continue
		}

		out = append(out, processorInfo{
			Socket:       spec.Socket,
			Manufacturer: spec.Manufacturer,
			Model:        strings.TrimSpace(spec.ProductName),
			Cores:        spec.CoreCount,
			Threads:      spec.ThreadCount,
			MaxSpeedMHz:  spec.MaxSpeed,
			BootSpeedMHz: spec.BootSpeed,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Socket < out[j].Socket })

	return out
}

func mapMemoryModules(in []*hardware.MemoryModule) []memoryModule {
	out := make([]memoryModule, 0, len(in))

	for _, m := range in {
		spec := m.TypedSpec()

		// Empty slots are reported with a zero size.
		if spec.Size == 0 {
			continue
		}

		out = append(out, memoryModule{
			Slot:         spec.DeviceLocator,
			Bank:         spec.BankLocator,
			SizeMiB:      spec.Size,
			Type:         spec.ProductName,
			SpeedMTs:     spec.Speed,
			Manufacturer: spec.Manufacturer,
			Serial:       spec.SerialNumber,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })

	return out
}

// mapAPIDisks maps the Disks API answer, skipping loop/zram/device-mapper devices which
// are not physical disks.
func mapAPIDisks(in []*storage.Disk) []diskInfo {
	out := make([]diskInfo, 0, len(in))

	for _, d := range in {
		// DeviceName is a path ("/dev/sda") on current Talos, a bare name on older ones.
		name := strings.TrimPrefix(d.GetDeviceName(), "/dev/")
		if isPseudoDisk(name) {
			continue
		}

		out = append(out, diskInfo{
			Name:       name,
			DevPath:    "/dev/" + name,
			Model:      d.GetModel(),
			Serial:     d.GetSerial(),
			Size:       d.GetSize(),
			Type:       strings.ToLower(d.GetType().String()),
			WWID:       d.GetWwid(),
			BusPath:    d.GetBusPath(),
			SystemDisk: d.GetSystemDisk(),
			Readonly:   d.GetReadonly(),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// mapBlockDisks maps block.Disk resources, skipping CD-ROMs and loop/zram/device-mapper
// devices which are not physical disks.
func mapBlockDisks(in []*block.Disk) []diskInfo {
	out := make([]diskInfo, 0, len(in))

	for _, d := range in {
		spec := d.TypedSpec()
		name := d.Metadata().ID()

		if spec.CDROM || spec.Size == 0 || isPseudoDisk(name) {
			continue
		}

		devPath := spec.DevPath
		if devPath == "" {
			devPath = "/dev/" + name
		}

		out = append(out, diskInfo{
			Name:     name,
			DevPath:  devPath,
			Model:    spec.Model,
			Serial:   spec.Serial,
			Size:     spec.Size,
			Type:     blockDiskType(spec),
			WWID:     spec.WWID,
			BusPath:  spec.BusPath,
			Readonly: spec.Readonly,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

func blockDiskType(spec *block.DiskSpec) string {
	switch {
	case spec.Transport == "nvme":
		return "nvme"
	case spec.Transport == "mmc":
		return "sd"
	case spec.Rotational:
		return "hdd"
	case spec.Transport == "":
		return "unknown"
	default:
		return "ssd"
	}
}

func isPseudoDisk(name string) bool {
	for _, p := range []string{"loop", "zram", "dm-", "ram", "nbd"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return false
}

func mapExtensions(in []*runtime.ExtensionStatus) []extensionInfo {
	out := make([]extensionInfo, 0, len(in))

	for _, e := range in {
		md := e.TypedSpec().Metadata

		out = append(out, extensionInfo{
			Name:        md.Name,
			Version:     md.Version,
			Author:      md.Author,
			Description: md.Description,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

func mapSecurity(spec *runtime.SecurityStateSpec) *securityInfo {
	return &securityInfo{
		SecureBoot:               spec.SecureBoot,
		BootedWithUKI:            spec.BootedWithUKI,
		UKISigningKeyFingerprint: spec.UKISigningKeyFingerprint,
		PCRSigningKeyFingerprint: spec.PCRSigningKeyFingerprint,
		SELinuxState:             spec.SELinuxState.String(),
		FIPSState:                spec.FIPSState.String(),
		ModuleSignatureEnforced:  spec.ModuleSignatureEnforced,
	}
}
