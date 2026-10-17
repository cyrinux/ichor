package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const (
	engineOld = "longhornio/longhorn-engine:v1.12.0"
	engineNew = "longhornio/longhorn-engine:v1.13.0"
	engineAlt = "longhornio/longhorn-engine:v1.12.1"

	engineNS    = "/apis/longhorn.io/v1beta2/namespaces/storage"
	engineProxy = "POST /api/v1/namespaces/storage/services/longhorn-backend:9500/proxy/v1/volumes/vol"

	engineVolume   = `{"metadata":{"name":"vol","namespace":"storage"},"spec":{"image":"` + engineOld + `","dataEngine":"v1","frontend":"blockdev","dataLocality":"disabled"},"status":{"state":"attached","robustness":"healthy","currentNodeID":"node-1","currentImage":"` + engineOld + `"}}`
	engineReplicas = `{"items":[{"metadata":{"name":"vol-r1","namespace":"storage"},"spec":{"volumeName":"vol","nodeID":"node-1"}},{"metadata":{"name":"vol-r2","namespace":"storage"},"spec":{"volumeName":"vol","nodeID":"node-2"}},{"metadata":{"name":"other-r1","namespace":"storage"},"spec":{"volumeName":"other","nodeID":"node-3"}}]}`
)

func engineImageJSON(image, version string, nodes string) string {
	return `{"metadata":{"name":"ei-` + version + `","namespace":"storage"},"spec":{"image":"` + image + `"},"status":{"state":"deployed","refCount":1,"version":"` + version + `","nodeDeploymentMap":{` + nodes + `}}}`
}

var engineImages = `{"items":[` +
	engineImageJSON(engineOld, "v1.12.0", `"node-1":true,"node-2":true,"node-3":true`) + `,` +
	engineImageJSON(engineNew, "v1.13.0", `"node-1":true,"node-2":true,"node-3":true`) + `,` +
	engineImageJSON(engineAlt, "v1.12.1", `"node-1":true,"node-2":true,"node-3":true`) + `]}`

func engineAPI(t *testing.T, volume, images, limit string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                                 `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET " + engineNS + "/volumes/vol":                          volume,
		"GET " + engineNS + "/replicas":                             engineReplicas,
		"GET " + engineNS + "/engineimages":                         images,
		"GET " + engineNS + "/settings/" + lhSettingDefaultEngine:   `{"metadata":{"name":"default-engine-image"},"value":"` + engineNew + `"}`,
		"GET " + engineNS + "/settings/" + lhSettingAutoUpgradeNode: `{"metadata":{"name":"x"},"value":"` + limit + `"}`,
		engineProxy: `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func TestLonghornEngineUpgradeRules(t *testing.T) {
	detached := strings.NewReplacer(`"state":"attached"`, `"state":"detached"`, `"currentNodeID":"node-1"`, `"currentNodeID":""`).Replace(engineVolume)
	notOnReplica := `{"items":[` + engineImageJSON(engineOld, "v1.12.0", `"node-1":true,"node-2":true`) + `,` +
		engineImageJSON(engineNew, "v1.13.0", `"node-1":true,"node-2":false`) + `]}`

	for _, tc := range []struct {
		name, volume, images, limit, image string
		live, can                          bool
		reason                             string // in the preview's reason and the refusal
	}{
		{name: "healthy attached: live", volume: engineVolume, image: engineNew, live: true, can: true},
		{name: "detached: offline", volume: detached, image: engineNew, can: true},
		{name: "detached degraded: offline", volume: strings.Replace(detached, "healthy", "degraded", 1), image: engineNew, can: true},
		{name: "degraded attached", volume: strings.Replace(engineVolume, "healthy", "degraded", 1), image: engineNew, live: true, reason: "healthy volume"},
		{name: "not deployed on a replica node", volume: engineVolume, images: notOnReplica, image: engineNew, live: true, reason: "not deployed"},
		{name: "v2 data engine", volume: strings.Replace(engineVolume, `"dataEngine":"v1"`, `"dataEngine":"v2"`, 1), image: engineNew, live: true, reason: "V2"},
		{name: "migrating", volume: strings.Replace(engineVolume, `"dataEngine"`, `"migrationNodeID":"node-2","dataEngine"`, 1), image: engineNew, live: true, reason: "migrating"},
		{name: "strict-local attached", volume: strings.Replace(engineVolume, `"disabled"`, `"strict-local"`, 1), image: engineNew, live: true, reason: "strict-local"},
		{name: "strict-local detached", volume: strings.Replace(detached, `"disabled"`, `"strict-local"`, 1), image: engineNew, can: true},
		{name: "iscsi attached", volume: strings.Replace(engineVolume, `"blockdev"`, `"iscsi"`, 1), image: engineNew, live: true, reason: "iSCSI"},
		{name: "upgrade in flight", volume: strings.Replace(engineVolume, `"image":"`+engineOld, `"image":"`+engineAlt, 1), image: engineNew, live: true, reason: "already in progress"},
		{name: "auto upgrade limit, non-default image", volume: engineVolume, limit: "1", image: engineAlt, live: true, can: true, reason: "Automatic engine upgrade"},
		{name: "auto upgrade limit, default image", volume: engineVolume, limit: "1", image: engineNew, live: true, can: true},
		{name: "image from the phone unknown", volume: engineVolume, image: "evil/engine:latest", live: true, can: true, reason: "not one of Longhorn's"},
		{name: "current image", volume: engineVolume, image: engineOld, live: true, can: true, reason: "already runs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := engineAPI(t, tc.volume, cmpOr(tc.images, engineImages), cmpOr(tc.limit, "0"))

			p, err := readLonghornEnginePreview(context.Background(), k, "storage", "vol")
			if err != nil || p.Error != "" {
				t.Fatalf("preview: %v %+v", err, p)
			}

			previewRefused := tc.reason != "" && !tc.can
			if p.Live != tc.live || p.CanUpgrade != tc.can || (previewRefused && !strings.Contains(p.Reason, tc.reason)) || (tc.can && p.Reason != "") {
				t.Fatalf("preview %+v", p)
			}

			live := !tc.live
			err = upgradeLonghornEngine(context.Background(), k, "storage", "vol", tc.image, &live)

			if live != tc.live {
				t.Errorf("live = %v", live)
			}

			posted := lastRequest(f).method == "POST"

			if tc.reason != "" {
				if err == nil || !strings.Contains(err.Error(), tc.reason) || posted {
					t.Fatalf("refusal: %v posted=%v", err, posted)
				}

				return
			}

			r := lastRequest(f)
			if err != nil || !posted || r.query != "action=engineUpgrade" || r.body != `{"image":"`+tc.image+`"}` {
				t.Fatalf("%v %+v", err, r)
			}
		})
	}
}

func TestLonghornEnginePreviewImages(t *testing.T) {
	_, k := engineAPI(t, engineVolume, engineImages, "0")

	p, err := readLonghornEnginePreview(context.Background(), k, "storage", "vol")
	if err != nil {
		t.Fatal(err)
	}

	// The current image is not offered; the default comes first.
	if len(p.Images) != 2 || p.Images[0].Image != engineNew || !p.Images[0].Default || !p.Images[0].Deployed ||
		p.Images[0].Version != "v1.13.0" || p.Images[1].Image != engineAlt || p.Images[1].Default {
		t.Fatalf("images %+v", p.Images)
	}

	if p.DefaultImage != engineNew || p.CurrentImage != engineOld || p.SpecImage != engineOld || p.State != "attached" || p.Robustness != "healthy" {
		t.Fatalf("preview %+v", p)
	}

	// Automatic engine upgrade on: only the default image is offered.
	_, k = engineAPI(t, engineVolume, engineImages, "2")
	if p, _ = readLonghornEnginePreview(context.Background(), k, "storage", "vol"); len(p.Images) != 1 || p.Images[0].Image != engineNew {
		t.Fatalf("auto upgrade images %+v", p.Images)
	}

	// The EngineImages cannot be read: the preview says so and refuses.
	f, k := engineAPI(t, engineVolume, engineImages, "0")
	f.answerWith("GET "+engineNS+"/engineimages", 403, `{"kind":"Status","message":"forbidden"}`)

	if p, err = readLonghornEnginePreview(context.Background(), k, "storage", "vol"); err != nil || p.CanUpgrade || p.Error == "" {
		t.Fatalf("forbidden images: %v %+v", err, p)
	}

	// Without the limit setting (an older Longhorn) the limit is 0.
	f, k = engineAPI(t, engineVolume, engineImages, "0")
	f.answerWith("GET "+engineNS+"/settings/"+lhSettingAutoUpgradeNode, 404, `{"kind":"Status"}`)

	if p, err = readLonghornEnginePreview(context.Background(), k, "storage", "vol"); err != nil || !p.CanUpgrade || len(p.Images) != 2 {
		t.Fatalf("no limit setting: %v %+v", err, p)
	}
}

func TestLonghornEngineManagerAnswerSurfaces(t *testing.T) {
	f, k := engineAPI(t, engineVolume, engineImages, "0")
	f.answerWith(engineProxy, 400, `{"kind":"Status","message":"cannot upgrade engine"}`)

	live := false
	if err := upgradeLonghornEngine(context.Background(), k, "storage", "vol", engineNew, &live); kubeCode(err) != 400 {
		t.Fatalf("%v", err)
	}

	f.answerWith("GET /apis", 200, `{"groups":[]}`)

	if err := upgradeLonghornEngine(context.Background(), k, "storage", "vol", engineNew, &live); !isNotFound(err) {
		t.Fatalf("missing Longhorn: %v", err)
	}
}

func TestLonghornEngineUpgradeArgsAndDemo(t *testing.T) {
	noKubeDial(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, image := range []string{"", "a b", strings.Repeat("x", lhMaxImageLen+1)} {
		if err := KubeLonghornEngineUpgrade(cfg, "Demo cluster", "", "longhorn-system", "pvc-1c07", image); err == nil || err == errDemoUnavailable {
			t.Errorf("image %q: %v", image, err)
		}
	}

	if err := KubeLonghornEngineUpgrade(cfg, "Demo cluster", "", "../x", "pvc-1c07", engineNew); err == nil {
		t.Error("bad namespace accepted")
	}

	if err := KubeLonghornEngineUpgrade(cfg, "Demo cluster", "", "longhorn-system", "pvc-1c07", engineNew); err != errDemoUnavailable {
		t.Fatalf("demo: %v", err)
	}

	out, err := KubeLonghornEngineUpgradePreview(cfg, "Demo cluster", "", "longhorn-system", "pvc-1c07")
	if err != nil {
		t.Fatal(err)
	}

	var p longhornEngineUpgradePreview
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if !p.CanUpgrade || !p.Live || p.CurrentImage != engineOld || p.DefaultImage != engineNew || len(p.Images) != 1 || !p.Images[0].Default || !p.Images[0].Deployed {
		t.Fatalf("demo preview %s", out)
	}

	// Exactly one demo volume lags the default engine.
	outdated := 0

	for _, v := range demoDataServices(fixtureNow).Longhorn.Volumes {
		if v.EngineOutdated {
			outdated++
		}
	}

	if outdated != 1 {
		t.Errorf("outdated demo volumes: %d", outdated)
	}

	if out, _ := KubeLonghornEngineUpgradePreview(cfg, "Demo cluster", "", "longhorn-system", "pvc-none"); !strings.Contains(out, "not found") {
		t.Errorf("missing demo volume: %s", out)
	}
}

func TestLonghornEngineUpgradePrivacyAndAudit(t *testing.T) {
	withDataDir(t)
	SetPrivacyMask(true, "alice")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	const ns = "alice-storage"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/longhorn.io/v1beta2/namespaces/" + ns + "/volumes/vol":                        strings.ReplaceAll(engineVolume, `"storage"`, `"`+ns+`"`),
		"GET /apis/longhorn.io/v1beta2/namespaces/" + ns + "/replicas":                           strings.ReplaceAll(engineReplicas, `"storage"`, `"`+ns+`"`),
		"GET /apis/longhorn.io/v1beta2/namespaces/" + ns + "/engineimages":                       engineImages,
		"GET /apis/longhorn.io/v1beta2/namespaces/" + ns + "/settings/" + lhSettingDefaultEngine: `{"value":"` + engineNew + `"}`,
		"POST /api/v1/namespaces/" + ns + "/services/longhorn-backend:9500/proxy/v1/volumes/vol": `{}`,
	})
	cfg := f.kubeconfigFor(f.URL)

	privacy.learnNamespaces([]string{ns})

	fake := strings.Trim(privacy.mask(`"`+ns+`"`), `"`)
	if fake == ns {
		t.Fatalf("not masked: %s", fake)
	}

	out, err := KubeLonghornEngineUpgradePreview(cfg, "admin@test", "", fake, "vol")
	if err != nil || strings.Contains(out, "alice") || !strings.Contains(out, `"namespace":"`+fake+`"`) || !strings.Contains(out, `"canUpgrade":true`) {
		t.Fatalf("masked preview: %v %s", err, out)
	}

	if err := KubeLonghornEngineUpgrade(cfg, "admin@test", "", fake, "vol", engineNew); err != nil {
		t.Fatal(err)
	}

	if r := lastRequest(f); r.path != "/api/v1/namespaces/"+ns+"/services/longhorn-backend:9500/proxy/v1/volumes/vol" || r.body != `{"image":"`+engineNew+`"}` {
		t.Fatalf("posted %+v", r)
	}

	got := readAudit(t, "admin@test", "longhorn-engine-upgrade")
	if len(got) != 1 || got[0].Outcome != auditOK || got[0].Params != "image="+engineNew+" live=true" || got[0].Object != "vol" {
		t.Fatalf("audit %+v", got)
	}
}

func TestReadLonghornEngineFields(t *testing.T) {
	vol := func(name, spec, current string) string {
		return `{"metadata":{"name":"` + name + `","namespace":"storage"},"spec":{"numberOfReplicas":1,"image":"` + spec + `"},"status":{"state":"detached","robustness":"healthy","currentImage":"` + current + `"}}`
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/longhorn.io/v1beta2/volumes": `{"items":[` + vol("old", engineOld, engineOld) + `,` + vol("new", engineNew, engineNew) + `,` +
			vol("moving", engineNew, engineOld) + `,` + vol("unset", "", "") + `]}`,
		"GET /apis/longhorn.io/v1beta2/replicas":                  `{"items":[]}`,
		"GET /apis/longhorn.io/v1beta2/nodes":                     `{"items":[]}`,
		"GET /apis/longhorn.io/v1beta2/engineimages":              `{"items":[` + engineImageJSON(engineOld, "v1.12.0", `"node-1":true,"node-2":false`) + `,` + engineImageJSON(engineNew, "v1.13.0", `"node-1":true,"node-2":true`) + `]}`,
		"GET " + engineNS + "/settings/" + lhSettingDefaultEngine: `{"value":"` + engineNew + `"}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	out := readLonghorn(context.Background(), k, "v1beta2")
	if out.Error != "" || out.DefaultEngineImage != engineNew || len(out.EngineImages) != 2 {
		t.Fatalf("%+v", out)
	}

	if ei := out.EngineImages[0]; ei.Image != engineNew || ei.Version != "v1.13.0" || ei.State != "deployed" || ei.DeployedNodes != 2 || ei.Nodes != 2 || ei.RefCount != 1 {
		t.Errorf("default image %+v", ei)
	}

	if ei := out.EngineImages[1]; ei.Image != engineOld || ei.DeployedNodes != 1 || ei.Nodes != 2 {
		t.Errorf("old image %+v", ei)
	}

	byName := map[string]longhornVolume{}
	for _, v := range out.Volumes {
		byName[v.Name] = v
	}

	want := map[string][3]bool{ // upgrading, outdated, version known
		"old": {false, true, true}, "new": {false, false, true}, "moving": {true, true, true}, "unset": {false, false, false},
	}

	for name, w := range want {
		v := byName[name]
		if v.EngineUpgrading != w[0] || v.EngineOutdated != w[1] || (v.EngineVersion != "") != w[2] {
			t.Errorf("%s: %+v", name, v)
		}
	}

	if v := byName["moving"]; v.EngineImage != engineNew || v.EngineCurrentImage != engineOld || v.EngineVersion != "v1.12.0" {
		t.Errorf("moving: %+v", v)
	}

	// Without EngineImages (an older Longhorn, or no right to list them) nothing changes.
	f.answerWith("GET /apis/longhorn.io/v1beta2/engineimages", 403, `{"kind":"Status"}`)

	out = readLonghorn(context.Background(), k, "v1beta2")
	if out.Error != "" || out.DefaultEngineImage != "" || len(out.EngineImages) != 0 || out.EngineImages == nil || byNameOutdated(out) {
		t.Fatalf("without images: %+v", out)
	}
}

func byNameOutdated(out *longhornStatus) bool {
	for _, v := range out.Volumes {
		if v.EngineOutdated {
			return true
		}
	}

	return false
}

func TestNaturalCompareOrdersEngineTags(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"longhornio/longhorn-engine:v1.9.1", "longhornio/longhorn-engine:v1.10.0", -1},
		{"longhornio/longhorn-engine:v1.10.0", "longhornio/longhorn-engine:v1.9.1", 1},
		{"longhornio/longhorn-engine:v1.12.0", "longhornio/longhorn-engine:v1.12.0", 0},
		{"a:v01", "a:v1", 0},
		{"a:v1", "a:v1.0", -1},
	} {
		if got := naturalCompare(tc.a, tc.b); got != tc.want {
			t.Errorf("naturalCompare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
