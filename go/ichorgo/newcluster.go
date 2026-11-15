package ichorgo

// Step 5 of the Talos lifecycle, the preparing half: a new single-node cluster from a machine
// booted from the Talos ISO, like `talosctl gen secrets` + `gen config` on a laptop.
// NewClusterPrepare makes the cluster's secrets and configs and keeps them in Go memory
// behind a draft id (gomobile passes strings only, so the bundle never crosses into the
// apps); the apps show the redacted preview and store the talosconfig, then StartNewCluster
// (newcluster_run.go) sends the config and bootstraps. Nothing here reaches the network.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/compatibility"
	machineryconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// newClusterDraftTTL: a draft lives this long; an app restart loses it sooner, which is
// fine: nothing was sent, the person starts again.
const newClusterDraftTTL = 30 * time.Minute

// newClusterSpec is what the apps send NewClusterPrepare.
type newClusterSpec struct {
	Name              string `json:"name"`
	Endpoint          string `json:"endpoint"`
	KubernetesVersion string `json:"kubernetesVersion"`
	TalosVersion      string `json:"talosVersion"`
	InstallDisk       string `json:"installDisk"`
	InstallImage      string `json:"installImage"`
	// AllowScheduling is a pointer: absent means on (a single node runs the workloads).
	AllowScheduling *bool    `json:"allowSchedulingOnControlPlanes"`
	AdditionalSANs  []string `json:"additionalSans"`
	ExtraEditsJSON  string   `json:"extraEditsJSON"`
}

// newClusterDraft is what NewClusterPrepare returns: the talosconfig (a credential the app
// stores) and the previews, never the secrets bundle.
type newClusterDraft struct {
	DraftID              string   `json:"draftId"`
	Talosconfig          string   `json:"talosconfig"`
	RedactedControlplane string   `json:"redactedControlplane"`
	RedactedWorker       string   `json:"redactedWorker"`
	Warnings             []string `json:"warnings"`
	Size                 int      `json:"size"` // bytes of the control-plane config sent
	TalosVersion         string   `json:"talosVersion"`
	KubernetesVersion    string   `json:"kubernetesVersion"`
}

// newClusterRequest is a validated spec.
type newClusterRequest struct {
	name, host, endpoint       string // host: the machine; endpoint: https://host:6443
	kubernetesVersion          string // no leading v
	talosVersion, installImage string // talosVersion: v1.x.y, empty for the machinery's own
	installDisk                string
	allowScheduling            bool
	sans                       []string
	edits                      []configEdit
}

// newClusterDraftEntry is a prepared draft: what StartNewCluster sends and checks.
type newClusterDraftEntry struct {
	req     newClusterRequest
	bundle  *secrets.Bundle
	full    string // the control-plane config sent to the machine
	talos   string // the os:admin talosconfig the app stores before the run
	expires time.Time
}

var (
	newClusterDraftsMu sync.Mutex
	newClusterDrafts   = map[string]*newClusterDraftEntry{}
)

// NewClusterPrepare makes a new cluster's secrets and configs from specJSON {"name",
// "endpoint" (the machine's address), "kubernetesVersion" (empty: the default),
// "talosVersion" (the machine's, from MaintenanceNodeInspect), "installDisk",
// "installImage" (empty: the installer of talosVersion), "allowSchedulingOnControlPlanes"
// (default true), "additionalSans", "extraEditsJSON"} and keeps them in memory for 30
// minutes. It returns {"draftId","talosconfig","redactedControlplane","redactedWorker",
// "warnings","size","talosVersion","kubernetesVersion"}: the app stores the talosconfig as
// a new context, then calls StartNewCluster. Refused in privacy mode; recorded in the
// activity log as new-cluster-generated (name and versions, never a secret).
func NewClusterPrepare(specJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var spec newClusterSpec

	defer func() {
		recordOutcome("", "", auditAction{Action: "new-cluster-generated", Params: newClusterAuditParams(spec)}, err)
	}()

	if privacy.isEnabled() {
		return "", errJoinPrivacy
	}

	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return "", fmt.Errorf("invalid new cluster: %w", err)
	}

	req, err := parseNewClusterSpec(spec)
	if err != nil {
		return "", err
	}

	entry, err := prepareNewCluster(req)
	if err != nil {
		return "", err
	}

	redactedCP, redactedWorker, warnings, err := newClusterPreview(entry)
	if err != nil {
		return "", err
	}

	id, err := storeNewClusterDraft(entry)
	if err != nil {
		return "", err
	}

	talosVersion := req.talosVersion
	if talosVersion == "" {
		talosVersion = machineryconfig.TalosVersionCurrent.String()
	}

	return toJSON(newClusterDraft{
		DraftID: id, Talosconfig: entry.talos,
		RedactedControlplane: redactedCP, RedactedWorker: redactedWorker,
		Warnings: warnings, Size: len(entry.full),
		TalosVersion: talosVersion, KubernetesVersion: req.kubernetesVersion,
	})
}

func newClusterAuditParams(spec newClusterSpec) string {
	return "name=" + strings.TrimSpace(spec.Name) + " talos=" + strings.TrimSpace(spec.TalosVersion) +
		" k8s=" + strings.TrimSpace(spec.KubernetesVersion)
}

// parseNewClusterSpec validates what the person typed.
func parseNewClusterSpec(spec newClusterSpec) (newClusterRequest, error) {
	req := newClusterRequest{
		name:              strings.TrimSpace(spec.Name),
		host:              hostOnly(strings.TrimSpace(spec.Endpoint)),
		kubernetesVersion: strings.TrimPrefix(strings.TrimSpace(spec.KubernetesVersion), "v"),
		talosVersion:      strings.TrimSpace(spec.TalosVersion),
		installDisk:       strings.TrimSpace(spec.InstallDisk),
		installImage:      strings.TrimSpace(spec.InstallImage),
		allowScheduling:   spec.AllowScheduling == nil || *spec.AllowScheduling,
	}

	if req.kubernetesVersion == "" {
		req.kubernetesVersion = constants.DefaultKubernetesVersion
	}

	if req.talosVersion != "" && !strings.HasPrefix(req.talosVersion, "v") {
		req.talosVersion = "v" + req.talosVersion
	}

	switch {
	case req.name == "" || !validHostname(req.name) || strings.Contains(req.name, "."):
		return req, fmt.Errorf("%q is not a cluster name (letters, digits and dashes)", spec.Name)
	case req.host == "" || (net.ParseIP(req.host) == nil && !validHostname(req.host)):
		return req, errMaintenanceAddress
	case !strings.HasPrefix(req.installDisk, "/dev/"):
		return req, fmt.Errorf("%q is not a disk (/dev/sda, /dev/nvme0n1…)", spec.InstallDisk)
	case !kubeVersionOK(req.kubernetesVersion):
		return req, fmt.Errorf("%q is not a Kubernetes version (1.34.1)", spec.KubernetesVersion)
	case strings.ContainsAny(req.installImage, " \t\n"):
		return req, fmt.Errorf("%q is not an image reference", spec.InstallImage)
	}

	k8s, err := newClusterKubeVersion(req.kubernetesVersion, req.talosVersion, strings.TrimSpace(spec.KubernetesVersion) == "")
	if err != nil {
		return req, err
	}

	req.kubernetesVersion = k8s
	req.endpoint = "https://" + net.JoinHostPort(req.host, strconv.Itoa(constants.DefaultControlPlanePort))

	for _, san := range spec.AdditionalSANs {
		if san = strings.TrimSpace(san); san != "" && !slices.Contains(req.sans, san) {
			req.sans = append(req.sans, san)
		}
	}

	if strings.TrimSpace(spec.ExtraEditsJSON) != "" {
		if err := json.Unmarshal([]byte(spec.ExtraEditsJSON), &req.edits); err != nil {
			return req, fmt.Errorf("invalid edits: %w", err)
		}
	}

	return req, nil
}

// hostOnly is an address without its port and brackets.
func hostOnly(address string) string {
	if h, _, err := net.SplitHostPort(address); err == nil {
		return h
	}

	return strings.Trim(address, "[]")
}

// kubeVersionOK: 1.x or 1.x.y, digits only.
func kubeVersionOK(v string) bool {
	parts := strings.Split(v, ".")
	if (len(parts) != 2 && len(parts) != 3) || parts[0] != "1" {
		return false
	}

	for _, p := range parts {
		if p == "" || len(p) > 3 || strings.ContainsFunc(p, func(r rune) bool { return r < '0' || r > '9' }) {
			return false
		}
	}

	return true
}

// newClusterKubeVersion checks that the machine's Talos runs Kubernetes k8s. A defaulted
// version Talos cannot run falls back to the newest minor it can (x.y.0); a chosen one is
// refused. An unknown Talos version (or one machinery does not know) checks nothing.
func newClusterKubeVersion(k8s, talosVersion string, defaulted bool) (string, error) {
	if talosVersion == "" {
		return k8s, nil
	}

	talos, err := compatibility.ParseTalosVersion(&machineapi.VersionInfo{Tag: talosVersion})
	if err != nil {
		return "", fmt.Errorf("%q is not a Talos version (v1.11.2)", talosVersion)
	}

	supported := func(v string) error {
		kv, err := compatibility.ParseKubernetesVersion(v)
		if err != nil {
			return err
		}

		return kv.SupportedWith(talos)
	}

	err = supported(k8s)
	if err == nil || strings.Contains(err.Error(), "is not supported") {
		return k8s, nil
	}

	if !defaulted || strings.Contains(err.Error(), "too old") {
		return "", errors.New(friendlyError(err))
	}

	minor, _ := strconv.Atoi(strings.Split(k8s, ".")[1])
	for ; minor > 0; minor-- {
		if v := "1." + strconv.Itoa(minor) + ".0"; supported(v) == nil {
			return v, nil
		}
	}

	return "", errors.New(friendlyError(err))
}

// newClusterContract is the config version contract of the machine's Talos: a config of a
// newer contract is refused at apply.
func newClusterContract(talosVersion string) (*machineryconfig.VersionContract, error) {
	if talosVersion == "" {
		return machineryconfig.TalosVersionCurrent, nil
	}

	contract, err := machineryconfig.ParseContractFromVersion(talosVersion)
	if err != nil {
		return nil, fmt.Errorf("%q is not a Talos version (v1.11.2)", talosVersion)
	}

	return contract, nil
}

// prepareNewCluster makes the secrets, the control-plane config and the talosconfig.
func prepareNewCluster(req newClusterRequest) (*newClusterDraftEntry, error) {
	contract, err := newClusterContract(req.talosVersion)
	if err != nil {
		return nil, err
	}

	bundle, err := secrets.NewBundle(secrets.NewFixedClock(time.Now()), contract)
	if err != nil {
		return nil, fmt.Errorf("generate the cluster's secrets: %s", friendlyError(err))
	}

	in, err := newClusterInput(req, bundle, contract)
	if err != nil {
		return nil, err
	}

	full, err := newClusterConfig(in, machine.TypeControlPlane, req.edits)
	if err != nil {
		return nil, err
	}

	tc, err := in.Talosconfig()
	if err != nil {
		return nil, fmt.Errorf("make the talosconfig: %s", friendlyError(err))
	}

	talos, err := tc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("make the talosconfig: %s", friendlyError(err))
	}

	return &newClusterDraftEntry{req: req, bundle: bundle, full: full, talos: string(talos)}, nil
}

func newClusterInput(req newClusterRequest, bundle *secrets.Bundle, contract *machineryconfig.VersionContract) (*generate.Input, error) {
	opts := []generate.Option{
		generate.WithSecretsBundle(bundle),
		generate.WithVersionContract(contract),
		generate.WithInstallDisk(req.installDisk),
		// The talosconfig's endpoint is the machine, its only control plane.
		generate.WithEndpointList([]string{req.host}),
		generate.WithAllowSchedulingOnControlPlanes(req.allowScheduling),
	}

	if image := newClusterInstallImage(req); image != "" {
		opts = append(opts, generate.WithInstallImage(image))
	}

	if len(req.sans) > 0 {
		opts = append(opts, generate.WithAdditionalSubjectAltNames(req.sans))
	}

	in, err := generate.NewInput(req.name, req.endpoint, req.kubernetesVersion, opts...)
	if err != nil {
		return nil, fmt.Errorf("make the cluster's config: %s", friendlyError(err))
	}

	return in, nil
}

// newClusterInstallImage is the installer the machine installs: the one chosen, else the
// installer of the machine's own Talos version (not the machinery's, which may be newer).
func newClusterInstallImage(req newClusterRequest) string {
	if req.installImage != "" || req.talosVersion == "" {
		return req.installImage
	}

	return "ghcr.io/siderolabs/installer:" + req.talosVersion
}

// newClusterConfig is the machine config of typ with the edits applied, validated as a
// client validates a join config.
func newClusterConfig(in *generate.Input, typ machine.Type, edits []configEdit) (string, error) {
	cfg, err := in.Config(typ)
	if err != nil {
		return "", fmt.Errorf("make the %s config: %s", typ, friendlyError(err))
	}

	text, err := cfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", err
	}

	if len(edits) > 0 {
		docs, err := parseConfigDocs(text)
		if err != nil {
			return "", err
		}

		if docs, err = applyConfigEdits(docs, edits); err != nil {
			return "", err
		}

		if text, err = encodeConfigDocs(docs); err != nil {
			return "", err
		}
	}

	provider, err := configloader.NewFromBytes([]byte(text))
	if err != nil {
		return "", fmt.Errorf("the new config is not valid: %w", err)
	}

	if _, err := provider.ValidateAsClient(joinRuntime{}); err != nil {
		return "", fmt.Errorf("the new config is not valid: %w", err)
	}

	return provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
}

// newClusterPreview is what the apps show before the run: both configs redacted, and one
// warning per thing the person confirms.
func newClusterPreview(e *newClusterDraftEntry) (redactedCP, redactedWorker string, warnings []string, err error) {
	if redactedCP, err = redactConfig(e.full); err != nil {
		return "", "", nil, err
	}

	contract, err := newClusterContract(e.req.talosVersion)
	if err != nil {
		return "", "", nil, err
	}

	in, err := newClusterInput(e.req, e.bundle, contract)
	if err != nil {
		return "", "", nil, err
	}

	worker, err := newClusterConfig(in, machine.TypeWorker, nil)
	if err != nil {
		return "", "", nil, err
	}

	if redactedWorker, err = redactConfig(worker); err != nil {
		return "", "", nil, err
	}

	warnings = []string{
		"This phone generates the cluster's CAs, keys and tokens; they then live only in the control plane's config and in the talosconfig stored here.",
		"The config is sent over TLS but without authentication to " + e.req.host + ": a machine in maintenance mode cannot prove who it is. Check the IP its console shows.",
		"The machine installs Talos to " + e.req.installDisk + " and erases it; it becomes the only control plane of " + e.req.name + ".",
	}

	if e.req.talosVersion == "" {
		warnings = append(warnings, "The machine's Talos version is unknown: the config is made for "+machineryconfig.TalosVersionCurrent.String()+", which an older Talos refuses.")
	}

	return redactedCP, redactedWorker, warnings, nil
}

// storeNewClusterDraft keeps e for newClusterDraftTTL under a random id, dropping expired
// drafts on the way.
func storeNewClusterDraft(e *newClusterDraftEntry) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New(friendlyError(err))
	}

	id := hex.EncodeToString(b)
	now := time.Now()
	e.expires = now.Add(newClusterDraftTTL)

	newClusterDraftsMu.Lock()
	defer newClusterDraftsMu.Unlock()

	for k, d := range newClusterDrafts {
		if now.After(d.expires) {
			delete(newClusterDrafts, k)
		}
	}

	newClusterDrafts[id] = e

	return id, nil
}

var errNewClusterDraftGone = errors.New("this new cluster's draft is gone (it lasts 30 minutes, and the app restarted or it was used): remove the cluster entry and prepare it again")

// takeNewClusterDraft hands a draft over once: it is cleared on use.
func takeNewClusterDraft(id string) (*newClusterDraftEntry, error) {
	newClusterDraftsMu.Lock()
	defer newClusterDraftsMu.Unlock()

	e, ok := newClusterDrafts[id]
	delete(newClusterDrafts, id)

	if !ok || time.Now().After(e.expires) {
		return nil, errNewClusterDraftGone
	}

	return e, nil
}

// returnNewClusterDraft puts back a draft whose run ended before anything was sent, so the
// person can try again with the talosconfig already stored.
func returnNewClusterDraft(id string, e *newClusterDraftEntry) {
	newClusterDraftsMu.Lock()
	defer newClusterDraftsMu.Unlock()

	if time.Now().Before(e.expires) {
		newClusterDrafts[id] = e
	}
}
