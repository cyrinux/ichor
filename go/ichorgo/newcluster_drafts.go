package ichorgo

// Step 5 of the Talos lifecycle, the preparing half: NewClusterPrepare turns a spec the
// user typed into the cluster's secrets and configs, kept in Go memory behind a draft id
// (gomobile passes strings only, so the bundle never crosses into the apps). The apps show
// a redacted preview and store the draft's talosconfig, then StartNewCluster sends the
// config to the maintenance-mode node and follows it. Nothing here reaches the network.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	machineryconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// newClusterDraftTTL: a draft lives this long before it is forgotten; an app restart loses
// the drafts sooner, which is fine: nothing was sent, the person starts again.
const newClusterDraftTTL = 30 * time.Minute

// newClusterSpec is what the apps send NewClusterPrepare (JSON, gomobile-friendly).
type newClusterSpec struct {
	Name                          string   `json:"name"`
	Endpoint                      string   `json:"endpoint"`
	KubernetesVersion             string   `json:"kubernetesVersion"`
	TalosVersion                  string   `json:"talosVersion"`
	InstallDisk                   string   `json:"installDisk"`
	InstallImage                  string   `json:"installImage"`
	AllowSchedulingOnControlPlane bool     `json:"allowSchedulingOnControlPlanes"`
	AdditionalSANs                []string `json:"additionalSans"`
	ExtraEditsJSON                string   `json:"extraEditsJSON"`
}

// newClusterDraft is what NewClusterPrepare returns: the talosconfig (a credential, masked
// on errors) and the previews the apps show, never the secrets bundle.
type newClusterDraft struct {
	DraftID              string   `json:"draftId"`
	Talosconfig          string   `json:"talosconfig"`
	RedactedControlplane string   `json:"redactedControlplane"`
	RedactedWorker       string   `json:"redactedWorker"`
	Warnings             []string `json:"warnings"`
	Size                 int      `json:"size"`
	TalosVersion         string   `json:"talosVersion"`
	KubernetesVersion    string   `json:"kubernetesVersion"`
}

// newClusterDraftEntry is a prepared draft held in memory: the bundle and the validated
// request, keyed by the id the apps were given.
type newClusterDraftEntry struct {
	req      newClusterRequest
	bundle   *secrets.Bundle
	full     string // the control-plane config sent at apply time
	talos    string // the os:admin talosconfig the app stores before the run
	expires  time.Time
	consumed bool
}

var (
	newClusterDraftsMu sync.Mutex
	newClusterDrafts   = map[string]*newClusterDraftEntry{}
)

// NewClusterPrepare is the spec's preparing entry point. specJSON carries {"name",
// "endpoint","kubernetesVersion","talosVersion","installDisk","installImage",
// "allowSchedulingOnControlPlanes","additionalSans","extraEditsJSON"}. It makes the
// cluster's secrets and configs and keeps them in memory, returning the talosconfig the
// app must store before the run and the redacted previews. Refused in privacy mode; the
// secrets are never logged. Audited as new-cluster-generated (name and versions only).
func NewClusterPrepare(specJSON string) (out string, err error) {
	defer maskResult(&out, &err)
	defer func() {
		recordOutcome("", "", auditAction{
			Action: "new-cluster-generated",
			Params: auditNewClusterParams(specJSON),
		}, err)
	}()

	if privacy.isEnabled() {
		return "", errNewClusterPrivacy
	}

	var spec newClusterSpec
	if err := fromJSON(specJSON, &spec); err != nil {
		return "", err
	}

	req, err := parseNewClusterSpec(spec)
	if err != nil {
		return "", err
	}

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), machineryconfig.TalosVersionCurrent)
	if err != nil {
		return "", fmt.Errorf("generate the cluster's secrets: %s", friendlyError(err))
	}

	full, redactedCP, _, redactedWorker, talos, err := newClusterConfigs(req, bundle)
	if err != nil {
		return "", err
	}

	id, err := newClusterDraftID()
	if err != nil {
		return "", err
	}

	newClusterDraftsMu.Lock()
	newClusterDrafts[id] = &newClusterDraftEntry{
		req: req, bundle: bundle, full: full, talos: talos,
		expires: time.Now().Add(newClusterDraftTTL),
	}
	newClusterDraftsMu.Unlock()

	return toJSON(newClusterDraft{
		DraftID: id, Talosconfig: talos,
		RedactedControlplane: redactedCP, RedactedWorker: redactedWorker,
		Warnings: newClusterWarnings(req), Size: len(full),
		TalosVersion: spec.TalosVersion, KubernetesVersion: req.kubernetesVersion,
	})
}

// newClusterConfigs makes the node's control-plane and worker configs, the talosconfig and
// the redacted previews from a prepared bundle.
func newClusterConfigs(req newClusterRequest, bundle *secrets.Bundle) (full, redactedCP, worker, redactedWorker, talos string, err error) {
	in, err := generate.NewInput(req.clusterName, req.endpoint, req.kubernetesVersion,
		generate.WithSecretsBundle(bundle),
		generate.WithInstallDisk(req.installDisk),
		generate.WithInstallImage(req.installImage),
		generate.WithAllowSchedulingOnControlPlanes(true),
		generate.WithVersionContract(machineryconfig.TalosVersionCurrent),
	)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("make the cluster's config: %s", friendlyError(err))
	}

	cp, err := in.Config(machine.TypeControlPlane)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("make the control-plane config: %s", friendlyError(err))
	}

	wk, err := in.Config(machine.TypeWorker)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("make the worker config: %s", friendlyError(err))
	}

	tc, err := in.Talosconfig()
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("make the cluster's talosconfig: %s", friendlyError(err))
	}

	cpStr, err := cp.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", "", "", "", "", errors.New(friendlyError(err))
	}

	wkStr, err := wk.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", "", "", "", "", errors.New(friendlyError(err))
	}

	tcStr, err := tc.Bytes()
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("encode the talosconfig: %s", friendlyError(err))
	}

	rCP, err := redactConfig(cpStr)
	if err != nil {
		return "", "", "", "", "", errors.New(friendlyError(err))
	}

	rWk, err := redactConfig(wkStr)
	if err != nil {
		return "", "", "", "", "", errors.New(friendlyError(err))
	}

	return cpStr, rCP, wkStr, rWk, string(tcStr), nil
}

// newClusterWarnings is one line per thing the user must confirm before the run.
func newClusterWarnings(req newClusterRequest) []string {
	return []string{
		"this phone generates the cluster's CAs, tokens and keys: nothing is downloaded, and no secret of another cluster is involved",
		"they travel to the node inside the config, over TLS but unauthenticated: a node in maintenance mode cannot prove who it is",
		"make sure " + req.address + " is your node, on your own network: check the IP its console shows",
		"the node installs Talos to " + req.installDisk + " and becomes the single control plane of " + req.clusterName + "; that disk is erased",
		"store the talosconfig and the kubeconfig this run hands over: they are the only copy of the cluster's access",
	}
}

// auditNewClusterParams names what was prepared, never a secret: a spec that cannot be read
// records what it can.
func auditNewClusterParams(specJSON string) string {
	var spec newClusterSpec
	if err := fromJSON(specJSON, &spec); err != nil {
		return ""
	}

	return "name=" + strings.TrimSpace(spec.Name) + " k8s=" + strings.TrimPrefix(strings.TrimSpace(spec.KubernetesVersion), "v")
}

// newClusterDraftID is a random id naming a prepared draft.
func newClusterDraftID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New(friendlyError(err))
	}

	return hex.EncodeToString(b), nil
}

// takeNewClusterDraft hands the draft over once: a second StartNewCluster of the same id
// finds nothing (the entry is cleared on use, as the spec asks).
func takeNewClusterDraft(id string) (*newClusterDraftEntry, error) {
	newClusterDraftsMu.Lock()
	defer newClusterDraftsMu.Unlock()

	e, ok := newClusterDrafts[id]
	if !ok || e.consumed || time.Now().After(e.expires) {
		delete(newClusterDrafts, id)

		return nil, errors.New("this cluster draft is gone (it expires after 30 minutes, or once used): prepare the cluster again")
	}

	e.consumed = true
	delete(newClusterDrafts, id)

	return e, nil
}

// parseNewClusterSpec validates what NewClusterPrepare was asked for, reusing the request
// validation the run shares.
func parseNewClusterSpec(spec newClusterSpec) (newClusterRequest, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return newClusterRequest{}, errors.New("the cluster needs a name")
	}

	endpoint := strings.TrimSpace(spec.Endpoint)
	if endpoint == "" {
		return newClusterRequest{}, errors.New("the machine's address is required")
	}

	host := reqAddressHost(endpoint)
	if host == "" || strings.ContainsAny(host, "/ @?#") {
		return newClusterRequest{}, fmt.Errorf("%q is not a valid address", endpoint)
	}

	req, err := parseNewClusterRequest(endpoint, name, spec.KubernetesVersion, spec.InstallDisk, name, "")
	if err != nil {
		return newClusterRequest{}, err
	}

	req.installImage = strings.TrimSpace(spec.InstallImage)

	return req, nil
}

// StartNewCluster bootstraps a fresh single-node cluster on the node at address from a
// draft NewClusterPrepare made: the address is a maintenance-mode Talos (it is refused if
// it asks for a client certificate, i.e. belongs to a cluster), the run sends the draft's
// control-plane config over the insecure maintenance API, follows the node with the
// talosconfig the app stored (storedYAML) until apid answers, bootstraps etcd once, then
// waits for the Kubernetes API server. The app must have stored storedYAML already: it is
// the only copy of the cluster's access. Refused in privacy mode; the demo is refused with
// errDemoUnavailable. Audited as new-cluster-bootstrap (never the config). Bounded by
// newClusterTimeout; Cancel after the apply only stops following.
func StartNewCluster(draftID, address, storedYAML string, listener JoinListener) *NewClusterRun {
	listener = maskedJoinListener{listener}

	entry, err := takeNewClusterDraft(draftID)
	if err != nil {
		// No draft: nothing can be sent. Report it the way a run would, so the apps have
		// one shape (an OnDone with the message, never a panic).
		listener.OnDone("", err.Error())

		return &NewClusterRun{cancel: func() {}}
	}

	if privacy.isEnabled() {
		listener.OnDone("", errNewClusterPrivacy.Error())

		return &NewClusterRun{cancel: func() {}}
	}

	req := entry.req
	req.address = strings.TrimSpace(address)
	req.host = reqAddressHost(req.address)

	c := &newCluster{
		address: req.address, clusterName: req.clusterName, kubernetesVersion: req.kubernetesVersion,
		installDisk: req.installDisk, hostname: req.hostname, endpoint: req.endpoint,
		listener: maskedNewClusterListener{newClusterJoinListener{listener}},
		draft:    entry, storedYAML: storedYAML,
		interval: upgradePollInterval, timeout: newClusterTimeout,
	}

	ctx, cancel := context.WithTimeout(context.Background(), newClusterTimeout)
	run := &NewClusterRun{cancel: cancel}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })
		defer holdCalls("", "")()

		err := recordedRun("", "", c.auditAction, func() error { return c.run(ctx) })

		listener.OnDone(c.node, errText(err))
	}()

	return run
}

// newClusterJoinListener adapts a JoinListener to the run's NewClusterListener: the run
// emits progress and one Done; the talosconfig/kubeconfig are not handed back here (the
// app already stored the talosconfig, and the Kubernetes screens fetch the kubeconfig on
// demand). Progress and the error text are masked by maskedJoinListener.
type newClusterJoinListener struct{ JoinListener }

func (l newClusterJoinListener) OnProgress(json string) { l.JoinListener.OnProgress(json) }

func (l newClusterJoinListener) OnTalosconfig(string) {}

func (l newClusterJoinListener) OnKubeconfig(string) {}

func (l newClusterJoinListener) OnDone(nodeAddress, errMessage string) {
	l.JoinListener.OnDone(nodeAddress, errMessage)
}
