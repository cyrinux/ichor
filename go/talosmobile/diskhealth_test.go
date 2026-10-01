package talosmobile

import (
	"strings"
	"testing"
)

// The fixtures follow SMARTStatusSpec's yaml tags in Talos main (v1.15):
// pkg/machinery/resources/block/smart_status.go.
const smartNVMeYAML = `dev_path: /dev/nvme0n1
dev_type: nvme
healthy: false
message: 'critical warning: 0x5'
power_state: active
temperature: 41
power_on_hours: 12345
power_cycles: 321
percent_used: 7
available_spare: 100
critical_warning: 5
media_errors: 2
`

const smartSATAYAML = `dev_path: /dev/sda
dev_type: sata
healthy: true
temperature: 33
power_on_hours: 40000
power_cycles: 80
attributes:
    - id: 5
      name: Reallocated_Sector_Ct
      current: 100
      worst: 100
      threshold: 10
      raw_value: 0
    - id: 177
      name: Wear_Leveling_Count
      current: 88
      worst: 88
      threshold: 0
      raw_value: 215
    - id: 9
      name: Power_On_Hours
      current: 91
      worst: 91
      threshold: 0
      raw_value: 18446744073709551615
    - id: 197
      name: Current_Pending_Sector
      current: 1
      worst: 1
      threshold: 5
      raw_value: 48
      failing: true
`

func smartSpec(t *testing.T, id, yamlSpec string) map[string]any {
	t.Helper()

	spec, err := resourceSpecMap(untypedResource(t, smartStatusType, id, yamlSpec))
	if err != nil {
		t.Fatal(err)
	}

	return spec
}

func TestBuildDiskHealth(t *testing.T) {
	disks := []diskInfo{
		{Name: "nvme0n1", Model: "Samsung SSD 990", Serial: "S1"},
		{Name: "sda", Model: "WDC WD40", Serial: "S2"},
		{Name: "vda", Model: "QEMU"},
	}

	got := buildDiskHealth(disks, map[string]map[string]any{
		"nvme0n1": smartSpec(t, "nvme0n1", smartNVMeYAML),
		"sda":     smartSpec(t, "sda", smartSATAYAML),
	})

	yes, no := true, false
	i := func(n int64) *int64 { return &n }

	want := diskHealth{Supported: true, Disks: []diskSMART{
		{
			Device: "nvme0n1", Model: "Samsung SSD 990", Serial: "S1", Healthy: &no, Type: "nvme",
			Message: "critical warning: 0x5", PowerState: "active",
			TemperatureC: i(41), PowerOnHours: i(12345), PowerCycles: i(321), WearPercent: i(7),
			CriticalWarnings: []string{"available spare below threshold", "reliability degraded", "2 media errors"},
			Attributes:       []smartAttr{{"available spare", "100"}, {"media errors", "2"}},
		},
		{
			Device: "sda", Model: "WDC WD40", Serial: "S2", Healthy: &yes, Type: "sata",
			TemperatureC: i(33), PowerOnHours: i(40000), PowerCycles: i(80), WearPercent: i(12),
			CriticalWarnings: []string{"Current_Pending_Sector is failing"},
			Attributes: []smartAttr{
				{"Reallocated_Sector_Ct", "100 (worst 100, threshold 10, raw 0)"},
				{"Wear_Leveling_Count", "88 (worst 88, threshold 0, raw 215)"},
				{"Power_On_Hours", "91 (worst 91, threshold 0, raw 18446744073709551615)"},
				{"Current_Pending_Sector", "1 (worst 1, threshold 5, raw 48)"},
			},
		},
		// A disk without SMART data (virtual): listed, health unknown.
		{Device: "vda", Model: "QEMU", CriticalWarnings: []string{}, Attributes: []smartAttr{}},
	}}

	if !equalJSON(t, got, want) {
		t.Error("disk health differs")
	}

	out, err := toJSON(got)
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []string{`"healthy":null`, `"temperatureC":null`, `"wearPercent":null`, `"healthy":false`, `"reason":""`} {
		if !strings.Contains(out, part) {
			t.Errorf("JSON lacks %s: %s", part, out)
		}
	}
}

func TestDiskHealthNotConfiguredOrTooOld(t *testing.T) {
	// The type exists (Talos 1.15+) but nothing is collected: no DiskSMARTConfig document.
	got := buildDiskHealth([]diskInfo{{Name: "sda", Model: "M"}}, nil)
	if !got.Supported || !strings.Contains(got.Reason, "DiskSMARTConfig") || len(got.Disks) != 1 || got.Disks[0].Healthy != nil {
		t.Errorf("got %+v", got)
	}

	// A status for a disk the disk list does not have still shows.
	got = buildDiskHealth(nil, map[string]map[string]any{"sdb": smartSpec(t, "sdb", "dev_type: scsi\nhealthy: true\n")})
	if got.Reason != "" || len(got.Disks) != 1 || got.Disks[0].Device != "sdb" || got.Disks[0].Healthy == nil || !*got.Disks[0].Healthy ||
		got.Disks[0].WearPercent != nil {
		t.Errorf("got %+v", got.Disks)
	}

	if r := unsupportedSMARTReason("v1.14.1"); r != "needs Talos v1.15 or newer (this node runs v1.14.1)" {
		t.Errorf("reason = %q", r)
	}

	if r := unsupportedSMARTReason(""); r != "needs Talos v1.15 or newer" {
		t.Errorf("reason = %q", r)
	}

	// A new NVMe omits percent_used (zero value): 0 % worn, not unknown.
	nvme := mapSMARTStatus("nvme1n1", smartSpec(t, "nvme1n1", "dev_type: nvme\nhealthy: true\n"))
	if nvme.WearPercent == nil || *nvme.WearPercent != 0 || len(nvme.CriticalWarnings) != 0 {
		t.Errorf("nvme = %+v", nvme)
	}
}
