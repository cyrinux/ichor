package ichorgo

import (
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/meta"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// metaCluster is one node with a metal network config, a binary value and a secret in META.
func metaCluster(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.81", "v1.11.0", machine.TypeWorker)

	key := func(tag uint8, value string) *runtime.MetaKey {
		k := runtime.NewMetaKey(runtime.NamespaceName, runtime.MetaKeyTagToID(tag))
		k.TypedSpec().Value = value

		return k
	}

	loaded := runtime.NewMetaLoaded()
	loaded.TypedSpec().Done = true

	f.put("192.0.2.81",
		key(meta.UserReserved1, "\x00\x01"),
		key(meta.MetalNetworkPlatformConfig, "addresses: []\n"),
		key(meta.UniqueMachineToken, "do-not-show"),
		loaded)

	return f, f.start(t, "192.0.2.81")
}

func readMetaKeys(t *testing.T, cfg string) metaKeyList {
	t.Helper()

	out, err := NodeMetaKeys(cfg, "fake", "192.0.2.81")

	return decodeJSON[metaKeyList](t, out, err)
}

func TestNodeMetaKeys(t *testing.T) {
	_, cfg := metaCluster(t)

	list := readMetaKeys(t, cfg)

	if !list.Loaded || len(list.Keys) != 3 {
		t.Fatalf("list = %+v", list)
	}

	network, user, token := list.Keys[0], list.Keys[1], list.Keys[2]

	if network.Hex != "0x0a" || network.Name != "metal network config" || network.Value != "addresses: []\n" || !network.Writable {
		t.Errorf("network = %+v", network)
	}

	if !user.Binary || user.Value != "0001" || user.Size != 2 {
		t.Errorf("user = %+v", user)
	}

	if !token.Secret || token.Value != "" || token.Size != len("do-not-show") {
		t.Errorf("token = %+v", token)
	}

	if len(list.Known) != len(metaKeyNames) || list.Known[0].Writable {
		t.Errorf("known = %+v", list.Known) // 0x06 upgrade first, written by Talos only
	}
}

func TestNodeMetaWriteAndDelete(t *testing.T) {
	withDataDir(t)

	f, cfg := metaCluster(t)

	if err := NodeMetaWrite(cfg, "fake", "192.0.2.81", meta.UserReserved2, "rack-7"); err != nil {
		t.Fatal(err)
	}

	if err := NodeMetaDelete(cfg, "fake", "192.0.2.81", meta.MetalNetworkPlatformConfig); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, k := range readMetaKeys(t, cfg).Keys {
		got = append(got, k.Hex+"="+k.Value)
	}

	if want := "0x0c=0001 0x0d=rack-7 0x10="; strings.Join(got, " ") != want {
		t.Errorf("keys = %q, want %q", strings.Join(got, " "), want)
	}

	if calls := strings.Join(append(f.called("MetaWrite"), f.called("MetaDelete")...), ","); calls != "MetaWrite 192.0.2.81,MetaDelete 192.0.2.81" {
		t.Errorf("calls = %s", calls)
	}

	writes := readAudit(t, "fake", "meta-write")
	if len(writes) != 1 || writes[0].Object != "META/0x0d" || writes[0].Params != "size=6" || strings.Contains(writes[0].Params, "rack") {
		t.Errorf("write audit = %+v", writes)
	}

	if deletes := readAudit(t, "fake", "meta-delete"); len(deletes) != 1 || deletes[0].Object != "META/0x0a" {
		t.Errorf("delete audit = %+v", deletes)
	}
}

func TestNodeMetaRefusals(t *testing.T) {
	withDataDir(t)

	f, cfg := metaCluster(t)

	for _, tc := range []struct {
		key   int
		value string
		want  string
	}{
		{256, "x", "0x00 to 0xff"},
		{-1, "x", "0x00 to 0xff"},
		{meta.Upgrade, "x", "written by Talos itself"},
		{meta.StateEncryptionConfig, "x", "written by Talos itself"},
		{0, "x", "written by Talos itself"},
		{meta.UserReserved3, "", "no value given"},
		{meta.UserReserved3, strings.Repeat("x", metaValueLimit+1), "at most"},
	} {
		if err := NodeMetaWrite(cfg, "fake", "192.0.2.81", tc.key, tc.value); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("write %d: %v", tc.key, err)
		}
	}

	if err := NodeMetaDelete(cfg, "fake", "192.0.2.81", meta.Upgrade); err == nil {
		t.Error("deleting the upgrade key must be refused")
	}

	if err := NodeMetaWrite(cfg, "fake", "192.0.2.99", meta.UserReserved3, "x"); err == nil || !strings.Contains(err.Error(), "not part of this context") {
		t.Errorf("unknown node: %v", err)
	}

	if calls := append(f.called("MetaWrite"), f.called("MetaDelete")...); len(calls) != 0 {
		t.Errorf("calls = %v", calls)
	}
}

func TestNodeMetaDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	node := demoNodes()[0].Node

	out, err := NodeMetaKeys(cfg, "", node)
	list := decodeJSON[metaKeyList](t, out, err)

	if len(list.Keys) != 3 || !list.Keys[2].Secret || list.Keys[2].Value != "" {
		t.Errorf("demo keys = %+v", list.Keys)
	}

	if err := NodeMetaWrite(cfg, "", node, meta.UserReserved1, "x"); err == nil {
		t.Error("the demo must refuse writes")
	}
}
