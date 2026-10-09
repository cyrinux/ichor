package ichorgo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

func TestSystemImagesOf(t *testing.T) {
	kubelet, etcd, apiserver, extra := "sha256:"+strings.Repeat("a1", 32), "sha256:"+strings.Repeat("b2", 32),
		"sha256:"+strings.Repeat("c3", 32), "sha256:"+strings.Repeat("d4", 32)
	pinned := "registry.example/proxy@sha256:" + strings.Repeat("e5", 32)

	configured := []roleImage{
		{systemImageInstaller, "factory.example/installer/abc:v1.14.0"},
		{systemImageKubelet, "ghcr.io/example/kubelet:v1.34.1"},
		{systemImageEtcd, "registry.example/etcd:3.6.5-0"},
		{systemImageAPIServer, "registry.example/kube-apiserver:v1.34.1"},
		{systemImageScheduler, "registry.example/kube-scheduler:v1.34.1"},
		{systemImageProxy, pinned},
	}
	system := []imageInfo{
		{Name: "ghcr.io/example/kubelet:v1.34.1", Digest: kubelet},
		{Name: "registry.example/etcd:3.6.4-0", Digest: etcd}, // same repository, another tag
		{Name: "ghcr.io/example/flannel:v0.27.0", Digest: extra},
		{Name: "ghcr.io/example/nodigest:1"},
	}
	cri := []imageInfo{{Name: "registry.example/kube-apiserver:v1.34.1", Digest: apiserver}}

	got := systemImagesOf(configured, system, cri)
	want := []systemImage{
		{Role: systemImageInstaller, Image: "factory.example/installer/abc:v1.14.0", Ref: "factory.example/installer/abc:v1.14.0"},
		{Role: systemImageKubelet, Image: "ghcr.io/example/kubelet:v1.34.1", Ref: "ghcr.io/example/kubelet@" + kubelet, Digest: kubelet},
		{Role: systemImageEtcd, Image: "registry.example/etcd:3.6.5-0", Ref: "registry.example/etcd@" + etcd, Digest: etcd},
		{Role: systemImageAPIServer, Image: "registry.example/kube-apiserver:v1.34.1", Ref: "registry.example/kube-apiserver@" + apiserver, Digest: apiserver},
		{Role: systemImageScheduler, Image: "registry.example/kube-scheduler:v1.34.1", Ref: "registry.example/kube-scheduler:v1.34.1"},
		{Role: systemImageProxy, Image: pinned, Ref: pinned, Digest: "sha256:" + strings.Repeat("e5", 32)},
		{Role: systemImageSystem, Image: "ghcr.io/example/flannel:v0.27.0", Ref: "ghcr.io/example/flannel@" + extra, Digest: extra},
	}

	if !reflect.DeepEqual(got, want) {
		js, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("got %s", js)
	}

	// Every ref is a valid scan option.
	refs := make([]string, len(got))
	for i, img := range got {
		refs[i] = img.Ref
	}

	js, _ := json.Marshal(map[string][]string{"images": refs})
	if _, err := decodeImageScanOptions(string(js)); err != nil {
		t.Fatal(err)
	}
}

func TestConfigImages(t *testing.T) {
	raw, err := generatedConfig()
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	roles := func(images []roleImage) []string {
		out := make([]string, len(images))
		for i, img := range images {
			out[i] = img.role
		}

		return out
	}

	// The generated control plane config leaves the images to Talos's defaults, and names no
	// installer.
	cp := configImages(provider)
	if got := roles(cp); !reflect.DeepEqual(got, []string{
		systemImageKubelet, systemImageEtcd, systemImageAPIServer,
		systemImageControllerManager, systemImageScheduler, systemImageCoreDNS, systemImageProxy,
	}) {
		t.Fatalf("roles %v", got)
	}

	if !strings.Contains(cp[0].image, "kubelet:v") {
		t.Fatalf("kubelet %q", cp[0].image)
	}

	worker, err := configloader.NewFromBytes([]byte(strings.Replace(string(raw), "type: controlplane", "type: worker", 1)))
	if err != nil {
		t.Fatal(err)
	}

	if got := roles(configImages(worker)); !reflect.DeepEqual(got, []string{systemImageKubelet}) {
		t.Fatalf("worker roles %v", got)
	}
}

// Through the Talos API: the machine config's images, the system namespace's digests.
func TestTalosSystemImagesFake(t *testing.T) {
	const node = "192.0.2.65"

	raw, err := generatedConfig()
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	kubelet := provider.K8sKubeletConfig().Image()
	digest := "sha256:" + strings.Repeat("f6", 32)

	f := newFakeTalos()
	f.addNode(t, node, "v1.13.0", machine.TypeControlPlane)
	f.putMachineConfig(t, node)
	f.systemImages = []*machineapi.ImageServiceListResponse{{Name: kubelet, Digest: digest}}

	out, err := TalosSystemImages(f.start(t, node), "fake", node)
	images := decodeJSON[[]systemImage](t, out, err)

	if len(images) != 7 || images[0].Role != systemImageKubelet || images[0].Digest != digest ||
		!strings.HasSuffix(images[0].Ref, "@"+digest) || images[1].Role != systemImageEtcd || images[1].Digest != "" {
		t.Fatalf("images = %s", out)
	}
}
