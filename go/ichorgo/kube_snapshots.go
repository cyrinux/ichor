package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

const groupSnapshot = "snapshot.storage.k8s.io"
const backupMatchKey = "ichor.levis.name/backup"
const snapshotDefaultKey = "snapshot.storage.kubernetes.io/is-default-class"

type volumeSnapshots struct {
	Installed     bool              `json:"installed"`
	Version       string            `json:"version"`
	Error         string            `json:"error"`
	ContentsError string            `json:"contentsError"`
	ClassesError  string            `json:"classesError"`
	ClaimsError   string            `json:"claimsError"`
	Snapshots     []volumeSnapshot  `json:"snapshots"`
	Contents      []snapshotContent `json:"contents"`
	Classes       []snapshotClass   `json:"classes"`
	Claims        []snapshotClaim   `json:"claims"`
}
type volumeSnapshot struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	PVC        string `json:"pvc"`
	Class      string `json:"class"`
	Content    string `json:"content"`
	Ready      bool   `json:"ready"`
	CreatedAt  int64  `json:"createdAt"`
	AgeSeconds int64  `json:"ageSeconds"`
	Error      string `json:"error"`
}
type snapshotContent struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Snapshot  string `json:"snapshot"`
	Class     string `json:"class"`
	Driver    string `json:"driver"`
	Ready     bool   `json:"ready"`
	CreatedAt int64  `json:"createdAt"`
	Error     string `json:"error"`
}
type snapshotClass struct {
	Name           string `json:"name"`
	Driver         string `json:"driver"`
	DeletionPolicy string `json:"deletionPolicy"`
	Default        bool   `json:"default"`
}
type snapshotClaim struct {
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	LastGoodAt int64  `json:"lastGoodAt"`
	CreatedAt  int64  `json:"createdAt"`
	AgeSeconds int64  `json:"ageSeconds"`
	Watched    bool   `json:"watched"`
	Stale      bool   `json:"stale"`
}
type snapshotObject struct {
	Metadata struct {
		kubeObjectMeta
		DeletionTimestamp *time.Time `json:"deletionTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Class  string `json:"volumeSnapshotClassName"`
		Source struct {
			PVC     string `json:"persistentVolumeClaimName"`
			Content string `json:"volumeSnapshotContentName"`
		} `json:"source"`
	} `json:"spec"`
	Status struct {
		Ready     bool       `json:"readyToUse"`
		CreatedAt *time.Time `json:"creationTime"`
		Content   string     `json:"boundVolumeSnapshotContentName"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"status"`
}
type snapshotContentObject struct {
	Metadata kubeObjectMeta `json:"metadata"`
	Spec     struct {
		Class  string `json:"volumeSnapshotClassName"`
		Driver string `json:"driver"`
		Ref    struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"volumeSnapshotRef"`
	} `json:"spec"`
	Status struct {
		Ready     bool  `json:"readyToUse"`
		CreatedAt int64 `json:"creationTime"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"status"`
}
type snapshotClassObject struct {
	Metadata       kubeObjectMeta `json:"metadata"`
	Driver         string         `json:"driver"`
	DeletionPolicy string         `json:"deletionPolicy"`
}

type snapshotPVCObject struct {
	Metadata kubeObjectMeta `json:"metadata"`
	Status   struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// KubeVolumeSnapshots reads CSI snapshots and freshness for namespace (empty means all).
func KubeVolumeSnapshots(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))
	if err := validateNamespace(namespace); err != nil {
		return "", err
	}
	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() volumeSnapshots {
		r := demoVolumeSnapshots(time.Now())
		r.Snapshots = inNamespace(r.Snapshots, namespace, func(s volumeSnapshot) string { return s.Namespace })
		r.Claims = inNamespace(r.Claims, namespace, func(s snapshotClaim) string { return s.Namespace })
		r.Contents = inNamespace(r.Contents, namespace, func(s snapshotContent) string { return s.Namespace })
		return r
	}, func(ctx context.Context, k *kubeClient) (volumeSnapshots, error) {
		return readVolumeSnapshots(ctx, k, namespace, time.Now()), nil
	})
}
func emptyVolumeSnapshots() volumeSnapshots {
	return volumeSnapshots{Snapshots: []volumeSnapshot{}, Contents: []snapshotContent{}, Classes: []snapshotClass{}, Claims: []snapshotClaim{}}
}
func pagedSnapshotObjects[T any](ctx context.Context, k *kubeClient, path string) ([]T, error) {
	out := []T{}
	err := k.listAll(ctx, path, pageQuery{}, func() { out = []T{} }, func(p kubePage) error {
		items, err := decodeItems[T](p)
		out = append(out, items...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func readVolumeSnapshots(ctx context.Context, k *kubeClient, namespace string, now time.Time) volumeSnapshots {
	r := emptyVolumeSnapshots()
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	v, ok := groups[groupSnapshot]
	if !ok {
		return r
	}
	r.Installed = true
	r.Version = v
	base := "/apis/" + groupSnapshot + "/" + v
	objs, err := pagedSnapshotObjects[snapshotObject](ctx, k, scopedPath(base, namespace, "volumesnapshots"))
	if err != nil {
		r.Error = err.Error()
	}
	contents, err := pagedSnapshotObjects[snapshotContentObject](ctx, k, base+"/volumesnapshotcontents")
	if err != nil {
		r.ContentsError = err.Error()
	}
	classes, err := pagedSnapshotObjects[snapshotClassObject](ctx, k, base+"/volumesnapshotclasses")
	if err != nil {
		r.ClassesError = err.Error()
	}
	claims, err := pagedSnapshotObjects[snapshotPVCObject](ctx, k, scopedPath("/api/v1", namespace, "persistentvolumeclaims"))
	if err != nil {
		r.ClaimsError = err.Error()
	}
	for _, c := range classes {
		r.Classes = append(r.Classes, snapshotClass{Name: c.Metadata.Name, Driver: c.Driver, DeletionPolicy: c.DeletionPolicy, Default: c.Metadata.Annotations[snapshotDefaultKey] == "true"})
	}
	for _, c := range contents {
		if namespace != "" && c.Spec.Ref.Namespace != namespace {
			continue
		}
		row := snapshotContent{Name: c.Metadata.Name, Namespace: c.Spec.Ref.Namespace, Snapshot: c.Spec.Ref.Name, Class: c.Spec.Class, Driver: c.Spec.Driver, Ready: c.Status.Ready}
		if c.Status.CreatedAt > 0 {
			row.CreatedAt = time.Unix(0, c.Status.CreatedAt).UnixMilli()
		}
		if c.Status.Error != nil {
			row.Error = c.Status.Error.Message
		}
		r.Contents = append(r.Contents, row)
	}
	byClaim := map[string]*snapshotClaim{}
	for _, c := range claims {
		if c.Status.Phase != "Bound" {
			continue
		}
		m := c.Metadata
		setting := m.Annotations[backupMatchKey]
		if setting == "" {
			setting = m.Labels[backupMatchKey]
		}
		row := &snapshotClaim{Namespace: m.Namespace, Name: m.Name, Watched: setting == "true"}
		if !m.CreationTimestamp.IsZero() {
			row.CreatedAt = m.CreationTimestamp.UnixMilli()
		}
		byClaim[m.Namespace+"/"+m.Name] = row
	}
	for _, s := range objs {
		row := volumeSnapshot{Namespace: s.Metadata.Namespace, Name: s.Metadata.Name, PVC: s.Spec.Source.PVC, Class: s.Spec.Class, Content: s.Status.Content, Ready: s.Status.Ready}
		if row.Content == "" {
			row.Content = s.Spec.Source.Content
		}
		if s.Status.CreatedAt != nil {
			row.CreatedAt = s.Status.CreatedAt.UnixMilli()
		}
		row.AgeSeconds = backupAge(row.CreatedAt, now)
		if s.Status.Error != nil {
			row.Error = s.Status.Error.Message
		}
		r.Snapshots = append(r.Snapshots, row)
		if c := byClaim[row.Namespace+"/"+row.PVC]; c != nil {
			c.Watched = true
			if row.Ready && row.Error == "" && s.Metadata.DeletionTimestamp == nil && row.CreatedAt > c.LastGoodAt {
				c.LastGoodAt = row.CreatedAt
			}
		}
	}
	for _, c := range byClaim {
		setSnapshotFreshness(c, now)
		// An incomplete snapshot list cannot prove that a backup is missing.
		if r.Error != "" {
			c.Stale = false
		}
		r.Claims = append(r.Claims, *c)
	}
	slices.SortFunc(r.Snapshots, func(a, b volumeSnapshot) int { return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name) })
	slices.SortFunc(r.Claims, func(a, b snapshotClaim) int { return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name) })
	slices.SortFunc(r.Contents, func(a, b snapshotContent) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(r.Classes, func(a, b snapshotClass) int { return strings.Compare(a.Name, b.Name) })
	privacy.learnNamespaces(namespacesOf(r.Claims, func(c snapshotClaim) string { return c.Namespace }))
	return r
}
func backupAge(at int64, now time.Time) int64 {
	if at <= 0 {
		return -1
	}
	return max(int64(0), (now.UnixMilli()-at)/1000)
}
func setSnapshotFreshness(c *snapshotClaim, now time.Time) {
	c.AgeSeconds = backupAge(c.LastGoodAt, now)
	c.Stale = c.Watched && (c.LastGoodAt == 0 || c.AgeSeconds >= int64((48*time.Hour)/time.Second))
	if c.LastGoodAt == 0 && c.CreatedAt > 0 && backupAge(c.CreatedAt, now) < int64((48*time.Hour)/time.Second) {
		c.Stale = false
	}
}

// KubeCreateVolumeSnapshot requests a snapshot using the PVC driver's unique default class.
func KubeCreateVolumeSnapshot(configYAML, contextName, kubeServer, namespace, pvc string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	namespace, pvc = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pvc))
	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "create-volume-snapshot", Namespace: namespace, Object: "PersistentVolumeClaim/" + pvc})
	if err := validateKubeName("PVC", namespace, pvc); err != nil {
		return "", err
	}
	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		out, err = createVolumeSnapshot(ctx, k, namespace, pvc)
		return err
	})
	return out, err
}
func createVolumeSnapshot(ctx context.Context, k *kubeClient, namespace, pvc string) (string, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return "", err
	}
	v, ok := groups[groupSnapshot]
	if !ok {
		return "", errors.New("CSI snapshot API is not installed")
	}
	var claim storageClaimObject
	if err := k.get(ctx, scopedPath("/api/v1", namespace, "persistentvolumeclaims")+"/"+url.PathEscape(pvc), &claim); err != nil {
		return "", err
	}
	if claim.Status.Phase != "Bound" || claim.Spec.StorageClassName == "" {
		return "", errors.New("snapshot requires a Bound PVC with a StorageClass")
	}
	var class storageClassInfo
	if err := k.get(ctx, "/apis/storage.k8s.io/v1/storageclasses/"+url.PathEscape(claim.Spec.StorageClassName), &class); err != nil {
		return "", err
	}
	base := "/apis/" + groupSnapshot + "/" + v
	classes, err := pagedSnapshotObjects[snapshotClassObject](ctx, k, base+"/volumesnapshotclasses")
	if err != nil {
		return "", err
	}
	selected := ""
	for _, c := range classes {
		if c.Driver == class.Provisioner && c.Metadata.Annotations[snapshotDefaultKey] == "true" {
			if selected != "" {
				return "", errors.New("multiple default snapshot classes match the PVC driver")
			}
			selected = c.Metadata.Name
		}
	}
	if selected == "" {
		return "", fmt.Errorf("no default snapshot class for driver %s", class.Provisioner)
	}
	payload := map[string]any{"apiVersion": groupSnapshot + "/" + v, "kind": "VolumeSnapshot", "metadata": map[string]any{"generateName": "ichor-snapshot-", "namespace": namespace}, "spec": map[string]any{"volumeSnapshotClassName": selected, "source": map[string]any{"persistentVolumeClaimName": pvc}}}
	var created struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := k.post(ctx, scopedPath(base, namespace, "volumesnapshots"), payload, &created); err != nil {
		return "", err
	}
	return created.Metadata.Name, nil
}
