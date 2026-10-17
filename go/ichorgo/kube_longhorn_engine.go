package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// A Longhorn volume's engine upgrade: after Longhorn itself is upgraded every volume keeps
// its old engine image until it is moved to a new one, live when attached and healthy,
// offline when detached. The rules are longhorn-manager's own (EngineUpgrade in
// manager/volume.go), checked on a fresh read before the request; the manager checks again.

const (
	lhSettingDefaultEngine   = "default-engine-image"
	lhSettingAutoUpgradeNode = "concurrent-automatic-engine-upgrade-per-node-limit"
	lhImageDeployed          = "deployed"
	lhMaxImageLen            = 512
)

// longhornEngineImage is one EngineImage of the cluster, for the Longhorn tab.
type longhornEngineImage struct {
	Image         string `json:"image"`
	Version       string `json:"version"`
	State         string `json:"state"` // deploying|deployed|incompatible|error
	RefCount      int    `json:"refCount"`
	Incompatible  bool   `json:"incompatible"`
	DeployedNodes int    `json:"deployedNodes"`
	Nodes         int    `json:"nodes"`
}

type lhEngineImageObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Image string `json:"image"`
	} `json:"spec"`
	Status struct {
		State             string          `json:"state"`
		RefCount          int             `json:"refCount"`
		Incompatible      bool            `json:"incompatible"`
		NodeDeploymentMap map[string]bool `json:"nodeDeploymentMap"`
		Version           string          `json:"version"`
	} `json:"status"`
}

type lhSettingObject struct {
	Value string `json:"value"`
}

// lhEngineVolume is what the upgrade rules read of a volume.
type lhEngineVolume struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		DeletionTimestamp string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Image           string `json:"image"`
		DataEngine      string `json:"dataEngine"`
		DataLocality    string `json:"dataLocality"`
		Frontend        string `json:"frontend"`
		MigrationNodeID string `json:"migrationNodeID"`
	} `json:"spec"`
	Status struct {
		State         string `json:"state"`
		Robustness    string `json:"robustness"`
		CurrentNodeID string `json:"currentNodeID"`
		CurrentImage  string `json:"currentImage"`
	} `json:"status"`
}

// longhornEngineChoice is an image the volume may move to. Deployed: on the volume's node and
// on every node holding one of its replicas.
type longhornEngineChoice struct {
	Image        string `json:"image"`
	Version      string `json:"version"`
	Deployed     bool   `json:"deployed"`
	Default      bool   `json:"default"`
	Incompatible bool   `json:"incompatible"`
}

type longhornEngineUpgradePreview struct {
	Namespace    string                 `json:"namespace"`
	Name         string                 `json:"name"`
	State        string                 `json:"state"`
	Robustness   string                 `json:"robustness"`
	CurrentImage string                 `json:"currentImage"`
	SpecImage    string                 `json:"specImage"`
	DefaultImage string                 `json:"defaultImage"`
	Live         bool                   `json:"live"`
	CanUpgrade   bool                   `json:"canUpgrade"`
	Reason       string                 `json:"reason"`
	Images       []longhornEngineChoice `json:"images"`
	Error        string                 `json:"error"`
}

// lhEngineFacts is everything the upgrade rules need, read fresh.
type lhEngineFacts struct {
	volume       lhEngineVolume
	replicaNodes []string
	images       []lhEngineImageObject
	defaultImage string
	autoLimit    int // concurrent-automatic-engine-upgrade-per-node-limit
}

// KubeLonghornEngineUpgradePreview reads the volume namespace/name, the engine images it may
// move to and whether it may now, with the reason when it may not.
func KubeLonghornEngineUpgradePreview(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	if err := validateKubeName("volume", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() longhornEngineUpgradePreview {
		return demoLonghornEnginePreview(namespace, name)
	}, func(ctx context.Context, k *kubeClient) (longhornEngineUpgradePreview, error) {
		return readLonghornEnginePreview(ctx, k, namespace, name)
	})
}

// KubeLonghornEngineUpgrade asks Longhorn to move the volume namespace/name to the engine
// image image (os:admin): live when attached, offline when detached. image must be one of
// the preview's images, checked against a fresh read of the cluster's EngineImages.
func KubeLonghornEngineUpgrade(configYAML, contextName, kubeServer, namespace, name, image string) (err error) {
	defer maskErr(&err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	image = privacy.reveal(strings.TrimSpace(image))

	live := false

	// recordedRun builds the entry once the volume was read: live or offline.
	return recordedRun(configYAML, contextName, func() auditAction {
		return auditAction{Server: kubeServer, Action: "longhorn-engine-upgrade", Namespace: namespace, Object: name,
			Params: "image=" + image + " live=" + strconv.FormatBool(live)}
	}, func() error {
		if err := validateKubeName("volume", namespace, name); err != nil {
			return err
		}

		if err := validateEngineImage(image); err != nil {
			return err
		}

		return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
			return upgradeLonghornEngine(ctx, k, namespace, name, image, &live)
		})
	})
}

// validateEngineImage refuses what cannot be an image reference before any request.
func validateEngineImage(image string) error {
	if image == "" || len(image) > lhMaxImageLen || strings.ContainsFunc(image, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return fmt.Errorf("invalid engine image %q", image)
	}

	return nil
}

func upgradeLonghornEngine(ctx context.Context, k *kubeClient, namespace, name, image string, live *bool) error {
	f, err := readLonghornEngineFacts(ctx, k, namespace, name)
	if err != nil {
		return err
	}

	*live = f.live()

	if reason := f.imageRefusal(image); reason != "" {
		return &kubeAPIError{Code: 409, Reason: "Refused", Message: reason}
	}

	return longhornManagerAction(ctx, k, namespace, name, "engineUpgrade", map[string]any{"image": image})
}

func readLonghornEnginePreview(ctx context.Context, k *kubeClient, namespace, name string) (longhornEngineUpgradePreview, error) {
	base, err := longhornAttachmentBase(ctx, k, namespace)
	if err != nil {
		return longhornEngineUpgradePreview{}, err
	}

	var v lhEngineVolume
	if err := k.get(ctx, base+"/volumes/"+url.PathEscape(name), &v); err != nil {
		return longhornEngineUpgradePreview{}, err
	}

	f := lhEngineFacts{volume: v}
	if err := f.readAround(ctx, k, base, name); err != nil {
		p := f.preview(namespace, name)
		p.CanUpgrade, p.Error = false, err.Error()

		return p, nil
	}

	return f.preview(namespace, name), nil
}

// readLonghornEngineFacts reads the volume, its replicas' nodes, the EngineImages and the
// two settings; any failure fails.
func readLonghornEngineFacts(ctx context.Context, k *kubeClient, namespace, name string) (lhEngineFacts, error) {
	base, err := longhornAttachmentBase(ctx, k, namespace)
	if err != nil {
		return lhEngineFacts{}, err
	}

	var f lhEngineFacts
	if err := k.get(ctx, base+"/volumes/"+url.PathEscape(name), &f.volume); err != nil {
		return f, err
	}

	return f, f.readAround(ctx, k, base, name)
}

// readAround fills in what surrounds the volume: replicas, EngineImages and settings.
func (f *lhEngineFacts) readAround(ctx context.Context, k *kubeClient, base, name string) error {
	var (
		replicas kubeList[lhReplicaObject]
		images   kubeList[lhEngineImageObject]
		limit    string
		errs     = make([]error, 4)
		wg       sync.WaitGroup
	)

	wg.Go(func() { errs[0] = getList(ctx, k, base+"/replicas", &replicas) })
	wg.Go(func() { errs[1] = getList(ctx, k, base+"/engineimages", &images) })
	wg.Go(func() { f.defaultImage, errs[2] = readLonghornSetting(ctx, k, base, lhSettingDefaultEngine) })
	wg.Go(func() { limit, errs[3] = readLonghornSetting(ctx, k, base, lhSettingAutoUpgradeNode) })
	wg.Wait()

	f.images = images.Items
	f.autoLimit, _ = strconv.Atoi(strings.TrimSpace(limit))

	for _, r := range replicas.Items {
		if r.Spec.VolumeName == name && r.Metadata.DeletionTimestamp == "" && r.Spec.NodeID != "" && !slices.Contains(f.replicaNodes, r.Spec.NodeID) {
			f.replicaNodes = append(f.replicaNodes, r.Spec.NodeID)
		}
	}

	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

// readLonghornSetting is the value of the setting name in nsBase (…/namespaces/<ns>), ""
// when Longhorn has no such setting.
func readLonghornSetting(ctx context.Context, k *kubeClient, nsBase, name string) (string, error) {
	var s lhSettingObject
	if err := k.get(ctx, nsBase+"/settings/"+url.PathEscape(name), &s); err != nil {
		if isNotFound(err) {
			return "", nil
		}

		return "", err
	}

	return strings.TrimSpace(s.Value), nil
}

// readLonghornDefaultEngine is Longhorn's default engine image, read in the EngineImages'
// namespace (Longhorn's own); "" when there are none or it cannot be read.
func readLonghornDefaultEngine(ctx context.Context, k *kubeClient, base string, images []lhEngineImageObject) string {
	if len(images) == 0 || images[0].Metadata.Namespace == "" {
		return ""
	}

	v, _ := readLonghornSetting(ctx, k, base+"namespaces/"+url.PathEscape(images[0].Metadata.Namespace), lhSettingDefaultEngine)

	return v
}

// addLonghornEngines lists the EngineImages and tells each volume its engine's version and
// whether it lags the default image.
func addLonghornEngines(out *longhornStatus, images []lhEngineImageObject, defaultImage string) {
	out.DefaultEngineImage = defaultImage

	byImage := map[string]lhEngineImageObject{}

	for _, ei := range images {
		byImage[ei.Spec.Image] = ei
		deployed := 0

		for _, ok := range ei.Status.NodeDeploymentMap {
			if ok {
				deployed++
			}
		}

		out.EngineImages = append(out.EngineImages, longhornEngineImage{
			Image: ei.Spec.Image, Version: ei.Status.Version, State: ei.Status.State, RefCount: ei.Status.RefCount,
			Incompatible: ei.Status.Incompatible, DeployedNodes: deployed, Nodes: len(ei.Status.NodeDeploymentMap),
		})
	}

	slices.SortFunc(out.EngineImages, func(a, b longhornEngineImage) int { return compareEngineImages(a.Image, b.Image, defaultImage) })

	def, ok := byImage[defaultImage]
	defaultDeployed := ok && defaultImage != "" && def.Status.State == lhImageDeployed

	for i := range out.Volumes {
		v := &out.Volumes[i]
		v.EngineVersion = byImage[v.EngineCurrentImage].Status.Version
		v.EngineOutdated = defaultDeployed && v.EngineCurrentImage != "" && v.EngineCurrentImage != defaultImage
	}
}

// compareEngineImages orders the default image first, then the others newest tag first.
func compareEngineImages(a, b, defaultImage string) int {
	if a == b {
		return 0
	}

	if a == defaultImage {
		return -1
	}

	if b == defaultImage {
		return 1
	}

	return strings.Compare(b, a)
}

func (f lhEngineFacts) live() bool { return f.volume.Status.State == "attached" }

// volumeRefusal is why the volume cannot change engine now, "" when it can.
func (f lhEngineFacts) volumeRefusal() string {
	v := f.volume

	switch {
	case v.Metadata.DeletionTimestamp != "":
		return "The volume is being deleted."
	case v.Spec.DataEngine == "v2":
		return "Engine upgrade is not supported for V2 data engine volumes."
	case v.Spec.MigrationNodeID != "":
		return "The volume is migrating between nodes: wait until the migration ends."
	case v.Status.CurrentImage == "":
		return "Longhorn has not reported the volume's current engine image yet."
	case v.Spec.Image != "" && v.Spec.Image != v.Status.CurrentImage:
		return "An engine upgrade to " + v.Spec.Image + " is already in progress."
	case v.Status.State != "attached" && v.Status.State != "detached":
		return "The volume is " + cmp.Or(v.Status.State, "in an unknown state") + ": wait until it is attached or detached."
	case f.live() && v.Status.Robustness != "healthy":
		return "A live engine upgrade needs a healthy volume, and this one is " + cmp.Or(v.Status.Robustness, "unknown") + "."
	case f.live() && v.Spec.DataLocality == "strict-local":
		return "Live engine upgrade is not supported for strict-local volumes: detach the volume first."
	case f.live() && v.Spec.Frontend == "iscsi":
		return "Live engine upgrade is not supported with the iSCSI frontend: detach the volume first."
	}

	return ""
}

// offered: an image the volume may move to. With automatic engine upgrade on (a per-node
// limit above 0) Longhorn accepts only the default image.
func (f lhEngineFacts) offered(ei lhEngineImageObject) bool {
	return ei.Spec.Image != "" && ei.Spec.Image != f.volume.Status.CurrentImage && (f.autoLimit <= 0 || ei.Spec.Image == f.defaultImage)
}

// deployed: the image runs on the volume's node and every node holding a replica.
func (f lhEngineFacts) deployed(ei lhEngineImageObject) bool {
	if ei.Status.State != lhImageDeployed {
		return false
	}

	nodes := f.replicaNodes
	if n := f.volume.Status.CurrentNodeID; n != "" {
		nodes = append(slices.Clone(nodes), n)
	}

	for _, n := range nodes {
		if !ei.Status.NodeDeploymentMap[n] {
			return false
		}
	}

	return true
}

// imageRefusal is why the volume cannot move to image now, "" when it can.
func (f lhEngineFacts) imageRefusal(image string) string {
	if reason := f.volumeRefusal(); reason != "" {
		return reason
	}

	i := slices.IndexFunc(f.images, func(ei lhEngineImageObject) bool { return ei.Spec.Image == image })

	switch {
	case i < 0:
		return "The engine image " + image + " is not one of Longhorn's engine images."
	case image == f.volume.Status.CurrentImage:
		return "The volume already runs this engine image."
	case !f.offered(f.images[i]):
		return "Automatic engine upgrade is on: Longhorn only accepts the default engine image."
	case f.images[i].Status.Incompatible:
		return "The engine image is incompatible with this Longhorn version."
	case !f.deployed(f.images[i]):
		return "The engine image is not deployed on every node of the volume yet."
	}

	return ""
}

func (f lhEngineFacts) preview(namespace, name string) longhornEngineUpgradePreview {
	v := f.volume
	p := longhornEngineUpgradePreview{
		Namespace: namespace, Name: name, State: v.Status.State, Robustness: v.Status.Robustness,
		CurrentImage: v.Status.CurrentImage, SpecImage: v.Spec.Image, DefaultImage: f.defaultImage,
		Live: f.live(), Images: []longhornEngineChoice{},
	}

	for _, ei := range f.images {
		if f.offered(ei) {
			p.Images = append(p.Images, longhornEngineChoice{
				Image: ei.Spec.Image, Version: ei.Status.Version, Deployed: f.deployed(ei),
				Default: ei.Spec.Image == f.defaultImage, Incompatible: ei.Status.Incompatible,
			})
		}
	}

	slices.SortFunc(p.Images, func(a, b longhornEngineChoice) int { return compareEngineImages(a.Image, b.Image, f.defaultImage) })

	p.Reason = f.volumeRefusal()
	if p.Reason == "" {
		p.Reason = f.choiceRefusal(p.Images)
	}

	p.CanUpgrade = p.Reason == ""

	return p
}

// choiceRefusal is "" when one of images can be chosen, else why none can (the first one's).
func (f lhEngineFacts) choiceRefusal(images []longhornEngineChoice) string {
	if len(images) == 0 {
		return "No other engine image is available for this volume."
	}

	for _, c := range images {
		if f.imageRefusal(c.Image) == "" {
			return ""
		}
	}

	return f.imageRefusal(images[0].Image)
}

// addDemoLonghornEngines: Longhorn was just upgraded and postgres-data still runs the old
// engine; both images are deployed on every node.
func addDemoLonghornEngines(lh *longhornStatus) {
	const (
		current  = "longhornio/longhorn-engine:v1.13.0"
		previous = "longhornio/longhorn-engine:v1.12.0"
	)

	nodes := len(lh.Nodes)
	lh.DefaultEngineImage = current
	lh.EngineImages = []longhornEngineImage{
		{Image: current, Version: "v1.13.0", State: lhImageDeployed, RefCount: len(lh.Volumes) - 1, DeployedNodes: nodes, Nodes: nodes},
		{Image: previous, Version: "v1.12.0", State: lhImageDeployed, RefCount: 1, DeployedNodes: nodes, Nodes: nodes},
	}

	for i := range lh.Volumes {
		v := &lh.Volumes[i]
		v.EngineImage, v.EngineCurrentImage, v.EngineVersion = current, current, "v1.13.0"

		if v.PVCName == "postgres-data" {
			v.EngineImage, v.EngineCurrentImage, v.EngineVersion, v.EngineOutdated = previous, previous, "v1.12.0", true
		}
	}
}

// demoLonghornEnginePreview: the demo volumes' engines, on images deployed everywhere.
func demoLonghornEnginePreview(namespace, name string) longhornEngineUpgradePreview {
	d := demoDataServices(time.Now()).Longhorn

	for _, v := range d.Volumes {
		if v.Namespace != namespace || v.Name != name {
			continue
		}

		f := lhEngineFacts{replicaNodes: v.ReplicaNodes, defaultImage: d.DefaultEngineImage}
		f.volume.Metadata.Name, f.volume.Metadata.Namespace = v.Name, v.Namespace
		f.volume.Spec.Image, f.volume.Spec.DataEngine = v.EngineImage, "v1"
		f.volume.Status.State, f.volume.Status.Robustness = v.State, v.Robustness
		f.volume.Status.CurrentNodeID, f.volume.Status.CurrentImage = v.Node, v.EngineCurrentImage

		for _, ei := range d.EngineImages {
			var obj lhEngineImageObject
			obj.Spec.Image = ei.Image
			obj.Status.State, obj.Status.Version, obj.Status.RefCount = ei.State, ei.Version, ei.RefCount
			obj.Status.NodeDeploymentMap = map[string]bool{}

			for _, n := range d.Nodes {
				obj.Status.NodeDeploymentMap[n.Name] = true
			}

			f.images = append(f.images, obj)
		}

		return f.preview(namespace, name)
	}

	return longhornEngineUpgradePreview{Namespace: namespace, Name: name, Images: []longhornEngineChoice{}, Error: "the demo volume was not found"}
}
