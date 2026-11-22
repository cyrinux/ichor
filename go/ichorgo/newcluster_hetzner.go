package ichorgo

// A new Talos cluster at Hetzner, from the phone: one control-plane server booted from the
// project's Talos snapshot. The API endpoint must be in the config before the server exists,
// so the run reserves a Primary IP first (cheaper than a load balancer for one control plane),
// makes the cluster's secrets for that address, hands the talosconfig to the app, and only
// then creates the server with the control-plane config as its user data (Talos's hcloud
// platform reads it at boot: the maintenance API is never exposed). The sibling run
// (newcluster_run.go) then follows the node, bootstraps etcd once and waits for Kubernetes.
//
// Money: the Primary IP is billed from its creation, the server by the started hour. Before
// the bootstrap succeeded, a failed or cancelled run deletes what it made, by id; after it, the
// cluster exists and everything is kept.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// newClusterPhaseIP: the Primary IP the cluster's endpoint will be is reserved.
	newClusterPhaseIP = "ip"

	// hetznerCodeIPAssigned: the IP still belongs to a server that is being deleted.
	hetznerCodeIPAssigned = "primary_ip_assigned"

	// hetznerNewClusterDisk is the disk Talos installs to on a Hetzner server.
	hetznerNewClusterDisk = "/dev/sda"

	// hetznerCleanupFor bounds the cleanup of a failed run, on a context of its own.
	hetznerCleanupFor = 2 * time.Minute

	// hetznerSpecAddress stands in for the IP while the spec is checked, before any write.
	hetznerSpecAddress = "192.0.2.1"
)

// hetznerNewClusterSpec is StartHetznerNewCluster's specJSON.
type hetznerNewClusterSpec struct {
	Name              string  `json:"name"`
	ServerType        string  `json:"serverType"`
	Location          string  `json:"location"`
	SnapshotID        int64   `json:"snapshotId"`
	InstallDisk       string  `json:"installDisk"`
	KubernetesVersion string  `json:"kubernetesVersion"`
	TalosVersion      string  `json:"talosVersion"`
	SSHKeyIDs         []int64 `json:"sshKeyIds"`
	// AllowScheduling is a pointer: absent means on (a single node runs the workloads).
	AllowScheduling *bool `json:"allowSchedulingOnControlPlanes"`
}

// hetznerNewClusterCost is HetznerNewClusterEstimate's answer: the server and the Primary IP,
// net of VAT, as Hetzner's pricing lists them.
type hetznerNewClusterCost struct {
	Currency         string `json:"currency"`
	ServerHourly     string `json:"serverHourly"`
	ServerMonthly    string `json:"serverMonthly"`
	PrimaryIPMonthly string `json:"primaryIPMonthly"`
	TotalMonthly     string `json:"totalMonthly"`
}

// HetznerNewClusterEstimate is the cost line of StartHetznerNewCluster: the server type at the
// location (by the hour, capped by the month) and the IPv4 Primary IP there, from GET
// /pricing and the type. Creates nothing. The demo answers samples. JSON: {"currency",
// "serverHourly","serverMonthly","primaryIPMonthly","totalMonthly"}.
func HetznerNewClusterEstimate(configYAML, contextName, tokenJSON, serverType, location string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(hetznerNewClusterCost{Currency: "EUR", ServerHourly: "0.0060", ServerMonthly: "3.49", PrimaryIPMonthly: "0.50", TotalMonthly: "3.99"})
	}

	serverType, location = strings.TrimSpace(serverType), strings.TrimSpace(location)
	if serverType == "" || location == "" {
		return "", errors.New("pick a server type and a location")
	}

	client, err := newHetznerClient(tokenJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()

	server, err := hetznerServerPrice(ctx, client, serverType, location)
	if err != nil {
		return "", err
	}

	currency, ip, err := hetznerPrimaryIPPrice(ctx, client, location)
	if err != nil {
		return "", err
	}

	serverMonthly, _ := strconv.ParseFloat(server.Monthly.Net, 64)
	ipMonthly, _ := strconv.ParseFloat(ip, 64)

	return toJSON(hetznerNewClusterCost{
		Currency:     currency,
		ServerHourly: hetznerAmount(server.Hourly.Net, 4), ServerMonthly: hetznerAmount(server.Monthly.Net, 2),
		PrimaryIPMonthly: hetznerAmount(ip, 2),
		TotalMonthly:     strconv.FormatFloat(serverMonthly+ipMonthly, 'f', 2, 64),
	})
}

// hetznerServerPrice is the price of serverType in location.
func hetznerServerPrice(ctx context.Context, client hetznerClient, serverType, location string) (hetznerAPIPrice, error) {
	types, err := hetznerList[hetznerAPIType](ctx, client, hetznerTypePath(serverType), "server_types")
	if err != nil {
		return hetznerAPIPrice{}, err
	}

	for _, t := range types {
		if t.Name != serverType {
			continue
		}

		for _, p := range t.Prices {
			if p.Location == location {
				return p, nil
			}
		}

		return hetznerAPIPrice{}, fmt.Errorf("server type %s is not sold in %s", serverType, location)
	}

	return hetznerAPIPrice{}, fmt.Errorf("no server type %q at Hetzner", serverType)
}

// hetznerPrimaryIPPrice is the currency and the monthly net price of an IPv4 Primary IP in
// location.
func hetznerPrimaryIPPrice(ctx context.Context, client hetznerClient, location string) (currency, monthly string, err error) {
	var pricing struct {
		Pricing struct {
			Currency   string `json:"currency"`
			PrimaryIPs []struct {
				Type   string            `json:"type"`
				Prices []hetznerAPIPrice `json:"prices"`
			} `json:"primary_ips"`
		} `json:"pricing"`
	}

	if err := client.do(ctx, http.MethodGet, "/pricing", nil, &pricing); err != nil {
		return "", "", err
	}

	for _, ip := range pricing.Pricing.PrimaryIPs {
		if ip.Type != "ipv4" {
			continue
		}

		for _, p := range ip.Prices {
			if p.Location == location {
				return pricing.Pricing.Currency, p.Monthly.Net, nil
			}
		}
	}

	return "", "", fmt.Errorf("no IPv4 Primary IP price at Hetzner in %s", location)
}

// newClusterProgress is a progress event that also carries the new talosconfig, once, in the
// secrets phase: the app stores it before any server is created.
type newClusterProgress struct {
	joinProgress

	Talosconfig string `json:"talosconfig,omitempty"`
}

// StartHetznerNewCluster makes a one-node Talos cluster at Hetzner. tokenJSON: {"token"}, a
// Read & Write token. specJSON: {"name","serverType","location","snapshotId","installDisk"
// (default /dev/sda),"kubernetesVersion","talosVersion","sshKeyIds",
// "allowSchedulingOnControlPlanes"}. Phases: ip → secrets (the event carries "talosconfig":
// store it as a new context) → creating → starting → booted → bootstrapping → ready; OnDone
// gets the cluster's address. Everything that can be refused is refused before any write (a
// read-only token, a snapshot of another architecture than the server type, a type not sold in
// the location, user data over 32 KiB). A failed or cancelled run deletes the server and the IP
// it made, unless etcd was bootstrapped; the message names what could not be deleted and is
// still billed. configYAML and contextName are the context the person started from, only to
// refuse the demo (both may be empty). Refused in privacy mode; recorded in the activity log
// as hetzner-cluster-create (name, type, location, ids; never the token or the config).
func StartHetznerNewCluster(configYAML, contextName, tokenJSON, specJSON string, listener JoinListener) *JoinRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedJoinListener{listener}

	r := &hetznerNewCluster{
		demo:      isTalosDemoContext(configYAML, contextName),
		tokenJSON: tokenJSON, listener: listener,
		poll: cloudPollInterval, interval: upgradePollInterval,
	}

	if err := json.Unmarshal([]byte(specJSON), &r.spec); err != nil {
		r.specErr = fmt.Errorf("invalid new cluster: %w", err)
	}

	return r.start(cloudCreateTimeout)
}

// hetznerNewCluster is one run of StartHetznerNewCluster.
type hetznerNewCluster struct {
	demo      bool
	tokenJSON string
	spec      hetznerNewClusterSpec
	specErr   error
	listener  JoinListener
	// poll paces the Hetzner calls, interval the node's.
	poll, interval time.Duration
	// cleanupFor bounds the cleanup (hetznerCleanupFor; shorter in tests).
	cleanupFor time.Duration

	client   hetznerClient
	runID    string
	ipID     int64 // the Primary IP this run reserved, 0 before
	ip       string
	serverID int64 // the server this run created, 0 before
	draft    *newClusterDraftEntry
	// bootstrapped: etcd runs, the cluster exists and is kept whatever happens next.
	bootstrapped bool
	// created is called once the server exists (tests serve the node then).
	created func(*hetznerNewCluster)
	// deadline makes the run's context; nil is context.WithTimeout. Tests set one that
	// expires on a fake's event instead of on the wall clock.
	deadline func(timeout time.Duration) (context.Context, context.CancelFunc)
}

func (r *hetznerNewCluster) start(timeout time.Duration) *JoinRun {
	newContext := r.deadline
	if newContext == nil {
		newContext = func(timeout time.Duration) (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), timeout)
		}
	}

	ctx, cancel := newContext(timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { r.listener.OnDone("", msg) })

		err := recordedRun("", "", r.auditAction, func() error { return r.run(ctx, timeout) })
		if err != nil {
			r.listener.OnDone("", errText(err))

			return
		}

		r.listener.OnDone(r.ip, "")
	}()

	return &JoinRun{cancel: cancel}
}

func (r *hetznerNewCluster) auditAction() auditAction {
	params := fmt.Sprintf("name=%s type=%s location=%s", strings.TrimSpace(r.spec.Name), r.spec.ServerType, r.spec.Location)
	if r.serverID != 0 {
		params += " server=" + strconv.FormatInt(r.serverID, 10)
	}

	if r.ipID != 0 {
		params += " ip=" + strconv.FormatInt(r.ipID, 10)
	}

	return auditAction{Action: "hetzner-cluster-create", Node: r.ip, Params: params}
}

func (r *hetznerNewCluster) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli(), Address: r.ip}, r.listener.OnProgress)
}

func (r *hetznerNewCluster) run(ctx context.Context, timeout time.Duration) error {
	if r.demo {
		return errDemoUnavailable
	}

	if r.specErr != nil {
		return r.specErr
	}

	if privacy.isEnabled() {
		return errJoinPrivacy
	}

	if err := r.prepare(ctx); err != nil {
		return err
	}

	err := r.build(ctx)

	switch {
	case err == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("%s was not ready within %s", r.spec.Name, timeout)
	case ctx.Err() != nil:
		err = errors.New("stopped")
	}

	return r.cleanup(err)
}

// request is the sibling's validated spec for the cluster at address.
func (r *hetznerNewCluster) request(address string) (newClusterRequest, error) {
	disk := strings.TrimSpace(r.spec.InstallDisk)
	if disk == "" {
		disk = hetznerNewClusterDisk
	}

	return parseNewClusterSpec(newClusterSpec{
		Name: r.spec.Name, Endpoint: address, KubernetesVersion: r.spec.KubernetesVersion,
		TalosVersion: r.spec.TalosVersion, InstallDisk: disk, AllowScheduling: r.spec.AllowScheduling,
	})
}

// prepare refuses everything it can before a write.
func (r *hetznerNewCluster) prepare(ctx context.Context) error {
	if _, err := r.request(hetznerSpecAddress); err != nil {
		return err
	}

	if r.spec.SnapshotID <= 0 || r.spec.ServerType == "" || r.spec.Location == "" {
		return errors.New("pick a server type, a location and a snapshot")
	}

	client, err := newHetznerClient(r.tokenJSON)
	if err != nil {
		return err
	}

	r.client = client

	readOnly, err := client.readOnly(ctx)
	if err != nil {
		return err
	}

	if readOnly {
		return errHetznerReadOnly
	}

	choice := &hetznerCreate{client: client, spec: hetznerCreateSpec{ServerType: r.spec.ServerType, Location: r.spec.Location, SnapshotID: r.spec.SnapshotID}}

	return choice.checkServerChoice(ctx)
}

// build makes the cluster: the IP, the secrets, the server, then the sibling's bootstrap.
func (r *hetznerNewCluster) build(ctx context.Context) error {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	r.runID = hex.EncodeToString(id)

	if err := r.reserveIP(ctx); err != nil {
		return err
	}

	if err := r.makeSecrets(); err != nil {
		return err
	}

	if err := r.createServer(ctx); err != nil {
		return err
	}

	if r.created != nil {
		r.created(r)
	}

	node := &nodeJoin{listener: r.listener}
	wait := &hetznerCreate{client: r.client, join: node, serverID: r.serverID, poll: r.poll}

	if err := wait.waitRunning(ctx); err != nil {
		return err
	}

	return r.follow(ctx)
}

func (r *hetznerNewCluster) labels() map[string]string {
	return map[string]string{"ichor-run": r.runID, "ichor-cluster": hetznerClusterLabel(r.spec.Name)}
}

// reserveIP creates the Primary IP the cluster's endpoint will be, in the server's location.
func (r *hetznerNewCluster) reserveIP(ctx context.Context) error {
	r.emit(newClusterPhaseIP, "reserving a Primary IP in "+r.spec.Location)

	body := map[string]any{
		"name": r.spec.Name + "-api", "type": "ipv4", "location": r.spec.Location,
		"assignee_type": "server", "auto_delete": false, "labels": r.labels(),
	}

	var resp struct {
		PrimaryIP hetznerPrimaryIP `json:"primary_ip"`
	}

	if err := r.client.do(ctx, http.MethodPost, "/primary_ips", body, &resp); err != nil {
		r.findUnanswered(ctx, err, "/primary_ips", "primary_ips", func(ip hetznerPrimaryIP) { r.ipID, r.ip = ip.ID, ip.IP })

		return err
	}

	r.ipID, r.ip = resp.PrimaryIP.ID, resp.PrimaryIP.IP
	if r.ip == "" {
		return errors.New("the Primary IP Hetzner reserved has no address")
	}

	return nil
}

type hetznerPrimaryIP struct {
	ID int64  `json:"id"`
	IP string `json:"ip"`
}

// makeSecrets makes the cluster's secrets and configs for the IP and hands the talosconfig to
// the app, before anything else is billed.
func (r *hetznerNewCluster) makeSecrets() error {
	req, err := r.request(r.ip)
	if err != nil {
		return err
	}

	draft, err := prepareNewCluster(req)
	if err != nil {
		return err
	}

	if err := checkUserDataSize(cloudUserData{UserData: draft.full, Size: len(draft.full)}); err != nil {
		return err
	}

	r.draft = draft

	emitJSON(newClusterProgress{
		joinProgress: joinProgress{
			Phase: newClusterPhaseSecrets, At: time.Now().UnixMilli(), Address: r.ip,
			Message: "the talosconfig of " + req.name + " is ready to store on this phone",
		},
		Talosconfig: draft.talos,
	}, r.listener.OnProgress)

	return nil
}

// createServer sends POST /servers once, with the control-plane config and the IP.
func (r *hetznerNewCluster) createServer(ctx context.Context) error {
	body := map[string]any{
		"name": r.spec.Name, "server_type": r.spec.ServerType, "location": r.spec.Location,
		"image": strconv.FormatInt(r.spec.SnapshotID, 10), "user_data": r.draft.full, "labels": r.labels(),
		"public_net": map[string]any{"enable_ipv4": true, "enable_ipv6": true, "ipv4": r.ipID},
	}

	if len(r.spec.SSHKeyIDs) > 0 {
		body["ssh_keys"] = r.spec.SSHKeyIDs
	}

	r.emit(hetznerPhaseCreating, "creating "+r.spec.Name+" ("+r.spec.ServerType+" in "+r.spec.Location+")")

	var resp struct {
		Server hetznerServer `json:"server"`
	}

	if err := r.client.do(ctx, http.MethodPost, "/servers", body, &resp); err != nil {
		r.findUnanswered(ctx, err, "/servers", "servers", func(s hetznerPrimaryIP) { r.serverID = s.ID })

		var e *hetznerError
		if errors.As(err, &e) && e.status == http.StatusForbidden && e.code == hetznerCodeForbidden {
			return errHetznerReadOnly
		}

		return err
	}

	r.serverID = resp.Server.ID

	return nil
}

// findUnanswered looks for what a POST with no clear answer may have made, by the run's
// label. A refusal (4xx) made nothing.
func (r *hetznerNewCluster) findUnanswered(ctx context.Context, why error, path, key string, found func(hetznerPrimaryIP)) {
	var e *hetznerError
	if errors.As(why, &e) && e.status < http.StatusInternalServerError {
		return
	}

	items, err := hetznerList[hetznerPrimaryIP](ctx, r.client, path+"?label_selector=ichor-run%3D"+r.runID, key)
	if err == nil && len(items) == 1 {
		found(items[0])
	}
}

// follow is the sibling's second half on the new server: booted → bootstrapping → ready.
func (r *hetznerNewCluster) follow(ctx context.Context) error {
	cl, err := newClusterClient(ctx, r.draft.talos)
	if err != nil {
		return err
	}

	defer cl.Close() //nolint:errcheck

	c := &newClusterRun{draft: r.draft, node: r.ip, stored: r.draft.talos, listener: r.listener, interval: r.interval}
	err = c.follow(ctx, cl)
	r.bootstrapped = c.bootstrapped

	return err
}

// cleanup deletes what the run made, by id, unless the cluster was bootstrapped; why is what
// went wrong, and the result says what is still billed.
func (r *hetznerNewCluster) cleanup(why error) error {
	if r.bootstrapped {
		return fmt.Errorf("%w. %s is kept: etcd was bootstrapped on server %d at %s, and its talosconfig is stored; delete the server and Primary IP %d at Hetzner if you do not want it",
			why, r.spec.Name, r.serverID, r.ip, r.ipID)
	}

	if r.serverID == 0 && r.ipID == 0 {
		return why
	}

	limit := r.cleanupFor
	if limit == 0 {
		limit = hetznerCleanupFor
	}

	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()

	r.emit(cloudPhaseCleanup, "deleting what this run made at Hetzner")

	var left []string

	if r.serverID != 0 {
		if err := r.deleteWhile(ctx, "/servers/"+strconv.FormatInt(r.serverID, 10), hetznerTransient); err != nil {
			left = append(left, fmt.Sprintf("server %d (%s)", r.serverID, friendlyError(err)))
		}
	}

	if r.ipID != 0 {
		// The IP is released once its server is gone: Hetzner refuses it as assigned meanwhile.
		assigned := func(err error) bool {
			var e *hetznerError

			return hetznerTransient(err) || (errors.As(err, &e) && e.code == hetznerCodeIPAssigned)
		}

		if err := r.deleteWhile(ctx, "/primary_ips/"+strconv.FormatInt(r.ipID, 10), assigned); err != nil {
			left = append(left, fmt.Sprintf("Primary IP %d (%s)", r.ipID, friendlyError(err)))
		}
	}

	if len(left) > 0 {
		return fmt.Errorf("%w. Not deleted, still billed: %s; delete them at Hetzner", why, strings.Join(left, ", "))
	}

	return fmt.Errorf("%w. The server and the Primary IP this run made are deleted; remove the cluster entry", why)
}

// deleteWhile deletes path, again while retry says so, until ctx ends.
func (r *hetznerNewCluster) deleteWhile(ctx context.Context, path string, retry func(error) bool) error {
	for {
		err := r.client.do(ctx, http.MethodDelete, path, nil, nil)
		if err == nil || !retry(err) || ctx.Err() != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return err
		case <-time.After(r.poll):
		}
	}
}
