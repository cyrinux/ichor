package ichorgo

import (
	"context"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// Roles of a Talos system image.
const (
	systemImageInstaller         = "installer"
	systemImageKubelet           = "kubelet"
	systemImageEtcd              = "etcd"
	systemImageAPIServer         = "apiServer"
	systemImageControllerManager = "controllerManager"
	systemImageScheduler         = "scheduler"
	systemImageCoreDNS           = "coreDNS"
	systemImageProxy             = "proxy"
	systemImageSystem            = "system" // any other image of the system containerd namespace
)

// systemImage is an image Talos runs on a node, outside of any app's pods.
type systemImage struct {
	Role   string `json:"role"`
	Image  string `json:"image"`  // as the machine config names it, or as containerd lists it
	Ref    string `json:"ref"`    // what a scan pulls: repo@digest when the node has it, else image
	Digest string `json:"digest"` // sha256:…, "" when unknown
}

// roleImage is an image the machine config names, by role.
type roleImage struct{ role, image string }

// TalosSystemImages lists the images Talos runs on node outside of any pod of an app, for an
// image scan (os:admin: it reads the machine config): [{role,image,ref,digest}]. The machine
// config's images (installer, kubelet; etcd, kube-apiserver, kube-controller-manager,
// kube-scheduler, CoreDNS and kube-proxy on a control plane node) come first, with the digest
// the node pulled when containerd has it (the installer never is: it is scanned by tag), then
// every other image of the system containerd namespace.
func TalosSystemImages(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("TalosSystemImages", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		mc, err := safe.StateGetByID[*config.MachineConfig](client.WithNode(ctx, node), s.client.COSI, config.ActiveID)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		system, err := listImages(ctx, s.client, common.ContainerdNamespace_NS_SYSTEM)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		// The static pods' images (kube-apiserver…) are pulled in the CRI namespace.
		cri, err := listImages(ctx, s.client, common.ContainerdNamespace_NS_CRI)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		return toJSON(systemImagesOf(configImages(mc.Provider()), mapImages(system), mapImages(cri)))
	})
}

// configImages are the images the machine config names (Talos's defaults when unset), the
// control plane's only on a control plane node and the add-ons' only when enabled.
func configImages(p talosconfig.Config) []roleImage {
	var out []roleImage

	add := func(role, image string) {
		if image = strings.TrimSpace(image); image != "" {
			out = append(out, roleImage{role, image})
		}
	}

	if m := p.Machine(); m != nil {
		if install := m.Install(); install != nil {
			add(systemImageInstaller, install.Image())
		}
	}

	if k := p.K8sKubeletConfig(); k != nil {
		add(systemImageKubelet, k.Image())
	}

	if m := p.Machine(); m == nil || !m.Type().IsControlPlane() {
		return out
	}

	if c := p.Cluster(); c != nil && c.Etcd() != nil {
		add(systemImageEtcd, c.Etcd().Image())
	}

	if a := p.K8sAPIServerConfig(); a != nil {
		add(systemImageAPIServer, a.Image())
	}

	if c := p.K8sControllerManagerConfig(); c != nil && c.Enabled() {
		add(systemImageControllerManager, c.Image())
	}

	if s := p.K8sSchedulerConfig(); s != nil && s.Enabled() {
		add(systemImageScheduler, s.Image())
	}

	if d := p.K8sCoreDNSConfig(); d != nil && d.Enabled() {
		add(systemImageCoreDNS, d.Image())
	}

	if x := p.K8sProxyConfig(); x != nil && x.Enabled() {
		add(systemImageProxy, x.Image())
	}

	return out
}

// systemImagesOf merges the configured images with what containerd holds: each configured
// image gets the digest of the image of that name (else of that repository) in the system or
// CRI namespace, then the system namespace's other images follow. Deduplicated by ref.
func systemImagesOf(configured []roleImage, system, cri []imageInfo) []systemImage {
	pulled := append(append([]imageInfo{}, system...), cri...)
	out := []systemImage{}
	used := map[string]bool{} // containerd image names already listed

	add := func(img systemImage) {
		for _, o := range out {
			if o.Ref == img.Ref {
				return
			}
		}

		out = append(out, img)
	}

	for _, c := range configured {
		img := systemImage{Role: c.role, Image: c.image, Ref: c.image}
		_, img.Digest, _ = strings.Cut(c.image, "@")

		if p, ok := pulledImage(pulled, c.image); ok && img.Digest == "" && c.role != systemImageInstaller {
			used[p.Name] = true
			repo, _ := splitImageRef(c.image)
			img.Digest, img.Ref = p.Digest, repo+"@"+p.Digest
		}

		add(img)
	}

	for _, s := range system {
		if used[s.Name] || s.Digest == "" {
			continue
		}

		repo, _ := splitImageRef(s.Name)
		add(systemImage{Role: systemImageSystem, Image: s.Name, Ref: repo + "@" + s.Digest, Digest: s.Digest})
	}

	return out
}

// pulledImage finds image among the pulled ones: by name, else by repository.
func pulledImage(pulled []imageInfo, image string) (imageInfo, bool) {
	for _, p := range pulled {
		if p.Name == image && p.Digest != "" {
			return p, true
		}
	}

	repo, _ := splitImageRef(image)

	for _, p := range pulled {
		if r, _ := splitImageRef(p.Name); r == repo && p.Digest != "" {
			return p, true
		}
	}

	return imageInfo{}, false
}
