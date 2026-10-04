package ichorgo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// The cluster-wide upgrade lock is a Kubernetes Lease: two Ichor apps (two phones, or a
// phone that lost track of its own run) cannot upgrade nodes of the same cluster at once.
// talosctl does not know it: peerBlockers and the plan re-run right before the request
// remain the guard against a talosctl upgrade.
const (
	upgradeLockNamespace = "kube-system"
	upgradeLockName      = "ichor-talos-upgrade"
	// upgradeLockDuration outlives a followed upgrade: a run the app could not release (the
	// app was killed, the user stopped following) expires on its own.
	upgradeLockDuration = upgradeTimeout + 10*time.Minute

	leaseTimeFormat = "2006-01-02T15:04:05.000000Z07:00" // metav1.MicroTime

	annotationNode     = "ichor.levis.name/node"
	annotationHostname = "ichor.levis.name/hostname"
	annotationFrom     = "ichor.levis.name/from-version"
	annotationTo       = "ichor.levis.name/to-version"
)

var leasesPath = "/apis/coordination.k8s.io/v1/namespaces/" + upgradeLockNamespace + "/leases"

type kubeLease struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Metadata   leaseMetadata `json:"metadata"`
	Spec       leaseSpec     `json:"spec"`
}

type leaseMetadata struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace,omitempty"`
	UID             string            `json:"uid,omitempty"`
	ResourceVersion string            `json:"resourceVersion,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
}

type leaseSpec struct {
	HolderIdentity       string `json:"holderIdentity,omitempty"`
	LeaseDurationSeconds int    `json:"leaseDurationSeconds,omitempty"`
	AcquireTime          string `json:"acquireTime,omitempty"`
	RenewTime            string `json:"renewTime,omitempty"`
}

// leaseStore is the Lease API the lock needs; tests run it against a fake.
type leaseStore interface {
	// get returns the lock's Lease, nil when there is none.
	get(ctx context.Context) (*kubeLease, error)
	// create fails with errLeaseConflict when the Lease exists.
	create(ctx context.Context, l kubeLease) (*kubeLease, error)
	// replace fails with errLeaseConflict when l's resourceVersion is not the current one.
	replace(ctx context.Context, l kubeLease) (*kubeLease, error)
	// delete removes the Lease only if its uid and resourceVersion still match.
	delete(ctx context.Context, uid, resourceVersion string) error
}

var errLeaseConflict = errors.New("the upgrade lock changed meanwhile")

// upgradeLockInfo is who holds the lock, for the messages.
type upgradeLockInfo struct {
	holder   string
	node     string
	hostname string
	from, to string
	since    time.Time
	expires  time.Time
}

func (i upgradeLockInfo) describe() string {
	who := i.hostname
	if who == "" {
		who = i.node
	}

	if who == "" {
		who = "unknown node"
	}

	upgrade := who
	if i.to != "" {
		upgrade += " to " + i.to
	}

	return fmt.Sprintf("another Ichor upgrade (%s, started %s) holds the cluster upgrade lock until %s",
		upgrade, i.since.Local().Format("15:04"), i.expires.Local().Format("15:04"))
}

// heldLock returns who holds l, nil when it is free or expired.
func heldLock(l *kubeLease, now time.Time) *upgradeLockInfo {
	if l == nil || l.Spec.HolderIdentity == "" {
		return nil
	}

	renew, err := time.Parse(time.RFC3339Nano, l.Spec.RenewTime)
	if err != nil {
		return nil
	}

	expires := renew.Add(time.Duration(l.Spec.LeaseDurationSeconds) * time.Second)
	if !now.Before(expires) {
		return nil
	}

	since, err := time.Parse(time.RFC3339Nano, l.Spec.AcquireTime)
	if err != nil {
		since = renew
	}

	a := l.Metadata.Annotations

	return &upgradeLockInfo{
		holder: l.Spec.HolderIdentity, node: a[annotationNode], hostname: a[annotationHostname],
		from: a[annotationFrom], to: a[annotationTo], since: since, expires: expires,
	}
}

// lockSettled tells the upgrade a held lock guards is visibly over: its node runs the
// target version and is ready, so a run that could not release it does not block for
// upgradeLockDuration. A reinstall (from == to) cannot be told apart, so it stays held.
func lockSettled(info *upgradeLockInfo, peers []planPeer) bool {
	if info.to == "" || sameVersion(info.from, info.to) {
		return false
	}

	for _, p := range peers {
		if p.node == info.node {
			return p.reachable && p.ready && p.stage == runtime.MachineStageRunning.String() && sameVersion(p.version, info.to)
		}
	}

	return false
}

// upgradeLockRequest is the run that wants the lock.
type upgradeLockRequest struct {
	holder         string
	node, hostname string
	from, to       string
}

func newLockHolder() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)

	return "ichor/" + hex.EncodeToString(b)
}

func (r upgradeLockRequest) lease(now time.Time, base *kubeLease) kubeLease {
	l := kubeLease{APIVersion: "coordination.k8s.io/v1", Kind: "Lease"}
	if base != nil {
		l.Metadata = base.Metadata
	}

	l.Metadata.Name, l.Metadata.Namespace = upgradeLockName, upgradeLockNamespace
	l.Metadata.Annotations = map[string]string{
		annotationNode: r.node, annotationHostname: r.hostname, annotationFrom: r.from, annotationTo: r.to,
	}

	stamp := now.UTC().Format(leaseTimeFormat)
	l.Spec = leaseSpec{
		HolderIdentity:       r.holder,
		LeaseDurationSeconds: int(upgradeLockDuration / time.Second),
		AcquireTime:          stamp,
		RenewTime:            stamp,
	}

	return l
}

// lockResult is the outcome of acquireUpgradeLock: held is set when someone else has it.
type lockResult struct {
	lease *kubeLease
	held  *upgradeLockInfo
}

// acquireUpgradeLock takes the lock for r, or reports who holds it. settled tells whether
// a held lock's upgrade is over (see lockSettled), which frees it.
func acquireUpgradeLock(ctx context.Context, s leaseStore, r upgradeLockRequest, settled func(*upgradeLockInfo) bool, now time.Time) (lockResult, error) {
	created, err := s.create(ctx, r.lease(now, nil))
	if err == nil {
		return lockResult{lease: created}, nil
	}

	if !errors.Is(err, errLeaseConflict) {
		return lockResult{}, err
	}

	current, err := s.get(ctx)
	if err != nil {
		return lockResult{}, err
	}

	if current == nil {
		return lockResult{}, errLeaseConflict // deleted between create and get: retry later
	}

	if info := heldLock(current, now); info != nil && info.holder != r.holder && !settled(info) {
		return lockResult{held: info}, nil
	}

	replaced, err := s.replace(ctx, r.lease(now, current))
	if err != nil {
		return lockResult{}, err
	}

	return lockResult{lease: replaced}, nil
}

// releaseUpgradeLock frees the lock if it is still the one l took.
func releaseUpgradeLock(ctx context.Context, s leaseStore, l *kubeLease) error {
	err := s.delete(ctx, l.Metadata.UID, l.Metadata.ResourceVersion)
	if errors.Is(err, errLeaseConflict) {
		return nil // taken over after expiring: not ours anymore
	}

	return err
}

// kubeLeaseStore is leaseStore over the Kubernetes API.
type kubeLeaseStore struct{ k *kubeClient }

func leaseConflict(err error) error {
	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
		return errLeaseConflict
	}

	return err
}

func (s kubeLeaseStore) get(ctx context.Context) (*kubeLease, error) {
	var l kubeLease

	err := s.k.get(ctx, leasesPath+"/"+url.PathEscape(upgradeLockName), &l)

	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &l, nil
}

func (s kubeLeaseStore) create(ctx context.Context, l kubeLease) (*kubeLease, error) {
	var out kubeLease
	if err := s.k.post(ctx, leasesPath, l, &out); err != nil {
		return nil, leaseConflict(err)
	}

	return &out, nil
}

func (s kubeLeaseStore) replace(ctx context.Context, l kubeLease) (*kubeLease, error) {
	data, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}

	var out kubeLease
	if err := s.k.do(ctx, http.MethodPut, leasesPath+"/"+url.PathEscape(upgradeLockName), "application/json", data, &out); err != nil {
		return nil, leaseConflict(err)
	}

	return &out, nil
}

func (s kubeLeaseStore) delete(ctx context.Context, uid, resourceVersion string) error {
	data, err := json.Marshal(map[string]any{
		"apiVersion": "v1", "kind": "DeleteOptions",
		"preconditions": map[string]string{"uid": uid, "resourceVersion": resourceVersion},
	})
	if err != nil {
		return err
	}

	err = s.k.do(ctx, http.MethodDelete, leasesPath+"/"+url.PathEscape(upgradeLockName), "application/json", data, nil)

	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
		return nil
	}

	return leaseConflict(err)
}

// lockState is the lock as UpgradePlan sees it: held by someone else, or unreadable.
type lockState struct {
	held *upgradeLockInfo
	err  string
}

// readUpgradeLock reads the lock for the plan; a zero target (demo, tests) skips it.
func readUpgradeLock(ctx context.Context, kube kubeTarget, now time.Time) *lockState {
	if kube.config == "" || isDemoContext(kube.config, kube.context) {
		return nil
	}

	l, err := withKubeContext(ctx, kube, func(ctx context.Context, k *kubeClient) (*kubeLease, error) {
		return kubeLeaseStore{k}.get(ctx)
	})
	if err != nil {
		return &lockState{err: err.Error()}
	}

	return &lockState{held: heldLock(l, now)}
}

// upgradeLock is the lock a run holds; release frees it (a no-op when none was taken).
type upgradeLock struct {
	kube  kubeTarget
	lease *kubeLease
}

// takeUpgradeLock acquires the lock for r. It refuses with an error when another run holds
// it; when the Kubernetes API cannot be used, it warns through note and goes on without a
// lock: the upgrade may be what repairs the cluster.
func takeUpgradeLock(ctx context.Context, kube kubeTarget, r upgradeLockRequest, settled func(*upgradeLockInfo) bool, note func(string)) (*upgradeLock, error) {
	lock := &upgradeLock{kube: kube}
	if kube.config == "" || isDemoContext(kube.config, kube.context) {
		return lock, nil
	}

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	res, err := withKubeContext(ctx, kube, func(ctx context.Context, k *kubeClient) (lockResult, error) {
		return acquireUpgradeLock(ctx, kubeLeaseStore{k}, r, settled, time.Now())
	})

	switch {
	case err != nil:
		note("no cluster-wide upgrade lock taken (" + err.Error() + "): make sure nobody else upgrades this cluster now")
	case res.held != nil:
		return nil, errors.New("upgrade refused: " + res.held.describe())
	default:
		lock.lease = res.lease
	}

	return lock, nil
}

func (l *upgradeLock) release() {
	if l == nil || l.lease == nil {
		return
	}

	_, _ = withKube(l.kube, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, releaseUpgradeLock(ctx, kubeLeaseStore{k}, l.lease)
	})
	l.lease = nil
}
