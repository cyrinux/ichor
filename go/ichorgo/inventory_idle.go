package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"
)

// An app with no running container is not left out of the inventory: its Deployments and
// StatefulSets without a ready pod name it. Scaled to zero, it is stopped (on purpose); wanting
// pods none of which is ready, it is not starting (a volume that does not attach, an image that
// does not pull, a crash loop).
const (
	appStopped     = "stopped"
	appNotStarting = "notStarting"
)

const (
	// idleReasonsMax bounds the stuck pods whose events are read for a reason.
	idleReasonsMax = 8
	// idleGrace is how long a Talos inventory waits for the idle workloads once its containers
	// are listed: a Kubernetes API slower than that (a cold client) leaves them out this time.
	idleGrace = 3 * time.Second
)

// idleKinds are the workloads read: a DaemonSet with nothing to schedule is not an app stopped.
var idleKinds = []workloadKind{workloadKinds[0], workloadKinds[1]}

// inventoryWorkload is an idle workload of an app, for the app's sheet to show (and scale).
type inventoryWorkload struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Desired   int32  `json:"desired"`
	// Reason is why its pods do not start: the kubelet's message or the pod's last warning
	// event ("" when none is known, and for a stopped one).
	Reason string `json:"reason,omitempty"`
}

// idleWorkload is a workload with no ready pod and its template's images.
type idleWorkload struct {
	inventoryWorkload
	images []string
	ready  int32
}

func (w idleWorkload) state() string {
	if w.Desired == 0 {
		return appStopped
	}

	return appNotStarting
}

// startIdleRead reads the idle workloads of a Talos cluster in the background, through target;
// the function it returns waits for them up to idleGrace. Nil when the API refuses (a reader
// role, a sign-in needed) or does not answer in time.
func startIdleRead(target kubeTarget) func() []idleWorkload {
	return inBackground(func() []idleWorkload {
		ctx, cancel := context.WithTimeout(context.Background(), inventoryTimeout)
		defer cancel()

		idle, _ := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) ([]idleWorkload, error) {
			idle, err := readIdleWorkloads(ctx, k)
			if errors.Is(err, context.DeadlineExceeded) {
				// Slow, not broken: the client other screens share stays.
				err = context.Canceled
			}

			return idle, err
		}) //nolint:errcheck

		return idle
	})
}

// inBackground runs read in a goroutine; the function it returns waits for its result up to
// idleGrace, nil past it. A panic in read gives nil at once rather than aborting the app.
func inBackground(read func() []idleWorkload) func() []idleWorkload {
	done := make(chan []idleWorkload, 1)

	go func() {
		var idle []idleWorkload

		defer func() {
			_ = recover()
			done <- idle
		}()

		idle = read()
	}()

	return func() []idleWorkload {
		select {
		case idle := <-done:
			return idle
		case <-time.After(idleGrace):
			return nil
		}
	}
}

// readIdleWorkloads lists the Deployments and StatefulSets without a ready pod, with the reason
// of those that want some. A kind the role may not list leaves the other.
func readIdleWorkloads(ctx context.Context, k *kubeClient) ([]idleWorkload, error) {
	idle, errs := listWorkloadsWhere(ctx, k, idleKinds, func(w kubeWorkload) bool { return w.Ready == 0 })

	if !slices.Contains(errs, nil) {
		return nil, errors.Join(errs...)
	}

	addIdleReasons(ctx, k, idle)

	return idle, nil
}

// listWorkloadsWhere lists the workloads of kinds that keep takes, and each kind's error.
func listWorkloadsWhere(ctx context.Context, k *kubeClient, kinds []workloadKind, keep func(kubeWorkload) bool) ([]idleWorkload, []error) {
	results := make([][]idleWorkload, len(kinds))
	errs := make([]error, len(kinds))

	forEachLimit(kinds, len(kinds), func(i int, kind workloadKind) {
		errs[i] = errors.New("not read") // a panic keeps it
		errs[i] = k.listAll(ctx, "/apis/apps/v1/"+kind.resource, pageQuery{}, func() { results[i] = nil }, func(page kubePage) error {
			objs, err := decodeItems[appsObject](page)
			for _, obj := range objs {
				if w := mapWorkload(kind.kind, obj); keep(w) {
					results[i] = append(results[i], idleWorkload{
						inventoryWorkload: inventoryWorkload{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, Desired: w.Desired},
						images:            w.Images,
						ready:             w.Ready,
					})
				}
			}

			return err
		})
	})

	return slices.Concat(results...), errs
}

// addIdleReasons explains the workloads that want pods from their unhealthy pods: the kubelet's
// waiting message, else the pod's last warning event (FailedAttachVolume, FailedScheduling),
// else its status. Best effort: a read that fails leaves the reasons empty.
func addIdleReasons(ctx context.Context, k *kubeClient, idle []idleWorkload) {
	var namespaces []string

	for _, w := range idle {
		if w.Desired > 0 {
			namespaces = append(namespaces, w.Namespace)
		}
	}

	if len(namespaces) == 0 {
		return
	}

	pods, err := unhealthyPods(ctx, k, namespaces)
	if err != nil {
		return
	}

	var stuck []int // idle workloads with a pod but no message yet

	stuckPod := map[int]kubePod{}

	for i := range idle {
		if idle[i].Desired == 0 {
			continue
		}

		at := stuckPodOf(pods, idle[i].inventoryWorkload)
		if at < 0 {
			continue
		}

		if idle[i].Reason = pods[at].Waiting; idle[i].Reason == "" {
			idle[i].Reason = pods[at].Status
			stuck = append(stuck, i)
			stuckPod[i] = pods[at]
		}
	}

	if len(stuck) > idleReasonsMax {
		stuck = stuck[:idleReasonsMax]
	}

	forEachLimit(stuck, unhealthyPodsParallel, func(_ int, i int) {
		p := stuckPod[i]

		events, err := readEvents(ctx, k, p.Namespace, "Pod", p.Name)
		if err != nil {
			return
		}

		// Newest first.
		if e := slices.IndexFunc(events.Events, func(e kubeEvent) bool { return e.Type == "Warning" }); e >= 0 {
			idle[i].Reason = events.Events[e].Message
		}
	})
}

// stuckPodOf is the index of the pod that explains w, -1 when none: one being replaced by the
// controller (Terminating) only when no other is there, its replacement says more.
func stuckPodOf(pods []kubePod, w inventoryWorkload) int {
	first := -1

	for i, p := range pods {
		if !podOf(p, w) {
			continue
		}

		if p.Status != "Terminating" {
			return i
		}

		if first < 0 {
			first = i
		}
	}

	return first
}

// podOf tells a pod of the workload: a StatefulSet or a DaemonSet owns its pods, a Deployment
// through a ReplicaSet named <deployment>-<pod-template-hash>.
func podOf(p kubePod, w inventoryWorkload) bool {
	if p.Namespace != w.Namespace {
		return false
	}

	kind, name, _ := strings.Cut(p.Owner, "/")

	switch w.Kind {
	case "StatefulSet", "DaemonSet":
		return kind == w.Kind && name == w.Name
	default:
		rest, ok := strings.CutPrefix(name, w.Name+"-")

		return kind == "ReplicaSet" && ok && !strings.Contains(rest, "-")
	}
}

// withIdleWorkloads adds the apps of idle workloads to inv: identified from their template's
// images like a pod's, an app already listed that runs nothing takes their state, one not
// listed (its pods never got a container) is added without containers.
func withIdleWorkloads(inv inventory, idle []idleWorkload) inventory {
	apps := slices.Clone(inv.Apps)
	index := map[string]int{}

	for i, a := range apps {
		index[a.ID] = i
	}

	for _, w := range idle {
		app, ok := idleApp(w)
		if !ok {
			continue
		}

		i, listed := index[app.ID]
		if !listed {
			index[app.ID] = len(apps)
			apps = append(apps, app)

			continue
		}

		if apps[i].Running > 0 {
			continue
		}

		apps[i] = joinIdle(apps[i], w)
	}

	slices.SortFunc(apps, func(a, b inventoryApp) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})

	inv.Apps = apps

	return inv
}

// joinIdle marks app with the idle workload: not starting wins over stopped.
func joinIdle(app inventoryApp, w idleWorkload) inventoryApp {
	if app.State != appNotStarting {
		app.State = w.state()
	}

	if app.Reason == "" {
		app.Reason = w.Reason
	}

	app.Workloads = append(slices.Clone(app.Workloads), w.inventoryWorkload)

	return app
}

// idleApp is the app a workload's template runs, as the inventory would name its pod, without
// a container counted.
func idleApp(w idleWorkload) (inventoryApp, bool) {
	if len(w.images) == 0 {
		return inventoryApp{}, false
	}

	containers := make([]containerInfo, len(w.images))
	for i, image := range w.images {
		containers[i] = containerInfo{Namespace: containerNSK8s, PodNamespace: w.Namespace, Pod: w.Name, Image: image}
	}

	placed := identifyContainers(loadAppCatalog(), []nodeContainers{{Containers: containers}})
	keys := groupKeys(placed)

	b := newAppBuilder(appIdentity(placed[0]))
	for i, p := range placed {
		if keys[i] == keys[0] {
			b.add(p)
		}
	}

	app := b.build()
	app.ID = keys[0]
	app.Containers, app.Nodes, app.Pods = 0, []string{}, []inventoryPod{}

	images := make([]inventoryImage, len(app.Images))
	for i, img := range app.Images {
		img.Containers = 0
		images[i] = img
	}

	app.Images = images

	return joinIdle(app, w), true
}
