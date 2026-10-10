package ichorgo

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

const testSchematic = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"

func factoryImage(tag string) string {
	return factoryHost + "/installer/" + testSchematic + ":" + tag
}

func TestCheckExtensions(t *testing.T) {
	installed := []extensionInfo{
		{Name: "iscsi-tools", Version: "v0.2.0"},
		{Name: "util-linux-tools", Version: "2.41"},
		{Name: "schematic", Version: testSchematic},
	}
	official := func(string) ([]string, error) {
		return []string{"siderolabs/util-linux-tools", "siderolabs/iscsi-tools", "siderolabs/zfs"}, nil
	}
	lacking := func(string) ([]string, error) { return []string{"siderolabs/util-linux-tools"}, nil }
	down := func(string) ([]string, error) { return nil, errors.New("could not check extensions: timeout") }

	for _, tt := range []struct {
		name     string
		image    string
		official func(string) ([]string, error)
		missing  []string
		unknown  bool
		err      string
	}{
		{"all present", factoryImage("v1.12.0"), official, []string{}, false, ""},
		{"one missing", factoryImage("v1.12.0"), lacking, []string{"iscsi-tools"}, false, ""},
		{"custom registry", "registry.example.invalid/talos/installer:v1.12.0", official, []string{}, true, ""},
		{"private factory", "factory.example.invalid/installer/" + testSchematic + ":v1.12.0", official, []string{}, true, ""},
		{"factory unreachable", factoryImage("v1.12.0"), down, []string{}, true, "could not check extensions: timeout"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := checkExtensions(installed, tt.image, tt.official)

			if !slices.Equal(got.Missing, tt.missing) || got.Unknown != tt.unknown || got.Error != tt.err {
				t.Fatalf("got %+v", got)
			}

			if len(got.Installed) != 2 {
				t.Errorf("the schematic pseudo-extension must not be checked: %+v", got.Installed)
			}

			if got.TargetVersion != "v1.12.0" {
				t.Errorf("version = %q", got.TargetVersion)
			}
		})
	}

	if got := checkExtensions(installed, factoryImage("v1.12.0"), lacking); got.Schematic != testSchematic {
		t.Errorf("schematic = %q", got.Schematic)
	}

	warnings := extensionWarnings(checkExtensions(installed, factoryImage("v1.12.0"), lacking))
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "extension iscsi-tools has no build for v1.12.0") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestOfficialExtensionsFromTheFactory(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		switch r.URL.Path {
		case "/version/v1.12.0/extensions/official":
			_, _ = io.WriteString(w, `[{"name":"siderolabs/iscsi-tools","ref":"ghcr.io/siderolabs/iscsi-tools:v0.2.0","digest":"sha256:x","author":"Sidero Labs","description":"iSCSI"},
			{"name":"siderolabs/zfs","ref":"ghcr.io/siderolabs/zfs:2.3","digest":"sha256:y","author":"Sidero Labs","description":"ZFS"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	saved := factoryBaseURL
	factoryBaseURL = srv.URL

	t.Cleanup(func() {
		factoryBaseURL = saved
		factoryExtensions.Lock()
		factoryExtensions.byVersion = map[string][]string{}
		factoryExtensions.Unlock()
	})

	names, err := officialExtensions("v1.12.0")
	if err != nil || !slices.Equal(names, []string{"siderolabs/iscsi-tools", "siderolabs/zfs"}) {
		t.Fatalf("%v %v", names, err)
	}

	if _, err := officialExtensions("v1.12.0"); err != nil || calls.Load() != 1 {
		t.Errorf("the list is cached per version: %d calls, %v", calls.Load(), err)
	}

	if _, err := officialExtensions("v9.9.9"); err == nil || !strings.Contains(err.Error(), "could not check extensions") {
		t.Errorf("unknown version = %v", err)
	}
}

func TestUpgradeExtensionCheckDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := UpgradeExtensionCheck(demo, "", demoNodes()[0].Node, "ghcr.io/siderolabs/installer:v1.99.0")
	got := decodeJSON[extensionCheck](t, out, err)

	if !slices.Equal(got.Missing, []string{"iscsi-tools"}) || got.Unknown || got.TargetVersion != "v1.99.0" {
		t.Fatalf("demo = %s", out)
	}
}
