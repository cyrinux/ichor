package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Health of one data-service item (a Longhorn volume, a Postgres cluster), worst first.
const (
	healthCritical = "critical"
	healthWarning  = "warning"
	healthOK       = "ok"
	healthIdle     = "idle" // not in use: a detached volume
)

// healthRank orders health values for sorting, worst first.
func healthRank(h string) int {
	switch h {
	case healthCritical:
		return 0
	case healthWarning:
		return 1
	case healthOK:
		return 2
	default:
		return 3
	}
}

// byHealthThenKey orders items worst health first, then by the stable key f returns
// (namespace/name, usually), for slices.SortFunc.
func byHealthThenKey[T any](f func(T) (health, key string)) func(a, b T) int {
	return func(a, b T) int {
		ah, ak := f(a)
		bh, bk := f(b)

		return cmp.Or(healthRank(ah)-healthRank(bh), strings.Compare(ak, bk))
	}
}

// dataServices is the health of the storage and database operators a cluster runs. A nil
// section is a system that is not installed.
type dataServices struct {
	Longhorn    *longhornStatus    `json:"longhorn,omitempty"`
	Garage      *garageStatus      `json:"garage,omitempty"`
	CNPG        *cnpgStatus        `json:"cnpg,omitempty"`
	Dragonfly   *dragonflyStatus   `json:"dragonfly,omitempty"`
	MariaDB     *mariadbStatus     `json:"mariadb,omitempty"`
	Percona     *perconaStatus     `json:"percona,omitempty"`
	CertManager *certManagerStatus `json:"certManager,omitempty"`
	Velero      *veleroStatus      `json:"velero,omitempty"`
	Ceph        *cephStatus        `json:"ceph,omitempty"`
	CastAI      *castAIStatus      `json:"castai,omitempty"`
}

// KubeDataServices reports the health of the storage and database operators the cluster
// runs (the sections of dataServices), through the Kubernetes API with the admin kubeconfig
// Talos issues (os:admin): the operators from their custom resources, Garage by running its
// own CLI (`garage json-api`) in one of its pods.
//
// hints is a comma-separated list of catalog app ids the app saw in the inventory
// ("longhorn,garage,cloudnative-pg"); "" checks everything. The operators are found by their
// API groups (their CRDs) either way; Garage, which has no API of its own, is only
// looked for (a listing of every pod) when hinted or when hints is "".
// kubeServer: see KubePods.
func KubeDataServices(configYAML, contextName, kubeServer, hints string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	demo := func() dataServices { return demoDataServices(time.Now()) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (dataServices, error) {
		return readDataServices(ctx, k, k.exec, parseHints(hints), time.Now())
	})
}

// execFunc runs a command in a container: kubeClient.exec, or a fake in tests.
type execFunc func(ctx context.Context, namespace, pod, container string, argv []string) (stdout, stderr []byte, err error)

type hintSet map[string]bool

func parseHints(hints string) hintSet {
	set := hintSet{}

	for _, h := range splitCSV(hints) {
		set[h] = true
	}

	return set
}

// wants reports whether the app id was hinted, or no hint was given at all.
func (h hintSet) wants(id string) bool { return len(h) == 0 || h[id] }

// API groups of the data services, with the version the reader uses.
const (
	groupLonghorn   = "longhorn.io"
	groupCNPG       = "postgresql.cnpg.io"
	groupBarmanPlug = "barmancloud.cnpg.io"
)

// readDataServices detects what is installed and reads each system in parallel. It fails
// only when nothing could be asked (the API groups); a system that fails on its own carries
// its error in its section.
func readDataServices(ctx context.Context, k *kubeClient, run execFunc, hints hintSet, now time.Time) (dataServices, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return dataServices{}, err
	}

	var (
		out       dataServices
		pods      []dsPod
		podsErr   error
		listedAll bool
	)

	if hints.wants("garage") {
		pods, podsErr = listDSPods(ctx, k, "")
		listedAll = podsErr == nil
	}

	var wg sync.WaitGroup

	if v, ok := groups[groupLonghorn]; ok {
		wg.Go(func() { out.Longhorn = readLonghorn(ctx, k, v) })
	}

	if v, ok := groups[groupCNPG]; ok {
		wg.Go(func() {
			cnpgPods, err := pods, error(nil)
			if !listedAll {
				cnpgPods, err = listDSPods(ctx, k, "cnpg.io/cluster")
			}

			out.CNPG = readCNPG(ctx, k, cnpgSources{version: v, pluginVersion: groups[groupBarmanPlug], pods: cnpgPods, podsErr: err}, now)
		})
	}

	if v, ok := groups[groupDragonfly]; ok {
		wg.Go(func() { out.Dragonfly = readDragonfly(ctx, k, v) })
	}

	if v, ok := groups[groupMariaDB]; ok {
		wg.Go(func() { out.MariaDB = readMariaDB(ctx, k, v, now) })
	}

	if v, ok := groups[groupPercona]; ok {
		wg.Go(func() { out.Percona = readPercona(ctx, k, v, now) })
	}

	if v, ok := groups[groupCertManager]; ok {
		wg.Go(func() { out.CertManager = readCertManager(ctx, k, v, now) })
	}

	if _, ok := groups[groupVelero]; ok {
		wg.Go(func() { out.Velero = readVelero(ctx, k, now) })
	}

	if v, ok := groups[groupCeph]; ok {
		wg.Go(func() { out.Ceph = readCeph(ctx, k, v) })
	}

	if _, ok := groups[groupCastAI]; ok {
		wg.Go(func() { out.CastAI = readCastAI(ctx, k) })
	}

	if hints.wants("garage") {
		wg.Go(func() { out.Garage = readGarage(ctx, k, run, pods, podsErr, hints["garage"]) })
	}

	wg.Wait()

	return out, nil
}

// readAPIGroups lists the API groups the server serves, with their preferred versions.
func readAPIGroups(ctx context.Context, k *kubeClient) (map[string]string, error) {
	var list struct {
		Groups []struct {
			Name             string `json:"name"`
			PreferredVersion struct {
				Version string `json:"version"`
			} `json:"preferredVersion"`
		} `json:"groups"`
	}

	if err := k.get(ctx, "/apis", &list); err != nil {
		return nil, err
	}

	groups := map[string]string{}
	for _, g := range list.Groups {
		groups[g.Name] = g.PreferredVersion.Version
	}

	return groups, nil
}

// dsPod holds the fields of a pod the data-service readers use.
type dsPod struct {
	Metadata struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		Labels          map[string]string `json:"labels"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// hasContainer tells whether the pod has a container named container.
func (p dsPod) hasContainer(container string) bool {
	for _, c := range p.Spec.Containers {
		if c.Name == container {
			return true
		}
	}

	return false
}

// containerReady reports whether the pod runs with container ready ("" for every container).
func (p dsPod) containerReady(container string) bool {
	if p.Status.Phase != "Running" {
		return false
	}

	found := false

	for _, cs := range p.Status.ContainerStatuses {
		if container != "" && cs.Name != container {
			continue
		}

		if !cs.Ready {
			return false
		}

		found = true
	}

	return found
}

// listDSPods lists the pods of every namespace, those matching the label selector when set
// ("cnpg.io/cluster" for a key, "app.kubernetes.io/name=dragonfly" for a value).
func listDSPods(ctx context.Context, k *kubeClient, selector string) ([]dsPod, error) {
	path := "/api/v1/pods"
	if selector != "" {
		path += "?labelSelector=" + url.QueryEscape(selector)
	}

	var list kubeList[dsPod]

	if err := getList(ctx, k, path, &list); err != nil {
		return nil, err
	}

	slices.SortFunc(list.Items, func(a, b dsPod) int {
		if c := strings.Compare(a.Metadata.Namespace, b.Metadata.Namespace); c != 0 {
			return c
		}

		return strings.Compare(a.Metadata.Name, b.Metadata.Name)
	})

	return list.Items, nil
}

// sectionError is the message of a section that failed, for its "error" field.
func sectionError(errs ...error) string {
	var msgs []string

	for _, err := range errs {
		if err != nil {
			msgs = append(msgs, kubeError(err).Error())
		}
	}

	return strings.Join(slices.Compact(msgs), "; ")
}

// unixMilli parses an RFC 3339 time into unix ms, 0 when empty or invalid.
func unixMilli(s string) int64 {
	if s == "" {
		return 0
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}

	return t.UnixMilli()
}
