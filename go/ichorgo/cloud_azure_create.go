package ichorgo

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Creating an Azure VM for a new node: the provider's half of cloudCreateRun.

const (
	// azureUserDataLimit is Azure's: 64 KB of custom data once base64-encoded, so 48 KiB raw.
	azureUserDataLimit = 48 << 10

	azurePollInterval = 5 * time.Second
	// azureNotFoundGrace: ARM may answer 404 for a resource it just made, for a moment.
	azureNotFoundGrace = 30 * time.Second

	azureDefaultDiskGB = 20
	azureMaxDiskGB     = 4095
	// azureAdminUser: ARM wants a Linux VM's user and key; Talos ignores both.
	azureAdminUser = "talos"
)

// azureDeleteWait bounds the wait for an accepted delete; a force delete usually ends in one or
// two minutes. Past it the delete is reported as unconfirmed, never as done (a test shortens it).
var azureDeleteWait = 5 * time.Minute

var (
	azureGroupPattern = regexp.MustCompile(`^[-\w.()]{1,90}$`)
	azureIDPattern    = regexp.MustCompile(`^/subscriptions/[^/?#\s]+/resourceGroups/[^/?#\s]+/providers/[^?#\s]+$`)
)

// azureCreateSpec is StartAzureCreate's specJSON.
type azureCreateSpec struct {
	ResourceGroup     string `json:"resourceGroup"`
	Location          string `json:"location"`
	Size              string `json:"size"`
	ImageID           string `json:"imageId"`
	SubnetID          string `json:"subnetId"`
	SecurityGroupID   string `json:"securityGroupId"`
	AssociatePublicIP bool   `json:"associatePublicIp"`
	OSDiskGB          int    `json:"osDiskGB"`
	Hostname          string `json:"hostname"`
	Role              string `json:"role"`
	TemplateNode      string `json:"templateNode"`
	InstallDisk       string `json:"installDisk"`
	ExtraEdits        string `json:"extraEditsJSON"`
	LabelsJSON        string `json:"labelsJson"`
}

// azureAPIVM is the part of a VM the run reads.
type azureAPIVM struct {
	ID         string            `json:"id"`
	Tags       map[string]string `json:"tags"`
	Properties struct {
		ProvisioningState string `json:"provisioningState"`
		InstanceView      struct {
			Statuses []struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"statuses"`
		} `json:"instanceView"`
	} `json:"properties"`
}

func (v azureAPIVM) status(code string) bool {
	return slices.ContainsFunc(v.Properties.InstanceView.Statuses, func(s struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}) bool {
		return strings.EqualFold(s.Code, code)
	})
}

// failure is what the VM's failed statuses say.
func (v azureAPIVM) failure() string {
	var out []string

	for _, s := range v.Properties.InstanceView.Statuses {
		if strings.HasPrefix(strings.ToLower(s.Code), "provisioningstate/failed") && s.Message != "" {
			out = append(out, s.Message)
		}
	}

	return cmp.Or(strings.Join(out, "; "), "Azure gave no reason")
}

// azureMade is a resource the run created: deleted by name when the VM never came.
type azureMade struct {
	word, path, version string
}

// StartAzureCreate creates an Azure VM from a Talos image the subscription holds, with the new
// node's config as custom data, and follows it: creating → starting → (plain Talos) rebooting →
// booted → joining → registering → done, or (Omni) omni → done. specJSON: {"resourceGroup",
// "location","size","imageId","subnetId","securityGroupId","associatePublicIp","osDiskGB",
// "hostname","role","templateNode","installDisk","extraEditsJSON","labelsJson"}. credJSON: as AzureTest's.
// Refused before any write: a principal whose permissions on the resource group lack a write the
// run makes, an image whose generation or architecture the size cannot run, a subnet or security
// group of another location, a VM, NIC or public IP of the host's name already there, custom
// data over 48 KiB. The run makes <hostname>-ip (when asked), <hostname>-nic and the VM
// <hostname> (computerName too), tagged Name, ichor-run and ichor-cluster (a hash of the context
// name), each with deleteOption Delete, so one VM delete takes the disk, the NIC and the address.
// A VM create with no clear answer is found by name and its ichor-run tag; a NIC or address made
// before a VM that never came is deleted by name. OnDone gets the node's address to add to the
// context ("" on Omni). Refused on the demo and in privacy mode; recorded in the activity log as
// azure-vm-create, never with the config or the credential. Billable: the app confirms the cost
// and the hostname first.
func StartAzureCreate(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec azureCreateSpec

	join, template, specErr := newCloudJoin(configYAML, contextName, specJSON, &spec, "VM", listener)
	spec.TemplateNode = template

	h := &azureCreate{
		join: join,
		spec: spec, specErr: specErr, credJSON: credJSON, poll: azurePollInterval,
	}

	return h.start(cloudCreateTimeout)
}

// azureCreate is one run of StartAzureCreate.
type azureCreate struct {
	join     *nodeJoin
	spec     azureCreateSpec
	specErr  error
	credJSON string
	poll     time.Duration
	client   *azureClient
	omni     bool
	runID    string
	vmID     string // the VM this run created, "" before
	// vmUnclear: the VM create got no clear answer; the VM is looked up by name and tag.
	vmUnclear bool
	made      []azureMade // the address and NIC this run created, in order
	noRoute   bool        // no public address and no NAT gateway on the subnet
	// omniHostnames lists the machines' host names (the Omni API; replaced in tests).
	omniHostnames func(context.Context, *session) ([]string, error)
}

func (h *azureCreate) start(timeout time.Duration) *JoinRun {
	run := &cloudCreateRun{provider: h, join: h.join, specErr: h.specErr, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}

	return run.start(timeout)
}

func (h *azureCreate) label() string { return "Azure (resource group " + h.spec.ResourceGroup + ")" }

// machineWord and machineID name what cleanup deletes: the VM, else the last resource made
// before it. The id is the resource id without the subscription.
func (h *azureCreate) machineWord() string {
	if h.vmID == "" && !h.vmUnclear && len(h.made) > 0 {
		return h.made[len(h.made)-1].word
	}

	return "VM"
}

func (h *azureCreate) machineID() string {
	switch {
	case h.vmID != "":
		return h.client.scrub(h.vmID)
	case h.vmUnclear:
		return h.client.scrub(h.vmPath())
	case len(h.made) > 0:
		return h.client.scrub(h.made[len(h.made)-1].path)
	}

	return ""
}

func (h *azureCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s size=%s location=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.Size, h.spec.Location)
	if h.vmID != "" {
		params += " vm=" + h.machineID()
	}

	return auditAction{Action: "azure-vm-create", Node: h.join.node, Params: params}
}

func (h *azureCreate) groupPath() string {
	return "/subscriptions/" + h.client.subscription + "/resourceGroups/" + url.PathEscape(h.spec.ResourceGroup)
}

func (h *azureCreate) vmPath() string {
	return h.groupPath() + "/providers/Microsoft.Compute/virtualMachines/" + h.spec.Hostname
}

func (h *azureCreate) nicPath() string {
	return h.groupPath() + "/providers/Microsoft.Network/networkInterfaces/" + h.spec.Hostname + "-nic"
}

func (h *azureCreate) ipPath() string {
	return h.groupPath() + "/providers/Microsoft.Network/publicIPAddresses/" + h.spec.Hostname + "-ip"
}

// prepare checks everything that can be refused before a write and makes the user data.
func (h *azureCreate) prepare(ctx context.Context, omni bool) (string, error) {
	h.spec.Hostname = strings.TrimSpace(h.spec.Hostname)
	h.join.req.hostname = h.spec.Hostname

	name := h.spec.Hostname
	if !validHostname(name) || strings.Contains(name, ".") || len(name) > 64 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return "", fmt.Errorf("%q is not a host name (letters, digits and dashes, at most 64)", name)
	}

	if err := h.checkSpec(); err != nil {
		return "", err
	}

	client, err := newAzureClient(h.credJSON)
	if err != nil {
		return "", err
	}

	h.client = client

	if err := client.mint(ctx); err != nil {
		return "", err
	}

	if err := h.checkChoice(ctx); err != nil {
		return "", err
	}

	data, err := cloudNodeUserData(h.join, omni, h.join.req, h.spec.ExtraEdits)
	if err != nil {
		return "", err
	}

	if data.Size > azureUserDataLimit {
		return "", fmt.Errorf("the user data is %d bytes, over the 48 KiB Azure takes: remove extra edits, or the config's docs and examples on the template", data.Size)
	}

	if err := h.checkPermissions(ctx); err != nil {
		return "", err
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}

	h.runID = hex.EncodeToString(id)

	return data.UserData, nil
}

// azureRefused: Azure answered no, and made nothing (a 4xx other than a rate limit).
func (h *azureCreate) refused(err error) bool {
	var e *azureError

	return errors.As(err, &e) && e.status >= 400 && e.status < 500 && !h.transient(err)
}

// put creates one resource and waits for its operation. A resource without a clear answer is
// kept in the list too: deleting a name that does not exist answers 204.
func (h *azureCreate) put(ctx context.Context, word, path, version string, body any) (string, error) {
	var answer azureAPIRef

	_, headers, err := h.client.do(ctx, http.MethodPut, path, url.Values{"api-version": {version}}, body, &answer)
	if err != nil {
		if !h.refused(err) {
			h.made = append(h.made, azureMade{word: word, path: path, version: version})
		}

		return "", err
	}

	h.made = append(h.made, azureMade{word: word, path: path, version: version})

	if err := h.client.wait(ctx, headers, h.poll); err != nil {
		return "", err
	}

	return cmp.Or(answer.ID, path), nil
}

// azureThrowawayKey is an SSH public key for ARM, which wants one on a Linux VM. Its private
// half is dropped here: Talos runs no SSH server, and nothing can use it.
func azureThrowawayKey() (string, error) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}

	key, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), nil
}

type azureJSON = map[string]any

// create makes the public address (when asked), the NIC, then the VM, each once.
func (h *azureCreate) create(ctx context.Context, userData string) error {
	line := "creating " + h.spec.Hostname + " (" + h.spec.Size + " in " + h.spec.Location + ")"
	if h.noRoute {
		line += ": no public address and no NAT gateway on the subnet, so the node may not reach the Image Factory or Omni"
	}

	h.join.emit(cloudPhaseCreating, line)

	tags := map[string]string{"Name": h.spec.Hostname, azureTagRun: h.runID, azureTagCluster: hetznerClusterLabel(h.join.context)}
	ipConfig := azureJSON{"subnet": azureJSON{"id": h.spec.SubnetID}, "privateIPAllocationMethod": "Dynamic"}

	if h.spec.AssociatePublicIP {
		ipID, err := h.put(ctx, "public IP address", h.ipPath(), azureNetworkVersion, azureJSON{
			"location": h.spec.Location, "tags": tags, "sku": azureJSON{"name": "Standard"},
			"properties": azureJSON{"publicIPAllocationMethod": "Static"},
		})
		if err != nil {
			return err
		}

		ipConfig["publicIPAddress"] = azureJSON{"id": ipID, "properties": azureJSON{"deleteOption": "Delete"}}
	}

	nic := azureJSON{"ipConfigurations": []azureJSON{{"name": "ipconfig1", "properties": ipConfig}}}
	if h.spec.SecurityGroupID != "" {
		nic["networkSecurityGroup"] = azureJSON{"id": h.spec.SecurityGroupID}
	}

	nicID, err := h.put(ctx, "network interface", h.nicPath(), azureNetworkVersion, azureJSON{"location": h.spec.Location, "tags": tags, "properties": nic})
	if err != nil {
		return err
	}

	key, err := azureThrowawayKey()
	if err != nil {
		return err
	}

	var answer azureAPIRef

	_, _, err = h.client.do(ctx, http.MethodPut, h.vmPath(), url.Values{"api-version": {azureComputeVersion}}, h.vmBody(userData, key, nicID, tags), &answer)
	if err != nil {
		h.vmUnclear = !h.refused(err)

		return err
	}

	h.vmID = cmp.Or(answer.ID, h.vmPath())
	h.join.applied = true

	return nil
}

// vmBody is the VM's create request: the node's config as custom data, the computer name the
// host name Talos reads from IMDS, and every attached resource deleted with the VM.
func (h *azureCreate) vmBody(userData, key, nicID string, tags map[string]string) azureJSON {
	return azureJSON{
		"location": h.spec.Location, "tags": tags,
		"properties": azureJSON{
			"hardwareProfile": azureJSON{"vmSize": h.spec.Size},
			"storageProfile": azureJSON{
				"imageReference": azureJSON{"id": h.spec.ImageID},
				"osDisk": azureJSON{
					"createOption": "FromImage", "deleteOption": "Delete", "diskSizeGB": cmp.Or(h.spec.OSDiskGB, azureDefaultDiskGB),
					"managedDisk": azureJSON{"storageAccountType": "StandardSSD_LRS"},
				},
			},
			"osProfile": azureJSON{
				"computerName": h.spec.Hostname, "adminUsername": azureAdminUser,
				"customData": base64.StdEncoding.EncodeToString([]byte(userData)),
				"linuxConfiguration": azureJSON{
					"disablePasswordAuthentication": true,
					"ssh":                           azureJSON{"publicKeys": []azureJSON{{"path": "/home/" + azureAdminUser + "/.ssh/authorized_keys", "keyData": key}}},
				},
			},
			"networkProfile": azureJSON{"networkInterfaces": []azureJSON{{"id": nicID, "properties": azureJSON{"primary": true, "deleteOption": "Delete"}}}},
		},
	}
}

// waitRunning follows the VM until it runs with an address: its public one when asked, else its
// private one. A provider hiccup is polled through, as is a VM ARM does not list yet (for
// azureNotFoundGrace): only a failed VM or the run's end stops it.
func (h *azureCreate) waitRunning(ctx context.Context) error {
	h.join.emit(cloudPhaseStarting, "waiting for VM "+h.spec.Hostname+" to start")

	last, missingSince := "", time.Time{}

	for {
		var vm azureAPIVM

		_, _, err := h.client.do(ctx, http.MethodGet, h.vmPath(), url.Values{"api-version": {azureComputeVersion}, "$expand": {"instanceView"}}, nil, &vm)
		switch {
		case err == nil:
			missingSince = time.Time{}
		case azureNotFound(err) && missingSince.IsZero():
			missingSince = time.Now()
		}

		state := vm.Properties.ProvisioningState

		switch {
		case azureNotFound(err) && time.Since(missingSince) < azureNotFoundGrace:
		case err != nil && !h.transient(err):
			return err
		case err == nil && strings.EqualFold(state, "Failed"):
			return fmt.Errorf("VM %s failed: %s", h.spec.Hostname, h.client.scrub(vm.failure()))
		case err == nil && strings.EqualFold(state, azureOpSucceeded) && vm.status("PowerState/running"):
			done, err := h.reachedRunning(ctx)
			if err != nil && !h.transient(err) && !azureNotFound(err) {
				return err
			}

			if done {
				return nil
			}
		case err == nil && state != last:
			last = state
			h.join.emit(cloudPhaseStarting, "VM "+h.spec.Hostname+": "+strings.ToLower(last))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

// reachedRunning reads the VM's addresses: false while a public one is asked and not given yet.
func (h *azureCreate) reachedRunning(ctx context.Context) (bool, error) {
	var nic struct {
		Properties struct {
			IPConfigurations []struct {
				Properties struct {
					PrivateIPAddress string `json:"privateIPAddress"`
				} `json:"properties"`
			} `json:"ipConfigurations"`
		} `json:"properties"`
	}

	if err := h.client.get(ctx, h.nicPath(), azureNetworkVersion, &nic); err != nil {
		return false, err
	}

	private := ""
	if c := nic.Properties.IPConfigurations; len(c) > 0 {
		private = c[0].Properties.PrivateIPAddress
	}

	if h.spec.AssociatePublicIP {
		var ip struct {
			Properties struct {
				IPAddress string `json:"ipAddress"`
			} `json:"properties"`
		}

		if err := h.client.get(ctx, h.ipPath(), azureNetworkVersion, &ip); err != nil || ip.Properties.IPAddress == "" {
			return false, err
		}

		h.join.node = normalizeEndpoint(ip.Properties.IPAddress)
		h.join.emit(cloudPhaseStarting, "VM "+h.spec.Hostname+" is running at "+h.join.node)

		return true, nil
	}

	if private == "" {
		return false, nil
	}

	h.join.node = normalizeEndpoint(private)
	h.join.emit(cloudPhaseStarting, "VM "+h.spec.Hostname+" is running with no public address: following it at its private address "+
		h.join.node+", which needs a route from this device (a VPN into the virtual network)")

	return true, nil
}

// transient: worth trying again (rate limit, a server error, no answer).
func (h *azureCreate) transient(err error) bool { return azureTransient(err) }

// azureTransient: worth trying again (rate limit, a server error, no answer).
func azureTransient(err error) bool {
	var e *azureError
	if errors.As(err, &e) {
		switch e.code {
		case "TooManyRequests", "RetryableError", "InternalServerError", "ServerTimeout":
			return true
		}

		return e.status == http.StatusTooManyRequests || e.status >= http.StatusInternalServerError
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// findUnanswered looks for the VM of a create with no clear answer: by its name, kept only when
// its ichor-run tag is this run's. A refusal (4xx) created nothing, and nothing is looked up.
func (h *azureCreate) findUnanswered(ctx context.Context, why error) {
	if h.runID == "" || h.client == nil || (h.refused(why) && !h.vmUnclear) {
		return
	}

	h.lookupVM(ctx)
}

func (h *azureCreate) lookupVM(ctx context.Context) {
	var vm azureAPIVM
	if err := h.client.get(ctx, h.vmPath(), azureComputeVersion, &vm); err == nil && vm.Tags[azureTagRun] == h.runID {
		h.vmID = cmp.Or(vm.ID, h.vmPath())
	}
}

// deleteMachine force-deletes the VM, which takes its disk, NIC and address (deleteOption);
// without a VM, it deletes by name what the run made before it.
func (h *azureCreate) deleteMachine(ctx context.Context) error {
	if h.vmID == "" && h.vmUnclear {
		h.lookupVM(ctx)
	}

	if h.vmID != "" {
		return h.deleteOne(ctx, h.vmPath(), url.Values{"api-version": {azureComputeVersion}, "forceDeletion": {"true"}})
	}

	for i := len(h.made) - 1; i >= 0; i-- {
		if err := h.deleteOne(ctx, h.made[i].path, url.Values{"api-version": {h.made[i].version}}); err != nil {
			return err
		}
	}

	return nil
}

// deleteOne deletes one resource and follows the delete for azureDeleteWait: a resource already
// gone is deleted; a delete still running then is an error naming the resource, so the run
// says what may still be billed instead of claiming it is gone.
func (h *azureCreate) deleteOne(ctx context.Context, path string, query url.Values) error {
	_, err := azureDeleteResource(ctx, h.client, h.poll, path, query)

	return err
}

// azureDeleteResource is deleteOne for any run; existed is false when the resource was already
// gone (Azure answered 204 or 404).
func azureDeleteResource(ctx context.Context, c *azureClient, poll time.Duration, path string, query url.Values) (bool, error) {
	status, headers, err := c.do(ctx, http.MethodDelete, path, query, nil, nil)

	switch {
	case azureNotFound(err):
		return false, nil
	case err != nil:
		return true, err
	case status == http.StatusNoContent:
		return false, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, azureDeleteWait)
	defer cancel()

	err = c.wait(waitCtx, headers, poll)
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		name := nameOnly(path)

		return true, &azureUnfinishedDelete{name: name[strings.LastIndexByte(name, '/')+1:]}
	}

	return true, err
}

// azureUnfinishedDelete is a delete Azure accepted but had not finished within azureDeleteWait.
// It unwraps to a deadline so the cleanup does not retry it as a lost answer.
type azureUnfinishedDelete struct{ name string }

func (e *azureUnfinishedDelete) Error() string {
	return fmt.Sprintf("azure accepted the delete of %s but had not finished it after %s: check that it is gone", e.name, azureDeleteWait)
}

func (e *azureUnfinishedDelete) Unwrap() error     { return context.DeadlineExceeded }
func (e *azureUnfinishedDelete) unfinishedDelete() {}

// nameOnly drops the query of a resource path, if any.
func nameOnly(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}

	return p
}
