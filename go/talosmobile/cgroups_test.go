package talosmobile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"
)

func cgroupTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o444, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return &buf
}

func TestCgroupTreeFromTarGz(t *testing.T) {
	root, err := cgroupTreeFromTarGz(cgroupTarGz(t, map[string]string{
		"io.pressure":                                  "some avg10=2.50 avg60=1.50 avg300=0.00 total=12\nfull avg10=0.25 avg60=0.50 avg300=0.00 total=6\n",
		"cgroup.procs":                                 "1\n",
		"system/apid/memory.current":                   "38797312\n",
		"system/apid/memory.max":                       "41943040\n",
		"system/apid/memory.high":                      "max\n",
		"system/apid/memory.events":                    "low 0\nhigh 0\nmax 3\noom 1\noom_kill 1\noom_group_kill 0\n",
		"system/apid/cpu.stat":                         "usage_usec 2000000\nuser_usec 1500000\nsystem_usec 500000\nnr_throttled 4\nthrottled_usec 9000\n",
		"system/apid/cpu.max":                          "50000 100000\n",
		"system/apid/cpu.weight":                       "20\n",
		"system/apid/io.stat":                          "8:0 rbytes=100 wbytes=200 rios=1 wios=2 dbytes=0 dios=0\n7:0 rbytes=50 wbytes=0 rios=1 wios=0 dbytes=0 dios=0\n",
		"kubepods/burstable/podabc/memory.current":     "1048576\n",
		"kubepods/burstable/podabc/memory.pressure":    "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
		"kubepods/burstable/podabc/cid1/memory.max":    "max\n",
		"kubepods/burstable/podabc/cid1/garbage.file":  "ignored",
		"kubepods/burstable/podabc/cid1/memory.events": "not a valid line",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if root.Pressure == nil || root.Pressure.IO != (cgroupPSI{Some10: 2.5, Some60: 1.5, Full10: 0.25, Full60: 0.5}) {
		t.Errorf("root io pressure = %+v", root.Pressure)
	}

	apid := root.children["system"].children["apid"]
	if apid.MemCurrent != 38797312 || apid.MemMax != 41943040 || apid.OOMKills != 1 || apid.CPUUsec != 2000000 {
		t.Errorf("apid = %+v", apid)
	}

	if apid.IORead != 150 || apid.IOWrite != 200 {
		t.Errorf("apid io = %d/%d", apid.IORead, apid.IOWrite)
	}

	// A malformed file leaves its fields unset rather than failing the tree.
	if c := root.children["kubepods"].children["burstable"].children["podabc"].children["cid1"]; c == nil || c.MemMax != 0 || c.OOMKills != 0 {
		t.Errorf("container = %+v", c)
	}
}

func TestBuildCgroupReport(t *testing.T) {
	root, err := cgroupTreeFromTarGz(cgroupTarGz(t, map[string]string{
		"cpu.pressure":                                        "some avg10=4.00 avg60=2.00 avg300=0.00 total=1\n",
		"init/memory.current":                                 "100\n",
		"system/apid/memory.events":                           "oom_kill 2\n",
		"system/apid/cpu.pressure":                            "some avg10=1.00 avg60=0.00 avg300=0.00 total=1\n",
		"podruntime/etcd/io.pressure":                         "some avg10=7.00 avg60=0.00 avg300=0.00 total=1\n",
		"podruntime/kubelet/io.pressure":                      "some avg10=3.00 avg60=0.00 avg300=0.00 total=1\n",
		"kubepods/memory.current":                             "950\n",
		"kubepods/memory.max":                                 "1000\n",
		"kubepods/podguaranteed/cpu.pressure":                 "some avg10=9.00 avg60=0.00 avg300=0.00 total=1\n",
		"kubepods/burstable/poduid1/memory.events":            "oom_kill 1\n",
		"kubepods/burstable/poduid1/cid-web/memory.current":   "95\n",
		"kubepods/burstable/poduid1/cid-web/memory.max":       "100\n",
		"kubepods/burstable/poduid1/cid-web/memory.events":    "oom_kill 1\n",
		"kubepods/burstable/poduid1/cid-pause/memory.current": "1\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	names := cgroupNames(map[string]string{}, "uid1", "default/web-0", "cid-web", "web")
	names = cgroupNames(names, "uid1", "default/web-0", "cid-pause", "default/web-0")

	report := buildCgroupReport(root, names, 42)

	if report.At != 42 || report.Pressure.CPU.Some10 != 4 {
		t.Errorf("report = %+v", report)
	}

	kinds := map[string]string{}
	walkCgroups(report.Root, "", func(n *cgroupNode, _ string) {
		kinds[n.Name] = n.Kind

		// The tree stops at pods: containers are the Pods tab's.
		if n.Kind == "pod" && len(n.Children) > 0 {
			t.Errorf("pod %s keeps its containers", n.Name)
		}
	})

	for name, kind := range map[string]string{
		"init": "service", "apid": "service", "etcd": "service", "system": "group", "kubepods": "group",
		"burstable": "group", "podguaranteed": "pod", "default/web-0": "pod", "web": "", "sandbox": "",
	} {
		if kinds[name] != kind {
			t.Errorf("kind of %s = %q, want %q", name, kinds[name], kind)
		}
	}

	// A pod's QoS class is not worth naming next to it; a service's group is.
	want := []cgroupHotspot{
		{Resource: "cpu", Name: "podguaranteed", Parent: "", Some10: 9},
		{Resource: "io", Name: "etcd", Parent: "podruntime", Some10: 7},
	}
	if len(report.Hotspots) != len(want) {
		t.Fatalf("hotspots = %+v", report.Hotspots)
	}

	for i := range want {
		if report.Hotspots[i] != want[i] {
			t.Errorf("hotspot %d = %+v, want %+v", i, report.Hotspots[i], want[i])
		}
	}

	// OOM kills once per workload (pod, not its container too); limits on the container
	// and on kubepods.
	got := map[string]cgroupAlert{}
	for _, a := range report.Alerts {
		got[a.Kind+" "+a.Name] = a
	}

	if len(report.Alerts) != 4 || got["oomKill apid"].Count != 2 || got["oomKill apid"].Parent != "system" ||
		got["oomKill default/web-0"].Count != 1 || got["oomKill default/web-0"].Parent != "" ||
		got["memoryLimit web"].Parent != "default/web-0" || got["memoryLimit kubepods"].Percent != 95 {
		t.Errorf("alerts = %+v", report.Alerts)
	}

	if report.Alerts[0].Kind != "oomKill" {
		t.Errorf("OOM kills must come first: %+v", report.Alerts)
	}

	out, err := toJSON(report)
	if err != nil || !json.Valid([]byte(out)) {
		t.Fatalf("json: %v", err)
	}
}

func TestCgroupNamesDoesNotMutate(t *testing.T) {
	in := map[string]string{"a": "b"}
	out := cgroupNames(in, "u", "ns/p", "c", "ctr")

	if len(in) != 1 || out["podu"] != "ns/p" || out["c"] != "ctr" || out["a"] != "b" {
		t.Errorf("in = %v, out = %v", in, out)
	}
}
