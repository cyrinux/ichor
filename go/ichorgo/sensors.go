package ichorgo

import (
	"context"
	"errors"
	"io"
	"math"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// nodeSensors is what NodeSensors returns: the node's hwmon chips, thermal zones, CPU
// frequencies, thermal throttling counters and PCI devices. A VM has every section empty.
type nodeSensors struct {
	Temperatures []temperatureSensor `json:"temperatures"`
	Fans         []fanSensor         `json:"fans"`
	Voltages     []voltageSensor     `json:"voltages"`
	ThermalZones []thermalZone       `json:"thermalZones"`
	CPUFreq      []cpuFrequency      `json:"cpuFreq"`
	Throttle     *throttleCounts     `json:"throttle"` // null when the CPU has no counters
	// Throttled: a core runs below throttledRatio of its max under the performance governor,
	// or a temperature is above its sensor's max.
	Throttled bool              `json:"throttled"`
	PCI       []pciDevice       `json:"pci"`
	Errors    map[string]string `json:"errors"` // section -> error, for sections that failed
}

type temperatureSensor struct {
	Chip        string  `json:"chip"`  // hwmon "name": coretemp, k10temp, nvme, acpitz...
	Label       string  `json:"label"` // tempN_label, else "tempN"
	Celsius     float64 `json:"celsius"`
	CritCelsius float64 `json:"critCelsius"` // 0 when the sensor has none
	MaxCelsius  float64 `json:"maxCelsius"`  // 0 when the sensor has none
}

type fanSensor struct {
	Chip  string `json:"chip"`
	Label string `json:"label"`
	RPM   int64  `json:"rpm"`
}

type voltageSensor struct {
	Chip  string  `json:"chip"`
	Label string  `json:"label"`
	Volts float64 `json:"volts"`
}

type thermalZone struct {
	Type    string  `json:"type"`
	Celsius float64 `json:"celsius"`
}

type cpuFrequency struct {
	CPU        int    `json:"cpu"`
	CurrentMHz int64  `json:"currentMhz"`
	MinMHz     int64  `json:"minMhz"`
	MaxMHz     int64  `json:"maxMhz"`
	Governor   string `json:"governor"`
}

type throttleCounts struct {
	CoreEvents    int64 `json:"coreEvents"`    // summed over the CPUs read
	PackageEvents int64 `json:"packageEvents"` // the first CPU's package
}

type pciDevice struct {
	ID       string `json:"id"` // bus address, 0000:00:1f.2
	Class    string `json:"class"`
	Subclass string `json:"subclass"`
	Vendor   string `json:"vendor"`
	Product  string `json:"product"`
	Driver   string `json:"driver"`
}

const (
	hwmonRoot   = "/sys/class/hwmon"
	thermalRoot = "/sys/class/thermal"
	cpuRoot     = "/sys/devices/system/cpu"
	// maxSensorReads caps the files read on one node, so an odd node cannot stall the screen.
	maxSensorReads = 200
	// maxSensorBytes caps one sysfs file (they hold a number or a short label).
	maxSensorBytes = 256
	// sensorParallel is how many reads run at once through apid.
	sensorParallel = 8
	// throttledRatio: a core under the performance governor running below this share of
	// its max frequency is throttled.
	throttledRatio = 0.7
)

var (
	hwmonAttr   = regexp.MustCompile(`^(temp|fan|in)(\d+)_(input|label|crit|max)$`)
	numberedDir = regexp.MustCompile(`^(hwmon|thermal_zone|cpu)(\d+)$`)
)

// sensorTree is what the listings found: each hwmon chip's attribute files, the thermal
// zones and the CPUs (directory names).
type sensorTree struct {
	Chips map[string][]string // hwmonN -> file names
	Zones []string            // thermal_zoneN
	CPUs  []string            // cpuN
}

// NodeSensors reads node's hardware sensors (os:reader): hwmon temperatures, fans and
// voltages, thermal zones, per-CPU frequency (CPUFreqStats), thermal throttle counters and
// PCI devices. Each section is best-effort: a failure is recorded in errors; a node without
// the files (a VM) has empty sections and no error.
func NodeSensors(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeSensors", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		errs := map[string]string{}

		tree, listErrs := listSensorTree(ctx, s)
		for section, e := range listErrs {
			errs[section] = s.friendly(node, e)
		}

		contents := readSensorFiles(ctx, s, sensorReads(tree))

		var freqs []*machineapi.CPUFreqStats

		if resp, e := s.client.MachineClient.CPUFreqStats(ctx, &emptypb.Empty{}); e != nil {
			if !isMissingPath(e) {
				errs["cpuFreq"] = s.friendly(node, e)
			}
		} else if msg := first(resp.GetMessages()); msg != nil {
			if m := metaError(msg.GetMetadata()); m != "" && !strings.Contains(m, "no such file") {
				errs["cpuFreq"] = m
			}

			freqs = msg.GetCpuFreqStats()
		}

		out := buildSensors(tree, contents, freqs)

		if pci, e := safe.StateListAll[*hardware.PCIDevice](ctx, s.client.COSI); e != nil {
			errs["pci"] = s.friendly(node, e)
		} else {
			out.PCI = mapPCIDevices(safe.ToSlice(pci, identity))
		}

		for k, v := range errs {
			out.Errors[k] = v
		}

		return toJSON(out)
	})
}

// listSensorTree lists the hwmon chips (and each chip's files), thermal zones and CPUs.
// A missing directory is an empty section; other failures are returned by section.
func listSensorTree(ctx context.Context, s *session) (sensorTree, map[string]error) {
	tree := sensorTree{Chips: map[string][]string{}}
	errs := map[string]error{}

	chips, err := listNames(ctx, s, hwmonRoot)
	if err != nil {
		errs["hwmon"] = err
	}

	zones, err := listNames(ctx, s, thermalRoot)
	if err != nil {
		errs["thermal"] = err
	}

	cpus, err := listNames(ctx, s, cpuRoot)
	if err != nil {
		errs["throttle"] = err
	}

	tree.Zones = numbered(zones, "thermal_zone")
	tree.CPUs = numbered(cpus, "cpu")
	chipDirs := numbered(chips, "hwmon")

	var mu sync.Mutex

	forEachLimit(chipDirs, sensorParallel, func(_ int, chip string) {
		files, err := listNames(ctx, s, hwmonRoot+"/"+chip)

		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			errs["hwmon"] = err

			return
		}

		tree.Chips[chip] = files
	})

	return tree, errs
}

// listNames returns the names of the entries directly below dir (a symlinked directory is
// followed by the node). A missing directory has no entries.
func listNames(ctx context.Context, s *session, dir string) ([]string, error) {
	stream, err := s.client.LS(ctx, &machineapi.ListRequest{Root: dir, Recurse: false})
	if err != nil {
		return missingIsEmpty(err)
	}

	var names []string

	for {
		info, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			return names, nil
		case err != nil:
			return missingIsEmpty(err)
		}

		if e := metaError(info.GetMetadata()); e != "" {
			return missingIsEmpty(errors.New(e))
		}

		if info.GetError() != "" {
			continue
		}

		name := info.GetRelativeName()
		if name == "" {
			name = strings.TrimPrefix(info.GetName(), dir+"/")
		}

		if name == "." || name == "" || strings.Contains(name, "/") || path.Clean(info.GetName()) == path.Clean(dir) {
			continue
		}

		names = append(names, name)
	}
}

func missingIsEmpty(err error) ([]string, error) {
	if isMissingPath(err) {
		return nil, nil
	}

	return nil, err
}

// isMissingPath tells a file or directory the node does not have (a VM without hwmon or
// cpufreq) from a real failure.
func isMissingPath(err error) bool {
	if err == nil {
		return false
	}

	if status.Code(err) == codes.NotFound {
		return true
	}

	return strings.Contains(err.Error(), "no such file or directory")
}

// numbered keeps the names prefixN, ordered by N.
func numbered(names []string, prefix string) []string {
	out := []string{}

	for _, n := range names {
		if m := numberedDir.FindStringSubmatch(n); m != nil && m[1] == prefix {
			out = append(out, n)
		}
	}

	sort.Slice(out, func(i, j int) bool { return dirIndex(out[i]) < dirIndex(out[j]) })

	return out
}

func dirIndex(name string) int {
	m := numberedDir.FindStringSubmatch(name)
	if m == nil {
		return math.MaxInt
	}

	n, _ := strconv.Atoi(m[2])

	return n
}

// sensorReads are the files to read, in priority order (chip names and temperatures,
// then fans and voltages, thermal zones, throttle counters), at most maxSensorReads.
func sensorReads(tree sensorTree) []string {
	var reads []string

	add := func(p string) {
		if len(reads) < maxSensorReads {
			reads = append(reads, p)
		}
	}

	chips := sortedKeys(tree.Chips)

	for _, kind := range []string{"temp", "fan", "in"} {
		for _, chip := range chips {
			dir := hwmonRoot + "/" + chip
			if kind == "temp" {
				add(dir + "/name")
			}

			for _, f := range tree.Chips[chip] {
				if m := hwmonAttr.FindStringSubmatch(f); m != nil && m[1] == kind {
					add(dir + "/" + f)
				}
			}
		}
	}

	for _, z := range tree.Zones {
		add(thermalRoot + "/" + z + "/type")
		add(thermalRoot + "/" + z + "/temp")
	}

	for i, cpu := range tree.CPUs {
		add(cpuRoot + "/" + cpu + "/thermal_throttle/core_throttle_count")

		if i == 0 {
			add(cpuRoot + "/" + cpu + "/thermal_throttle/package_throttle_count")
		}
	}

	return reads
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool { return dirIndex(keys[i]) < dirIndex(keys[j]) })

	return keys
}

// readSensorFiles reads the files, sensorParallel at a time; a file that cannot be read is
// left out (an attribute the driver does not implement, a CPU without throttle counters).
func readSensorFiles(ctx context.Context, s *session, paths []string) map[string]string {
	out := map[string]string{}

	var mu sync.Mutex

	forEachLimit(paths, sensorParallel, func(_ int, p string) {
		r, err := s.client.Read(ctx, p)
		if err != nil {
			return
		}

		defer r.Close() //nolint:errcheck

		b, err := io.ReadAll(io.LimitReader(r, maxSensorBytes))
		if err != nil {
			return
		}

		mu.Lock()
		out[p] = strings.TrimSpace(string(b))
		mu.Unlock()
	})

	return out
}

// buildSensors maps the files read and the CPU frequencies; contents maps a path to its
// trimmed content.
func buildSensors(tree sensorTree, contents map[string]string, freqs []*machineapi.CPUFreqStats) nodeSensors {
	out := nodeSensors{
		Temperatures: []temperatureSensor{}, Fans: []fanSensor{}, Voltages: []voltageSensor{},
		ThermalZones: []thermalZone{}, CPUFreq: []cpuFrequency{}, PCI: []pciDevice{}, Errors: map[string]string{},
	}

	for _, chip := range sortedKeys(tree.Chips) {
		addChip(&out, chip, tree.Chips[chip], contents)
	}

	for _, z := range tree.Zones {
		raw, ok := contents[thermalRoot+"/"+z+"/temp"]
		if milli, err := strconv.ParseInt(raw, 10, 64); ok && err == nil {
			out.ThermalZones = append(out.ThermalZones, thermalZone{Type: contents[thermalRoot+"/"+z+"/type"], Celsius: milliToUnit(milli, 1)})
		}
	}

	out.Throttle = throttleOf(tree, contents)
	out.CPUFreq = mapCPUFreq(freqs)
	out.Throttled = isThrottled(out.CPUFreq, out.Temperatures)

	return out
}

// addChip appends one hwmon chip's temperatures, fans and voltages, by channel number.
func addChip(out *nodeSensors, chip string, files []string, contents map[string]string) {
	dir := hwmonRoot + "/" + chip
	name := contents[dir+"/name"]

	if name == "" {
		name = chip
	}

	channels := map[string][]int{}

	for _, f := range files {
		if m := hwmonAttr.FindStringSubmatch(f); m != nil && m[3] == "input" {
			n, _ := strconv.Atoi(m[2])
			channels[m[1]] = append(channels[m[1]], n)
		}
	}

	for _, kind := range []string{"temp", "fan", "in"} {
		sort.Ints(channels[kind])

		for _, n := range channels[kind] {
			attr := func(a string) (int64, bool) {
				v, err := strconv.ParseInt(contents[dir+"/"+kind+strconv.Itoa(n)+"_"+a], 10, 64)

				return v, err == nil
			}

			input, ok := attr("input")
			if !ok {
				continue
			}

			label := contents[dir+"/"+kind+strconv.Itoa(n)+"_label"]
			if label == "" {
				label = kind + strconv.Itoa(n)
			}

			switch kind {
			case "temp":
				t := temperatureSensor{Chip: name, Label: label, Celsius: milliToUnit(input, 1)}
				if v, ok := attr("crit"); ok {
					t.CritCelsius = milliToUnit(v, 1)
				}

				if v, ok := attr("max"); ok {
					t.MaxCelsius = milliToUnit(v, 1)
				}

				out.Temperatures = append(out.Temperatures, t)
			case "fan":
				out.Fans = append(out.Fans, fanSensor{Chip: name, Label: label, RPM: input})
			default:
				out.Voltages = append(out.Voltages, voltageSensor{Chip: name, Label: label, Volts: milliToUnit(input, 3)})
			}
		}
	}
}

// milliToUnit turns a sysfs milli-value (m°C, mV) into its unit, rounded to digits decimals.
func milliToUnit(milli int64, digits int) float64 {
	scale := math.Pow(10, float64(digits))

	return math.Round(float64(milli)/1000*scale) / scale
}

// throttleOf sums the CPUs' core throttle events; null when no CPU has the counters (AMD,
// most VMs).
func throttleOf(tree sensorTree, contents map[string]string) *throttleCounts {
	var (
		t     throttleCounts
		found bool
	)

	for i, cpu := range tree.CPUs {
		dir := cpuRoot + "/" + cpu + "/thermal_throttle/"
		if v, err := strconv.ParseInt(contents[dir+"core_throttle_count"], 10, 64); err == nil {
			t.CoreEvents += v
			found = true
		}

		if i == 0 {
			if v, err := strconv.ParseInt(contents[dir+"package_throttle_count"], 10, 64); err == nil {
				t.PackageEvents = v
				found = true
			}
		}
	}

	if !found {
		return nil
	}

	return &t
}

// mapCPUFreq maps CPUFreqStats (kHz) to MHz; a CPU without cpufreq reports zeros and is
// left out.
func mapCPUFreq(in []*machineapi.CPUFreqStats) []cpuFrequency {
	out := []cpuFrequency{}

	for i, f := range in {
		if f.GetCurrentFrequency() == 0 && f.GetMaximumFrequency() == 0 {
			continue
		}

		out = append(out, cpuFrequency{
			CPU:        i,
			CurrentMHz: int64(f.GetCurrentFrequency() / 1000),
			MinMHz:     int64(f.GetMinimumFrequency() / 1000),
			MaxMHz:     int64(f.GetMaximumFrequency() / 1000),
			Governor:   f.GetGovernor(),
		})
	}

	return out
}

// isThrottled: a core under the performance governor runs below throttledRatio of its max
// (other governors lower the clock of idle cores on purpose), or a temperature is above its
// sensor's max.
func isThrottled(freqs []cpuFrequency, temps []temperatureSensor) bool {
	for _, f := range freqs {
		if f.Governor == "performance" && f.MaxMHz > 0 && float64(f.CurrentMHz) < throttledRatio*float64(f.MaxMHz) {
			return true
		}
	}

	for _, t := range temps {
		if t.MaxCelsius > 0 && t.Celsius > t.MaxCelsius {
			return true
		}
	}

	return false
}

func mapPCIDevices(in []*hardware.PCIDevice) []pciDevice {
	out := make([]pciDevice, 0, len(in))

	for _, d := range in {
		spec := d.TypedSpec()
		out = append(out, pciDevice{
			ID: d.Metadata().ID(), Class: spec.Class, Subclass: spec.Subclass,
			Vendor: spec.Vendor, Product: spec.Product, Driver: spec.Driver,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}
