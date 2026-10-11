package ichorgo

import (
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

func TestClusterSensorHealth(t *testing.T) {
	const metal, vm, down = "192.0.2.81", "192.0.2.82", "192.0.2.84"

	f := newFakeTalos()
	sensorsNode(t, f, metal)
	f.addNode(t, vm, "v1.14.0", machine.TypeWorker)
	f.mu.Lock()
	f.files[vm] = map[string]string{"/proc/version": "Linux"}
	f.cpuFreq[vm] = nil
	f.mu.Unlock()

	cfg := f.start(t, metal, vm, down)

	out, err := ClusterSensorHealth(cfg, "fake")
	got := decodeJSON[clusterSensors](t, out, err)

	if len(got.Nodes) != 3 {
		t.Fatalf("nodes = %+v", got.Nodes)
	}

	byNode := map[string]nodeSensorHealth{}
	for _, n := range got.Nodes {
		byNode[n.Node] = n
	}

	// The fixture's hottest sensor is the CPU package, with its crit and max.
	if h := byNode[metal].Hottest; h == nil || h.Label != "Package id 0" || h.Celsius != 61.3 || h.CritCelsius != 100 || h.MaxCelsius != 85 {
		t.Errorf("metal hottest = %+v", h)
	}

	if n := byNode[metal]; n.Hostname != "host-192-0-2-81" || n.Error != "" {
		t.Errorf("metal = %+v", n)
	}

	// A VM has no sensor: nothing to report, and no error.
	if n := byNode[vm]; n.Hottest != nil || n.Error != "" || n.Throttled {
		t.Errorf("vm = %+v", n)
	}

	if n := byNode[down]; n.Error == "" || n.Hottest != nil {
		t.Errorf("down = %+v", n)
	}
}

func TestClusterSensorHealthDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := ClusterSensorHealth(cfg, "")
	got := decodeJSON[clusterSensors](t, out, err)

	hot := 0

	for _, n := range got.Nodes {
		if n.Hottest == nil {
			t.Fatalf("demo node without sensors: %+v", n)
		}

		if n.Throttled {
			hot++

			if n.Hostname != "demo-worker-1" || n.Hottest.Celsius < n.Hottest.MaxCelsius {
				t.Errorf("hot node = %+v", n)
			}
		}
	}

	if hot != 1 {
		t.Errorf("throttled nodes = %d", hot)
	}
}

func TestHottest(t *testing.T) {
	if hottest(nil) != nil {
		t.Error("no sensor: nil")
	}

	temps := []temperatureSensor{{Label: "a", Celsius: 40}, {Label: "b", Celsius: 72}, {Label: "c", Celsius: 55}}
	if h := hottest(temps); h == nil || h.Label != "b" {
		t.Errorf("hottest = %+v", h)
	}
}
