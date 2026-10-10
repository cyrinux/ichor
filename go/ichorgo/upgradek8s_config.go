//lint:file-ignore SA1019 most machine configs still hold the images in the v1alpha1 fields; the documents replacing them are handled next to them

package ichorgo

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	configdoc "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	k8sdoc "github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
)

// The Kubernetes images in a machine config: in the v1alpha1 document (cluster.apiServer.image,
// …, machine.kubelet.image) or, since Talos 1.14, in documents of their own
// (KubeAPIServerConfig, …, KubeletConfig), which win over the v1alpha1 fields.

// configK8sImages are the images the config sets (component -> image; one it leaves to
// Talos's default is missing) and whether kube-proxy is enabled.
func configK8sImages(p talosconfig.Provider) (images map[string]string, proxy bool) {
	images, proxy = map[string]string{}, true

	set := func(component, image string) {
		if image != "" {
			images[component] = image
		}
	}

	for _, d := range p.Documents() {
		switch doc := d.(type) {
		case *v1alpha1.Config:
			proxy = readV1Alpha1Images(doc, set)
		case *k8sdoc.KubeAPIServerConfigV1Alpha1:
			set(k8sAPIServer, doc.PodImage)
		case *k8sdoc.KubeControllerManagerConfigV1Alpha1:
			set(k8sControllerManager, doc.PodImage)
		case *k8sdoc.KubeSchedulerConfigV1Alpha1:
			set(k8sScheduler, doc.PodImage)
		case *k8sdoc.KubeProxyConfigV1Alpha1:
			set(k8sProxy, doc.ProxyImage)

			if doc.ProxyEnabled != nil {
				proxy = *doc.ProxyEnabled
			}
		case *k8sdoc.KubeletConfigV1Alpha1:
			set(k8sKubelet, doc.KubeletImage)
		}
	}

	return images, proxy
}

// readV1Alpha1Images passes the images of c to set and tells whether kube-proxy is enabled.
func readV1Alpha1Images(c *v1alpha1.Config, set func(component, image string)) (proxy bool) {
	proxy = true

	if m := c.MachineConfig; m != nil && m.MachineKubelet != nil {
		set(k8sKubelet, m.MachineKubelet.KubeletImage)
	}

	cc := c.ClusterConfig
	if cc == nil {
		return proxy
	}

	if cc.APIServerConfig != nil {
		set(k8sAPIServer, cc.APIServerConfig.ContainerImage)
	}

	if cc.ControllerManagerConfig != nil {
		set(k8sControllerManager, cc.ControllerManagerConfig.ContainerImage)
	}

	if cc.SchedulerConfig != nil {
		set(k8sScheduler, cc.SchedulerConfig.ContainerImage)
	}

	if cc.ProxyConfig != nil {
		set(k8sProxy, cc.ProxyConfig.ContainerImage)
		proxy = cc.ProxyConfig.Disabled == nil || !*cc.ProxyConfig.Disabled
	}

	return proxy
}

// withK8sImages is p with the images of want (component -> image) and nothing else changed:
// in the component's own document when the config has one, else in the v1alpha1 document.
func withK8sImages(p talosconfig.Provider, want map[string]string) (talosconfig.Provider, error) {
	left := maps.Clone(want)
	docs := make([]configdoc.Document, 0, len(p.Documents()))

	for _, d := range p.Documents() {
		doc := d.Clone()

		switch typed := doc.(type) {
		case *k8sdoc.KubeAPIServerConfigV1Alpha1:
			setDocImage(left, k8sAPIServer, &typed.PodImage)
		case *k8sdoc.KubeControllerManagerConfigV1Alpha1:
			setDocImage(left, k8sControllerManager, &typed.PodImage)
		case *k8sdoc.KubeSchedulerConfigV1Alpha1:
			setDocImage(left, k8sScheduler, &typed.PodImage)
		case *k8sdoc.KubeProxyConfigV1Alpha1:
			setDocImage(left, k8sProxy, &typed.ProxyImage)
		case *k8sdoc.KubeletConfigV1Alpha1:
			setDocImage(left, k8sKubelet, &typed.KubeletImage)
		}

		docs = append(docs, doc)
	}

	if len(left) > 0 {
		i := slices.IndexFunc(docs, func(d configdoc.Document) bool { _, ok := d.(*v1alpha1.Config); return ok })
		if i < 0 {
			return nil, fmt.Errorf("the config has no place for the %s image", strings.Join(slices.Sorted(maps.Keys(left)), ", "))
		}

		setV1Alpha1Images(docs[i].(*v1alpha1.Config), left) //nolint:forcetypeassert // found just above
	}

	return container.New(docs...)
}

func setDocImage(left map[string]string, component string, field *string) {
	if image, ok := left[component]; ok {
		*field = image
		delete(left, component)
	}
}

// setV1Alpha1Images sets the image fields of want in c, and nothing else.
func setV1Alpha1Images(c *v1alpha1.Config, want map[string]string) {
	if c.ClusterConfig == nil {
		c.ClusterConfig = &v1alpha1.ClusterConfig{}
	}

	if c.MachineConfig == nil {
		c.MachineConfig = &v1alpha1.MachineConfig{}
	}

	cc := c.ClusterConfig

	for component, image := range want {
		switch component {
		case k8sAPIServer:
			if cc.APIServerConfig == nil {
				cc.APIServerConfig = &v1alpha1.APIServerConfig{}
			}

			cc.APIServerConfig.ContainerImage = image
		case k8sControllerManager:
			if cc.ControllerManagerConfig == nil {
				cc.ControllerManagerConfig = &v1alpha1.ControllerManagerConfig{}
			}

			cc.ControllerManagerConfig.ContainerImage = image
		case k8sScheduler:
			if cc.SchedulerConfig == nil {
				cc.SchedulerConfig = &v1alpha1.SchedulerConfig{}
			}

			cc.SchedulerConfig.ContainerImage = image
		case k8sProxy:
			if cc.ProxyConfig == nil {
				cc.ProxyConfig = &v1alpha1.ProxyConfig{}
			}

			cc.ProxyConfig.ContainerImage = image
		case k8sKubelet:
			if c.MachineConfig.MachineKubelet == nil {
				c.MachineConfig.MachineKubelet = &v1alpha1.KubeletConfig{}
			}

			c.MachineConfig.MachineKubelet.KubeletImage = image
		}
	}
}
