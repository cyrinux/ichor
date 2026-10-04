package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

// smartStatusType is Talos 1.15's SMARTStatus resource (pkg/machinery/resources/block/
// smart_status.go on main), one per disk, keyed like block.Disk ("sda", "nvme0n1"), in the
// block namespace. This client's machinery (1.14) has no Go type for it, so it is read
// untyped and its YAML spec decoded by key:
//
//	dev_path, dev_type (nvme|sata|scsi), healthy, message, power_state, temperature (°C),
//	power_on_hours, power_cycles, percent_used, available_spare, critical_warning,
//	media_errors (NVMe), attributes[{id,name,current,worst,threshold,raw_value,failing}] (ATA)
//
// Collection runs only while the machine config has a DiskSMARTConfig document
// (apiVersion v1alpha1, kind DiskSMARTConfig, optional interval): configs generated for 1.15
// include it, upgraded clusters must add it.
const smartStatusType = "SMARTStatuses.block.talos.dev"

const (
	reasonSMARTNeedsVersion = "needs Talos v1.15 or newer"
	reasonSMARTNotEnabled   = "SMART collection is not configured on this node: add a DiskSMARTConfig document to its machine config"
)

type diskHealth struct {
	Supported bool        `json:"supported"`
	Reason    string      `json:"reason"`
	Disks     []diskSMART `json:"disks"`
}

type diskSMART struct {
	Device           string      `json:"device"` // sda, nvme0n1...
	Model            string      `json:"model"`
	Serial           string      `json:"serial"`
	Healthy          *bool       `json:"healthy"` // null: no SMART data for this disk
	Type             string      `json:"type"`    // nvme | sata | scsi | ""
	Message          string      `json:"message"`
	PowerState       string      `json:"powerState"`
	TemperatureC     *int64      `json:"temperatureC"` // null when not reported
	PowerOnHours     *int64      `json:"powerOnHours"`
	PowerCycles      *int64      `json:"powerCycles"`
	WearPercent      *int64      `json:"wearPercent"` // life used, 0-100 (may exceed 100 on NVMe)
	CriticalWarnings []string    `json:"criticalWarnings"`
	Attributes       []smartAttr `json:"attributes"`
}

type smartAttr struct {
	K string `json:"k"`
	V string `json:"v"`
}

// NodeDiskHealth returns the SMART health of node's disks from Talos's SMARTStatus resources,
// like `talosctl get smart` (os:reader). supported is false with a reason on Talos older
// than 1.15; when SMART collection is not configured, supported is true, reason says so and
// the disks are listed without health (healthy null).
func NodeDiskHealth(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeDiskHealth", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		known, err := s.hasResourceType(ctx, node, smartStatusType)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		if !known {
			return toJSON(diskHealth{Reason: unsupportedSMARTReason(s.nodeVersion(ctx, node)), Disks: []diskSMART{}})
		}

		list, err := s.client.COSI.List(ctx, resource.NewMetadata(block.NamespaceName, smartStatusType, "", resource.VersionUndefined))
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		statuses := make(map[string]map[string]any, len(list.Items))

		for _, r := range list.Items {
			spec, err := resourceSpecMap(r)
			if err != nil {
				return "", fmt.Errorf("SMART status of %s: %w", r.Metadata().ID(), err)
			}

			statuses[r.Metadata().ID()] = spec
		}

		// Model and serial come from the disk resources; without them the SMART data still shows.
		var disks []diskInfo

		if cosiDisks, err := safe.StateListAll[*block.Disk](ctx, s.client.COSI); err == nil {
			disks = mapBlockDisks(safe.ToSlice(cosiDisks, identity))
		}

		return toJSON(buildDiskHealth(disks, statuses))
	})
}

func unsupportedSMARTReason(version string) string {
	if version == "" {
		return reasonSMARTNeedsVersion
	}

	return reasonSMARTNeedsVersion + " (this node runs " + version + ")"
}

// buildDiskHealth joins the physical disks with their SMART status (by device name).
func buildDiskHealth(disks []diskInfo, statuses map[string]map[string]any) diskHealth {
	out := diskHealth{Supported: true, Disks: make([]diskSMART, 0, len(disks))}

	seen := map[string]bool{}

	for _, d := range disks {
		seen[d.Name] = true

		entry := mapSMARTStatus(d.Name, statuses[d.Name])
		entry.Model, entry.Serial = d.Model, d.Serial
		out.Disks = append(out.Disks, entry)
	}

	for name, spec := range statuses {
		if !seen[name] {
			out.Disks = append(out.Disks, mapSMARTStatus(name, spec))
		}
	}

	sort.Slice(out.Disks, func(i, j int) bool { return out.Disks[i].Device < out.Disks[j].Device })

	if len(statuses) == 0 {
		out.Reason = reasonSMARTNotEnabled
	}

	return out
}

// nvmeCriticalWarnings are the bits of the NVMe SMART log's critical warning byte.
var nvmeCriticalWarnings = []string{
	"available spare below threshold",
	"temperature outside thresholds",
	"reliability degraded",
	"media is read-only",
	"volatile memory backup failed",
	"persistent memory region unreliable",
}

// mapSMARTStatus maps a SMARTStatus spec (nil: no SMART data for the disk).
func mapSMARTStatus(device string, spec map[string]any) diskSMART {
	out := diskSMART{Device: device, CriticalWarnings: []string{}, Attributes: []smartAttr{}}

	if spec == nil {
		return out
	}

	out.Type = specString(spec, "dev_type")
	out.Message = specString(spec, "message")
	out.PowerState = specString(spec, "power_state")
	out.TemperatureC = specInt(spec, "temperature")
	out.PowerOnHours = specInt(spec, "power_on_hours")
	out.PowerCycles = specInt(spec, "power_cycles")

	if healthy, ok := spec["healthy"].(bool); ok {
		out.Healthy = &healthy
	}

	if out.Type == "nvme" {
		out.WearPercent = specInt(spec, "percent_used")
		if out.WearPercent == nil {
			// The spec omits zero values: a healthy new NVMe reports 0% used.
			zero := int64(0)
			out.WearPercent = &zero
		}
	}

	if w := specInt(spec, "critical_warning"); w != nil {
		for bit, text := range nvmeCriticalWarnings {
			if *w&(1<<bit) != 0 {
				out.CriticalWarnings = append(out.CriticalWarnings, text)
			}
		}
	}

	for _, key := range []string{"available_spare", "media_errors"} {
		if v := specInt(spec, key); v != nil {
			out.Attributes = append(out.Attributes, smartAttr{K: strings.ReplaceAll(key, "_", " "), V: fmt.Sprint(*v)})

			if key == "media_errors" && *v > 0 {
				out.CriticalWarnings = append(out.CriticalWarnings, fmt.Sprintf("%d media errors", *v))
			}
		}
	}

	attrs, _ := spec["attributes"].([]any) //nolint:errcheck

	for _, raw := range attrs {
		a, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		name := specString(a, "name")
		if name == "" {
			name = "attribute " + specText(a, "id")
		}

		out.Attributes = append(out.Attributes, smartAttr{
			K: name,
			V: fmt.Sprintf("%s (worst %s, threshold %s, raw %s)",
				specText(a, "current"), specText(a, "worst"), specText(a, "threshold"), specText(a, "raw_value")),
		})

		if failing, _ := a["failing"].(bool); failing { //nolint:errcheck
			out.CriticalWarnings = append(out.CriticalWarnings, name+" is failing")
		}

		if out.WearPercent == nil {
			out.WearPercent = ataWear(a)
		}
	}

	return out
}

// ataLifeLeftAttributes are the ATA attributes whose normalized value is the remaining life
// in percent (100 = new): SSD_Life_Left, Wear_Leveling_Count, Media_Wearout_Indicator,
// Percent_Lifetime_Remain.
var ataLifeLeftAttributes = []int64{231, 177, 233, 202}

func ataWear(attr map[string]any) *int64 {
	id, current := specInt(attr, "id"), specInt(attr, "current")
	if id == nil || current == nil || *current > 100 {
		return nil
	}

	for _, known := range ataLifeLeftAttributes {
		if *id == known {
			wear := 100 - *current

			return &wear
		}
	}

	return nil
}

func specString(spec map[string]any, key string) string {
	s, _ := spec[key].(string) //nolint:errcheck

	return s
}

// specInt reads a YAML number (int, uint64 or float, depending on its size).
func specInt(spec map[string]any, key string) *int64 {
	var n int64

	switch v := spec[key].(type) {
	case int:
		n = int64(v)
	case int64:
		n = v
	case uint64:
		n = int64(v) //nolint:gosec
	case float64:
		n = int64(v)
	default:
		return nil
	}

	return &n
}

func specText(spec map[string]any, key string) string {
	if v, ok := spec[key]; ok {
		return fmt.Sprint(v)
	}

	return "0"
}
