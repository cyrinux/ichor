package ichorgo

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
)

// fixtureFiles loads testdata/hwmon as a node's file tree rooted at /.
func fixtureFiles(t *testing.T) map[string]string {
	t.Helper()

	root := filepath.Join("testdata", "hwmon")
	files := map[string]string{}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, p)
		files["/"+filepath.ToSlash(rel)] = string(b)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return files
}

func TestSensorsMilliToUnit(t *testing.T) {
	for _, tc := range []struct {
		milli  int64
		digits int
		want   float64
	}{
		{61250, 1, 61.3},
		{27800, 1, 27.8},
		{-5000, 1, -5},
		{912, 3, 0.912},
		{12096, 3, 12.096},
	} {
		if got := milliToUnit(tc.milli, tc.digits); got != tc.want {
			t.Errorf("milliToUnit(%d, %d) = %v, want %v", tc.milli, tc.digits, got, tc.want)
		}
	}
}

func TestSensorsThrottled(t *testing.T) {
	cool := []temperatureSensor{{Celsius: 60, MaxCelsius: 85}, {Celsius: 99}}
	hot := []temperatureSensor{{Celsius: 86, MaxCelsius: 85}}

	for _, tc := range []struct {
		name  string
		freqs []cpuFrequency
		temps []temperatureSensor
		want  bool
	}{
		{"nothing", nil, nil, false},
		{"cool and fast", []cpuFrequency{{CurrentMHz: 3000, MaxMHz: 3400, Governor: "performance"}}, cool, false},
		{"slow under performance", []cpuFrequency{{CurrentMHz: 3000, MaxMHz: 3400, Governor: "performance"}, {CurrentMHz: 2000, MaxMHz: 3400, Governor: "performance"}}, cool, true},
		{"exactly 70 %", []cpuFrequency{{CurrentMHz: 2380, MaxMHz: 3400, Governor: "performance"}}, nil, false},
		{"idle under schedutil", []cpuFrequency{{CurrentMHz: 800, MaxMHz: 3400, Governor: "schedutil"}}, nil, false},
		{"idle under powersave", []cpuFrequency{{CurrentMHz: 800, MaxMHz: 3400, Governor: "powersave"}}, nil, false},
		{"no max frequency", []cpuFrequency{{CurrentMHz: 800, Governor: "performance"}}, nil, false},
		{"above a sensor's max", nil, hot, true},
	} {
		if got := isThrottled(tc.freqs, tc.temps); got != tc.want {
			t.Errorf("%s: throttled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSensorsReadsCapped(t *testing.T) {
	tree := sensorTree{Chips: map[string][]string{"hwmon0": {"name"}}}
	for i := range 300 {
		tree.CPUs = append(tree.CPUs, "cpu"+strconv.Itoa(i))
	}

	reads := sensorReads(tree)
	if len(reads) != maxSensorReads || reads[0] != "/sys/class/hwmon/hwmon0/name" {
		t.Errorf("reads = %d, first %q", len(reads), reads[0])
	}
}

func TestSensorsNumbered(t *testing.T) {
	got := numbered([]string{"hwmon10", "hwmon2", "cpufreq", "cpu1", "hwmon0", "uevent"}, "hwmon")
	if !slices.Equal(got, []string{"hwmon0", "hwmon2", "hwmon10"}) {
		t.Errorf("numbered = %v", got)
	}
}

// sensorsNode is a bare-metal node: the fixture tree, two CPUs under the performance
// governor (one held at 2 GHz) and two PCI devices.
func sensorsNode(t *testing.T, f *fakeTalos, node string) {
	t.Helper()

	f.addNode(t, node, "v1.14.0", machine.TypeWorker)

	f.mu.Lock()
	if f.files == nil {
		f.files = map[string]map[string]string{}
	}
	f.files[node] = fixtureFiles(t)
	if f.cpuFreq == nil {
		f.cpuFreq = map[string][]*machineapi.CPUFreqStats{}
	}
	f.cpuFreq[node] = []*machineapi.CPUFreqStats{
		{CurrentFrequency: 3_300_000, MinimumFrequency: 800_000, MaximumFrequency: 3_400_000, Governor: "performance"},
		{CurrentFrequency: 2_000_000, MinimumFrequency: 800_000, MaximumFrequency: 3_400_000, Governor: "performance"},
	}
	f.mu.Unlock()

	nic := hardware.NewPCIDeviceInfo("0000:00:1f.6")
	nic.TypedSpec().Class, nic.TypedSpec().Vendor, nic.TypedSpec().Product, nic.TypedSpec().Driver = "Network controller", "Intel Corporation", "Ethernet Connection", "e1000e"
	bridge := hardware.NewPCIDeviceInfo("0000:00:00.0")
	bridge.TypedSpec().Class, bridge.TypedSpec().Subclass = "Bridge", "Host bridge"

	f.put(node, nic, bridge)
}

func TestNodeSensorsFake(t *testing.T) {
	const metal, vm = "192.0.2.81", "192.0.2.82"

	f := newFakeTalos()
	sensorsNode(t, f, metal)
	// A VM: no hwmon, thermal or CPU directories, no PCI resources.
	f.addNode(t, vm, "v1.14.0", machine.TypeWorker)
	f.mu.Lock()
	f.files[vm] = map[string]string{"/proc/version": "Linux"}
	f.cpuFreq[vm] = nil // no cpufreq: an empty answer
	f.mu.Unlock()

	cfg := f.start(t, metal, vm)

	out, err := NodeSensors(cfg, "fake", metal)
	got := decodeJSON[nodeSensors](t, out, err)

	wantTemps := []temperatureSensor{
		{Chip: "acpitz", Label: "temp1", Celsius: 27.8, CritCelsius: 119},
		{Chip: "coretemp", Label: "Package id 0", Celsius: 61.3, CritCelsius: 100, MaxCelsius: 85},
		{Chip: "coretemp", Label: "temp2", Celsius: 58},
	}
	if !slices.Equal(got.Temperatures, wantTemps) {
		t.Errorf("temperatures = %+v", got.Temperatures)
	}

	if want := []fanSensor{{Chip: "nct6775", Label: "CPU fan", RPM: 1180}, {Chip: "nct6775", Label: "fan2", RPM: 0}}; !slices.Equal(got.Fans, want) {
		t.Errorf("fans = %+v", got.Fans)
	}

	if want := []voltageSensor{{Chip: "nct6775", Label: "Vcore", Volts: 0.912}, {Chip: "nct6775", Label: "in1", Volts: 12.096}}; !slices.Equal(got.Voltages, want) {
		t.Errorf("voltages = %+v", got.Voltages)
	}

	if want := []thermalZone{{Type: "x86_pkg_temp", Celsius: 61}}; !slices.Equal(got.ThermalZones, want) {
		t.Errorf("thermal zones = %+v", got.ThermalZones)
	}

	wantFreq := []cpuFrequency{
		{CPU: 0, CurrentMHz: 3300, MinMHz: 800, MaxMHz: 3400, Governor: "performance"},
		{CPU: 1, CurrentMHz: 2000, MinMHz: 800, MaxMHz: 3400, Governor: "performance"},
	}
	if !slices.Equal(got.CPUFreq, wantFreq) || !got.Throttled {
		t.Errorf("cpu freq = %+v, throttled = %v", got.CPUFreq, got.Throttled)
	}

	if got.Throttle == nil || *got.Throttle != (throttleCounts{CoreEvents: 15, PackageEvents: 5}) {
		t.Errorf("throttle = %+v", got.Throttle)
	}

	if len(got.PCI) != 2 || got.PCI[0].ID != "0000:00:00.0" || got.PCI[1].Driver != "e1000e" || len(got.Errors) != 0 {
		t.Errorf("pci = %+v, errors = %v", got.PCI, got.Errors)
	}

	// The VM: every section empty, no error, no throttle counters.
	out, err = NodeSensors(cfg, "fake", vm)
	got = decodeJSON[nodeSensors](t, out, err)

	if len(got.Temperatures)+len(got.Fans)+len(got.Voltages)+len(got.ThermalZones)+len(got.PCI) != 0 || got.Throttle != nil || got.Throttled || len(got.Errors) != 0 {
		t.Errorf("vm = %s", out)
	}

	// Empty lists encode as [] for the apps.
	if !strings.Contains(out, `"temperatures":[]`) || !strings.Contains(out, `"throttle":null`) {
		t.Errorf("vm json = %s", out)
	}
}

func TestNodeSensorsCPUFreqUnavailable(t *testing.T) {
	const node = "192.0.2.83"

	f := newFakeTalos()
	sensorsNode(t, f, node)
	delete(f.cpuFreq, node) // a Talos without CPUFreqStats

	cfg := f.start(t, node)

	out, err := NodeSensors(cfg, "fake", node)
	got := decodeJSON[nodeSensors](t, out, err)

	if got.Errors["cpuFreq"] == "" || len(got.CPUFreq) != 0 || len(got.Temperatures) != 3 || got.Throttled {
		t.Errorf("sensors = %s", out)
	}
}

func TestNodeSensorsDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	var throttled []string

	for _, n := range demoNodes() {
		out, err := NodeSensors(yaml, "", n.Node)
		got := decodeJSON[nodeSensors](t, out, err)

		if len(got.Temperatures) == 0 || len(got.CPUFreq) != n.CPUCount || len(got.PCI) == 0 {
			t.Errorf("demo %s = %s", n.Hostname, out)
		}

		if got.Throttled {
			throttled = append(throttled, n.Hostname)
		}
	}

	if !slices.Equal(throttled, []string{"demo-worker-1"}) {
		t.Errorf("throttled = %v", throttled)
	}
}
