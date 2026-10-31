package ichorgo

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// S8 step 6: the Talos snapshot of a Hetzner project, made from the phone. Hetzner has no image
// upload, so the run takes the route Talos documents: a throwaway server in the rescue system
// writes the Image Factory's image to its own disk over SSH (the rescue host downloads it, the
// phone moves no image bytes), then becomes a snapshot and is deleted. Billable: one small server
// for a few minutes, then the snapshot's storage. Every path ends in cleanup; see
// cloud_hetzner_ssh.go for the commands.

const (
	hetznerSnapshotTimeout        = 40 * time.Minute
	hetznerOffWait                = 3 * time.Minute
	hetznerSnapshotCleanupTimeout = 2 * time.Minute
	hetznerSnapshotCleanupTries   = 10

	hetznerStatusOff   = "off"
	hetznerStockImage  = "ubuntu-24.04" // any: it is overwritten
	hetznerRescueType  = "linux64"
	hetznerActionOK    = "success"
	hetznerAuditAction = "hetzner-snapshot"
	hetznerImageType   = "snapshot"

	// A label value holds at most 63 characters and a schematic id has 64.
	hetznerSchematicLabel = 16
	hetznerSchematicShown = 12

	// Phases of the run; creating and cleanup are the create run's.
	hetznerPhaseKey      = "key"
	hetznerPhaseRescue   = "rescue"
	hetznerPhaseWriting  = "writing"
	hetznerPhaseSnapshot = "snapshot"
)

// hetznerSnapshotSpec is StartHetznerSnapshot's specJSON. The image URL is never in it: it comes
// from the template's schematic and version.
type hetznerSnapshotSpec struct {
	TemplateNode string `json:"templateNode"`
	Arch         string `json:"arch"`
	Location     string `json:"location"`
	ServerType   string `json:"serverType"`
	Description  string `json:"description"`
}

type hetznerSnapshotResult struct {
	ImageID     int64             `json:"imageId"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	SizeGB      float64           `json:"sizeGB"`
	Created     string            `json:"created"`
}

type hetznerSnapshotCost struct {
	ServerType   string `json:"serverType"`
	Hourly       string `json:"hourly"`
	MonthlyPerGB string `json:"monthlyPerGB"`
	Currency     string `json:"currency"`
}

// StartHetznerSnapshot makes a Talos snapshot in the token's project: key → creating → rescue →
// writing → snapshot → cleanup → done. specJSON: {"templateNode","arch","location","serverType",
// "description"}; templateNode "" takes the first member that answers, arch "" the template's,
// serverType "" the cheapest of that architecture at the location. tokenJSON: {"token"}, a Read &
// Write token. OnDone's first argument is the result JSON, not an address: {"imageId",
// "description","labels","sizeGB","created"}. Cancel, or any failure, deletes the server, the SSH
// key and an unfinished snapshot this run created, and the message names them. Nothing of the
// cluster is written; the schematic comes from the template's machine config, and a template whose
// config cannot be read is refused. Refused on the demo; recorded in the activity log as
// hetzner-snapshot. Billable: the app shows HetznerSnapshotEstimate first.
func StartHetznerSnapshot(configYAML, contextName, tokenJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec hetznerSnapshotSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)
	if specErr != nil {
		specErr = errors.New("invalid snapshot request")
	}

	contextName, spec.TemplateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(spec.TemplateNode))

	listener = maskedJoinListener{listener}

	r := &hetznerSnapshotRun{
		config: configYAML, context: contextName, tokenJSON: tokenJSON, spec: spec, specErr: specErr,
		listener: listener,
		poll:     hetznerPollInterval, offWait: hetznerOffWait, ssh: newHetznerSSH(),
	}

	return r.start(hetznerSnapshotTimeout)
}

// hetznerSnapshotRun is one run of StartHetznerSnapshot.
type hetznerSnapshotRun struct {
	config, context string
	tokenJSON       string
	spec            hetznerSnapshotSpec
	specErr         error
	listener        JoinListener
	poll, offWait   time.Duration
	ssh             hetznerSSH

	client     hetznerClient
	target     bootTarget
	serverType string
	runID      string
	name       string
	result     hetznerSnapshotResult

	// What this run created (0 before), and the POSTs that may have created it without an answer.
	keyID, serverID, imageID int64
	keyPosted, serverPosted  bool
}

func (r *hetznerSnapshotRun) start(timeout time.Duration) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { r.listener.OnDone("", msg) })
		defer holdCalls(r.config, r.context)()

		err := recordedRun(r.config, r.context, r.auditAction, func() error { return r.run(ctx, timeout) })
		if err != nil {
			r.listener.OnDone("", errText(err))

			return
		}

		out, err := toJSON(r.result)
		if err != nil {
			r.listener.OnDone("", errText(err))

			return
		}

		r.emit(joinPhaseDone, "snapshot "+strconv.FormatInt(r.result.ImageID, 10)+" is ready")
		r.listener.OnDone(privacy.mask(out), "")
	}()

	return &JoinRun{cancel: cancel}
}

func (r *hetznerSnapshotRun) auditAction() auditAction {
	params := fmt.Sprintf("arch=%s location=%s version=%s", cmp.Or(r.target.arch, r.spec.Arch), r.spec.Location, r.target.version)
	if r.imageID != 0 {
		params += " image=" + strconv.FormatInt(r.imageID, 10)
	}

	return auditAction{Action: hetznerAuditAction, Node: r.spec.TemplateNode, Params: params}
}

func (r *hetznerSnapshotRun) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, r.listener.OnProgress)
}

func (r *hetznerSnapshotRun) run(ctx context.Context, timeout time.Duration) error {
	if r.specErr != nil {
		return r.specErr
	}

	if isTalosDemoContext(r.config, r.context) {
		return errDemoUnavailable
	}

	if err := r.prepare(ctx); err != nil {
		return err
	}

	err := r.make(ctx)

	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("the snapshot was not ready within %s", timeout)
	case ctx.Err() != nil:
		err = errors.New("stopped")
	}

	return r.cleanup(err)
}

// prepare checks everything that can be refused before a write.
func (r *hetznerSnapshotRun) prepare(ctx context.Context) error {
	arch, err := normalizeBootArch(r.spec.Arch)
	if err != nil {
		return err
	}

	if r.spec.Location = strings.TrimSpace(r.spec.Location); r.spec.Location == "" {
		return errors.New("pick a location")
	}

	if r.client, err = newHetznerClient(r.tokenJSON); err != nil {
		return err
	}

	readOnly, err := r.client.readOnly(ctx)
	if err != nil {
		return err
	}

	if readOnly {
		return errHetznerReadOnly
	}

	media, err := withSession(r.config, r.context, configPreviewTimeout, func(ctx context.Context, s *session) (bootMedia, error) {
		src, err := readBootSource(ctx, s, r.spec.TemplateNode)
		if err != nil {
			return bootMedia{}, err
		}

		// A snapshot lasts: one silently without the cluster's extensions is worse than none.
		if src.imageErr != "" {
			return bootMedia{}, fmt.Errorf("the install image of %s cannot be read (%s), so neither can its schematic", src.node, src.imageErr)
		}

		return buildBootMedia(src, "", false)
	})
	if err != nil {
		return err
	}

	for _, warning := range media.Warnings {
		r.emit(hetznerPhaseKey, warning)
	}

	r.target = media.target()
	r.target.secureBoot = false
	r.target.arch = cmp.Or(arch, r.target.arch)
	r.spec.TemplateNode = media.TemplateNode

	types, err := hetznerList[hetznerAPIType](ctx, r.client, "/server_types", "server_types")
	if err != nil {
		return err
	}

	option, _, ok := hetznerSnapshotType(types, r.spec.ServerType, r.target.arch, r.spec.Location)
	if !ok {
		return fmt.Errorf("no server type for %s at %s: pick another location", r.target.arch, r.spec.Location)
	}

	r.serverType = option.Name

	return nil
}

// hetznerSnapshotType is name when it fits, else the cheapest type of arch at location.
func hetznerSnapshotType(types []hetznerAPIType, name, arch, location string) (hetznerTypeOption, hetznerPrice, bool) {
	var (
		best      hetznerTypeOption
		bestPrice hetznerPrice
		bestRate  float64
		found     bool
	)

	for _, t := range types {
		option, ok := t.option()
		if !ok || option.Arch != arch || (name != "" && option.Name != name) {
			continue
		}

		i := slices.IndexFunc(option.Prices, func(p hetznerPrice) bool { return p.Location == location })
		if i < 0 {
			continue
		}

		rate, err := strconv.ParseFloat(option.Prices[i].Hourly, 64)
		if err != nil {
			continue
		}

		if !found || rate < bestRate {
			best, bestPrice, bestRate, found = option, option.Prices[i], rate, true
		}
	}

	return best, bestPrice, found
}

// HetznerSnapshotEstimate is the cost line of a snapshot run: the server type it would use (about
// one started hour of it) and the storage price of the snapshot. Creates nothing. JSON:
// {"serverType","hourly","monthlyPerGB","currency"}.
func HetznerSnapshotEstimate(tokenJSON, arch, location string) (out string, err error) {
	defer maskResult(&out, &err)

	arch, err = normalizeBootArch(arch)
	if err != nil {
		return "", err
	}

	client, err := newHetznerClient(tokenJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()

	var pricing struct {
		Pricing struct {
			Currency string `json:"currency"`
			Image    struct {
				PerGBMonth struct {
					Net string `json:"net"`
				} `json:"price_per_gb_month"`
			} `json:"image"`
		} `json:"pricing"`
	}

	if err := client.do(ctx, http.MethodGet, "/pricing", nil, &pricing); err != nil {
		return "", err
	}

	types, err := hetznerList[hetznerAPIType](ctx, client, "/server_types", "server_types")
	if err != nil {
		return "", err
	}

	option, price, ok := hetznerSnapshotType(types, "", arch, location)
	if !ok {
		return "", fmt.Errorf("no server type for %s at %s: pick another location", arch, location)
	}

	return toJSON(hetznerSnapshotCost{
		ServerType: option.Name, Hourly: price.Hourly, Currency: pricing.Pricing.Currency,
		MonthlyPerGB: hetznerAmount(pricing.Pricing.Image.PerGBMonth.Net, 4),
	})
}

// make runs the steps up to the finished snapshot; cleanup follows, whatever it returns.
func (r *hetznerSnapshotRun) make(ctx context.Context) error {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	r.runID = hex.EncodeToString(id)
	r.name = "ichor-snap-" + r.runID

	key, err := newHetznerSSHKey()
	if err != nil {
		return err
	}

	r.emit(hetznerPhaseKey, "adding a temporary SSH key to the project")

	if err := r.createKey(ctx, key.publicKey); err != nil {
		return err
	}

	r.emit(hetznerPhaseCreating, "creating the throwaway server "+r.name+" ("+r.serverType+" in "+r.spec.Location+")")

	if err := r.createServer(ctx); err != nil {
		return err
	}

	r.emit(hetznerPhaseRescue, "starting server "+strconv.FormatInt(r.serverID, 10)+" in the rescue system")

	address, err := r.startRescue(ctx)
	if err != nil {
		return err
	}

	conn, err := r.ssh.connect(ctx, address, key)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	url := cloudImageURL(r.target, cloudPlatformHetzner)
	r.emit(hetznerPhaseWriting, "writing "+url)

	if err := conn.writeImage(ctx, url, func(line string) { r.emit(hetznerPhaseWriting, line) }); err != nil {
		return err
	}

	if err := r.waitOff(ctx); err != nil {
		return err
	}

	r.emit(hetznerPhaseSnapshot, "taking the snapshot")

	return r.createImage(ctx)
}

func (r *hetznerSnapshotRun) labels() map[string]string {
	return map[string]string{"ichor-run": r.runID}
}

func (r *hetznerSnapshotRun) createKey(ctx context.Context, publicKey string) error {
	var resp struct {
		SSHKey struct {
			ID int64 `json:"id"`
		} `json:"ssh_key"`
	}

	r.keyPosted = true

	body := map[string]any{"name": r.name, "public_key": publicKey, "labels": r.labels()}
	if err := r.client.do(ctx, http.MethodPost, "/ssh_keys", body, &resp); err != nil {
		return err
	}

	r.keyID = resp.SSHKey.ID

	return nil
}

func (r *hetznerSnapshotRun) createServer(ctx context.Context) error {
	var resp struct {
		Server hetznerServer `json:"server"`
		Action hetznerAction `json:"action"`
	}

	r.serverPosted = true

	body := map[string]any{
		"name": r.name, "server_type": r.serverType, "location": r.spec.Location, "image": hetznerStockImage,
		"ssh_keys": []int64{r.keyID}, "start_after_create": false, "labels": r.labels(),
	}
	if err := r.client.do(ctx, http.MethodPost, "/servers", body, &resp); err != nil {
		return err
	}

	r.serverID = resp.Server.ID

	return r.waitAction(ctx, resp.Action)
}

func (r *hetznerSnapshotRun) serverAction(ctx context.Context, action string, body any, out any) error {
	return r.client.do(ctx, http.MethodPost, "/servers/"+strconv.FormatInt(r.serverID, 10)+"/actions/"+action, body, out)
}

// startRescue boots the server into the rescue system and returns its address.
func (r *hetznerSnapshotRun) startRescue(ctx context.Context) (string, error) {
	var resp struct {
		Action hetznerAction `json:"action"`
	}

	body := map[string]any{"type": hetznerRescueType, "ssh_keys": []int64{r.keyID}}
	if err := r.serverAction(ctx, "enable_rescue", body, &resp); err != nil {
		return "", err
	}

	if err := r.waitAction(ctx, resp.Action); err != nil {
		return "", err
	}

	if err := r.serverAction(ctx, "poweron", nil, &resp); err != nil {
		return "", err
	}

	if err := r.waitAction(ctx, resp.Action); err != nil {
		return "", err
	}

	s, err := r.server(ctx)
	if err != nil {
		return "", err
	}

	if s.address() == "" {
		return "", fmt.Errorf("server %d has no public address", r.serverID)
	}

	return s.address(), nil
}

func (r *hetznerSnapshotRun) server(ctx context.Context) (hetznerServer, error) {
	var resp struct {
		Server hetznerServer `json:"server"`
	}

	err := r.client.do(ctx, http.MethodGet, "/servers/"+strconv.FormatInt(r.serverID, 10), nil, &resp)

	return resp.Server, err
}

// waitAction follows an action to its end; a provider hiccup is polled through.
func (r *hetznerSnapshotRun) waitAction(ctx context.Context, action hetznerAction) error {
	for action.ID != 0 {
		var a struct {
			Action hetznerAction `json:"action"`
		}

		err := r.client.do(ctx, http.MethodGet, "/actions/"+strconv.FormatInt(action.ID, 10), nil, &a)

		switch {
		case err != nil && !hetznerTransient(err):
			return err
		case err == nil && a.Action.Status == hetznerStatusError:
			if a.Action.Error != nil {
				return errors.New("Hetzner: " + a.Action.Error.Message)
			}

			return errors.New("hetzner reported a failed action")
		case err == nil && a.Action.Status == hetznerActionOK:
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.poll):
		}
	}

	return nil
}

// waitOff waits for the shutdown the write ended with, then powers the server off.
func (r *hetznerSnapshotRun) waitOff(ctx context.Context) error {
	off, err := r.pollOff(ctx)
	if off || err != nil {
		return err
	}

	r.emit(hetznerPhaseWriting, "the server did not shut down: powering it off")

	var resp struct {
		Action hetznerAction `json:"action"`
	}

	if err := r.serverAction(ctx, "poweroff", nil, &resp); err != nil {
		return err
	}

	if err := r.waitAction(ctx, resp.Action); err != nil {
		return err
	}

	if off, err = r.pollOff(ctx); err == nil && !off {
		err = fmt.Errorf("server %d did not power off", r.serverID)
	}

	return err
}

func (r *hetznerSnapshotRun) pollOff(ctx context.Context) (bool, error) {
	deadline := time.Now().Add(r.offWait)

	for {
		s, err := r.server(ctx)

		switch {
		case err != nil && !hetznerTransient(err):
			return false, err
		case err == nil && s.Status == hetznerStatusOff:
			return true, nil
		case time.Now().After(deadline):
			return false, nil
		}

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(r.poll):
		}
	}
}

// createImage takes the snapshot, labelled so that the picker ranks it first.
func (r *hetznerSnapshotRun) createImage(ctx context.Context) error {
	schematic := r.target.schematic
	description := cmp.Or(strings.TrimSpace(r.spec.Description), fmt.Sprintf("Talos %s %s %s %s",
		r.target.version, cloudPlatformHetzner, r.target.arch, schematic[:min(hetznerSchematicShown, len(schematic))]))
	labels := map[string]string{
		"os": "talos", "talos-version": r.target.version, "arch": r.target.arch,
		"schematic": schematic[:min(hetznerSchematicLabel, len(schematic))],
	}

	var resp struct {
		Image struct {
			ID int64 `json:"id"`
		} `json:"image"`
		Action hetznerAction `json:"action"`
	}

	body := map[string]any{"type": hetznerImageType, "description": description, "labels": labels}
	if err := r.serverAction(ctx, "create_image", body, &resp); err != nil {
		return err
	}

	r.imageID = resp.Image.ID
	r.result = hetznerSnapshotResult{ImageID: r.imageID, Description: description, Labels: labels}

	if err := r.waitAction(ctx, resp.Action); err != nil {
		return err
	}

	// The size and the date are extras: the snapshot is made without them.
	var image struct {
		Image struct {
			Size    float64 `json:"image_size"`
			Created string  `json:"created"`
		} `json:"image"`
	}

	if r.client.do(ctx, http.MethodGet, hetznerImagePath(r.imageID), nil, &image) == nil {
		r.result.SizeGB, r.result.Created = image.Image.Size, image.Image.Created
	}

	return nil
}

// cleanup deletes what this run created and adds the outcome to why: an unfinished snapshot, the
// server and the key. Its context is its own: the run's has often ended. A snapshot that is ready
// stays, also when the rest could not be deleted.
func (r *hetznerSnapshotRun) cleanup(why error) error {
	ctx, cancel := context.WithTimeout(context.Background(), hetznerSnapshotCleanupTimeout)
	defer cancel()

	r.findUnanswered(ctx)

	var deleted, left []string

	remove := func(what, path string) {
		if err := r.deleteRetrying(ctx, path); err != nil {
			left = append(left, what+" ("+friendlyError(err)+")")
		} else {
			deleted = append(deleted, what)
		}
	}

	if r.imageID != 0 || r.serverID != 0 || r.keyID != 0 {
		r.emit(hetznerPhaseCleanup, "deleting the throwaway server and its key")
	}

	if why != nil && r.imageID != 0 {
		remove(fmt.Sprintf("unfinished snapshot %d", r.imageID), hetznerImagePath(r.imageID))
	}

	if r.serverID != 0 {
		remove(fmt.Sprintf("server %d (%s)", r.serverID, r.name), "/servers/"+strconv.FormatInt(r.serverID, 10))
	}

	if r.keyID != 0 {
		remove(fmt.Sprintf("SSH key %d (%s)", r.keyID, r.name), "/ssh_keys/"+strconv.FormatInt(r.keyID, 10))
	}

	return hetznerCleanupOutcome(why, r.imageID, deleted, left)
}

func hetznerCleanupOutcome(why error, imageID int64, deleted, left []string) error {
	stillThere := ""
	if len(left) > 0 {
		stillThere = "Could not be deleted, and still billed: " + strings.Join(left, ", ") + ". Delete it in the Hetzner console"
	}

	switch {
	case why == nil && stillThere == "":
		return nil
	case why == nil:
		return fmt.Errorf("snapshot %d is ready. %s", imageID, stillThere)
	case len(deleted) > 0 && stillThere != "":
		return fmt.Errorf("%w. Deleted: %s. %s", why, strings.Join(deleted, ", "), stillThere)
	case len(deleted) > 0:
		return fmt.Errorf("%w. Deleted: %s", why, strings.Join(deleted, ", "))
	case stillThere != "":
		return fmt.Errorf("%w. %s", why, stillThere)
	}

	return why
}

// findUnanswered looks up what a POST with no clear answer may have created, by the run's label.
func (r *hetznerSnapshotRun) findUnanswered(ctx context.Context) {
	selector := "?label_selector=ichor-run%3D" + r.runID

	type object struct {
		ID int64 `json:"id"`
	}

	if r.serverPosted && r.serverID == 0 {
		if found, err := hetznerList[object](ctx, r.client, "/servers"+selector, "servers"); err == nil && len(found) == 1 {
			r.serverID = found[0].ID
		}
	}

	if r.keyPosted && r.keyID == 0 {
		if found, err := hetznerList[object](ctx, r.client, "/ssh_keys"+selector, "ssh_keys"); err == nil && len(found) == 1 {
			r.keyID = found[0].ID
		}
	}
}

// deleteRetrying sends one DELETE until it holds: a server stays locked while an action on it
// ends. What is already gone counts as deleted.
func (r *hetznerSnapshotRun) deleteRetrying(ctx context.Context, path string) error {
	var err error

	for try := range hetznerSnapshotCleanupTries {
		err = r.client.do(ctx, http.MethodDelete, path, nil, nil)

		var e *hetznerError
		if errors.As(err, &e) && e.status == http.StatusNotFound {
			return nil
		}

		locked := errors.As(err, &e) && (e.status == http.StatusLocked || e.status == http.StatusConflict)
		if err == nil || !(locked || hetznerTransient(err)) {
			return err
		}

		if try < hetznerSnapshotCleanupTries-1 {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(r.poll):
			}
		}
	}

	return err
}
