package ichorgo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// S8 step 22: Compute Engine virtual machines, on plain REST (JSON over HTTPS) with a service
// account key: the JWT bearer grant of the GKE sign-in (gkeMethod.mint), whose token carries the
// cloud-platform scope. No Google SDK in the app. The app seals the key and passes it per call.
// A GKE key may mint a token Compute Engine still refuses: testIamPermissions is the gate. See
// https://cloud.google.com/compute/docs/reference/rest/v1.

const (
	gcpOptionsTimeout = 60 * time.Second
	gcpPriceTimeout   = 10 * time.Second
	// gcpMaxBody bounds an answer: a page of the billing catalog runs to a few MB.
	gcpMaxBody = 16 << 20
	// gcpMaxPages bounds the nextPageToken pages of one listing.
	gcpMaxPages = 20

	// gcpComputeService is Compute Engine's id in the Cloud Billing Catalog.
	gcpComputeService = "6F81-5844-456A"

	// The labels of what a run makes, and of an image the app made (the image child). Label
	// values are lowercase letters, digits, - and _: a version is written v1-12-0.
	gcpLabelRun          = "ichor-run"
	gcpLabelCluster      = "ichor-cluster"
	gcpLabelTalosVersion = "ichor-talos-version"
	gcpLabelSchematic    = "ichor-schematic"
	gcpLabelArch         = "ichor-arch"
	// gcpNetworkTag is what the guided network's firewall rules target.
	gcpNetworkTag = "ichor-node"

	gcpReasonDisabled  = "SERVICE_DISABLED"
	gcpReasonForbidden = "forbidden"
	gcpReasonNotFound  = "notFound"
	// gcpReasonUserProjectDenied: the signed-in account may not bill calls to the quota project.
	gcpReasonUserProjectDenied = "USER_PROJECT_DENIED"

	gcpOpDone = "DONE"
)

// Google endpoints, overridable in tests (as gcpCRMEndpoint and gcpTokenTarget).
var (
	gcpComputeEndpoint = "https://compute.googleapis.com"
	gcpBillingEndpoint = "https://cloudbilling.googleapis.com"

	errGCPNoCredential = errors.New("add a service account key first: the JSON file gcloud iam service-accounts keys create writes")

	gcpZonePattern        = regexp.MustCompile(`^[a-z]+-[a-z]+[0-9]+-[a-z]$`)
	gcpMachineTypePattern = regexp.MustCompile(`^[a-z][a-z0-9]*-[a-z0-9-]{1,60}$`)
	gcpPermissionPattern  = regexp.MustCompile(`'((compute|resourcemanager)\.[A-Za-z]+\.[A-Za-z]+)'`)
	gcpTokenPattern       = regexp.MustCompile(`ya29\.[-\w.]+`)
	gcpEmailPattern       = regexp.MustCompile(`[-\w.+]+@[-\w]+\.iam\.gserviceaccount\.com`)
	gcpNumberPattern      = regexp.MustCompile(`(?i)(projects?[/= ]+|project number:? *)[0-9]{6,}`)
	gcpVersionLabel       = regexp.MustCompile(`^v([0-9]+)-([0-9]+)-([0-9]+)(.*)$`)
)

// gcpError is a refusal: Google's {"error":{"code","message","errors":[{"reason"}],"status",
// "details"}}, or an operation's error. Its message never carries the key or its ids (scrub).
type gcpError struct {
	action  string // the permission (compute.instances.create) or what was asked
	reason  string // forbidden, SERVICE_DISABLED, quotaExceeded, notFound, alreadyExists…
	message string
	status  int
	// activationURL is where to enable the API (service) in project; zone is the create's.
	activationURL, service, project, zone string
}

func (e *gcpError) Error() string {
	switch e.reason {
	case gcpReasonDisabled, "accessNotConfigured":
		line := "the " + gcpServiceName(e.service) + " API is not enabled in project " + e.project
		if e.activationURL != "" {
			line += ": enable it at " + e.activationURL
		}

		return line
	case gcpReasonForbidden, "PERMISSION_DENIED", "insufficientPermissions":
		return "Google Cloud refused " + e.action + ": grant the printed role to the service account (or the signed-in account)"
	case gcpReasonUserProjectDenied:
		return "you may not bill calls to project " + e.project + ": ask for serviceusage.services.use on it (the Service Usage Consumer role), or pick another project"
	case "quotaExceeded", "QUOTA_EXCEEDED", "ZONE_RESOURCE_POOL_EXHAUSTED", "ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS":
		return "quota exceeded in " + cmp.Or(e.zone, "this zone") + ": pick another zone or machine type (" + e.message + ")"
	case gcpReasonNotFound:
		return "Google Cloud " + e.action + ": not found (" + e.message + ")"
	case "alreadyExists", "RESOURCE_ALREADY_EXISTS":
		return "Google Cloud " + e.action + ": it already exists (" + e.message + ")"
	}

	return "Google Cloud " + e.action + ": " + cmp.Or(e.message, e.reason, http.StatusText(e.status))
}

// gcpServiceName is the name the console gives an API.
func gcpServiceName(service string) string {
	names := map[string]string{
		"compute.googleapis.com": "Compute Engine", "cloudresourcemanager.googleapis.com": "Cloud Resource Manager",
		"cloudbilling.googleapis.com": "Cloud Billing",
	}

	return cmp.Or(names[service], service, "Google Cloud")
}

// gcpScrubbed is a transport error whose message went through scrub; errors.Is sees the cause.
type gcpScrubbed struct {
	message string
	err     error
}

func (e *gcpScrubbed) Error() string { return e.message }
func (e *gcpScrubbed) Unwrap() error { return e.err }

// gcpCredential is credJSON: {"serviceAccountJson":"<the key file's text>","project","zone"}, or a
// Google-account sign-in: {"googleSignIn":<its state>,"account","project" (the quota project),
// "zone"} (cloud_gcp_signin.go).
type gcpCredential struct {
	ServiceAccountJSON string         `json:"serviceAccountJson,omitempty"`
	Project            string         `json:"project"`
	Zone               string         `json:"zone"`
	GoogleSignIn       *kubeAuthState `json:"googleSignIn,omitempty"`
	Account            string         `json:"account,omitempty"`
}

// gcpClient calls Compute Engine with one service account key. The key goes only to its
// token_uri (signed, never as is), the token only to Google's APIs: never in a URL, a message or
// a log.
type gcpClient struct {
	project, zone string
	token         string
	expiry        time.Time
	sa            gcpServiceAccount
	raw           string // the key file's text, for mint
	number        string // the project number, learned from Compute: scrubbed from messages
	// user is the Google-account sign-in (cloud_gcp_signin.go), nil for a key; account its email.
	user    *gcpUser
	account string
}

func newGCPClient(credJSON string) (*gcpClient, error) {
	var c gcpCredential

	if strings.TrimSpace(credJSON) != "" {
		if err := json.Unmarshal([]byte(credJSON), &c); err != nil {
			return nil, errors.New("invalid Google Cloud credential data") //nolint:nilerr // the parser's message may quote the key
		}
	}

	if c.usesGoogleAccount() {
		return newGCPUserClient(c)
	}

	if strings.TrimSpace(c.ServiceAccountJSON) == "" {
		return nil, errGCPNoCredential
	}

	sa, _, err := parseServiceAccount(c.ServiceAccountJSON)
	if err != nil {
		return nil, err
	}

	project := cmp.Or(strings.TrimSpace(c.Project), sa.ProjectID)
	if project == "" {
		return nil, errors.New("the service account key names no project: give one")
	}

	if !gcpProjectPattern.MatchString(project) {
		return nil, fmt.Errorf("%q is not a project ID", project)
	}

	zone := strings.ToLower(strings.TrimSpace(c.Zone))
	if zone != "" && !gcpZonePattern.MatchString(zone) {
		return nil, fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", zone)
	}

	return &gcpClient{project: project, zone: zone, sa: sa, raw: c.ServiceAccountJSON}, nil
}

// scrub drops what a message must not carry: the private key, the token, the key's email and id,
// the project number.
func (c *gcpClient) scrub(message string) string {
	for _, secret := range append([]string{c.sa.PrivateKey, c.token, c.sa.ClientEmail, c.sa.KeyID, c.number}, c.userSecrets()...) {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "…")
		}
	}

	message = gcpTokenPattern.ReplaceAllString(message, "…")
	message = gcpEmailPattern.ReplaceAllString(message, "…")

	return gcpNumberPattern.ReplaceAllString(message, "${1}…")
}

// mint gets a cloud-platform token with the key, as the GKE sign-in does, and keeps it for the
// run (minted again under the refresh margin).
func (c *gcpClient) mint(ctx context.Context) error {
	if c.token != "" && time.Until(c.expiry) > tokenRefreshMargin {
		return nil
	}

	if c.user != nil {
		token, expiry, err := c.mintUser(ctx)
		if err != nil {
			return &gcpScrubbed{message: "Google sign-in: " + c.scrub(err.Error()), err: err}
		}

		c.token, c.expiry = token, expiry

		return nil
	}

	token, expiry, _, err := gkeMethod{}.mint(ctx, kubeAuthState{Method: authGKE, Secrets: map[string]string{gcpFieldServiceAccount: c.raw}})
	if err != nil {
		return &gcpScrubbed{message: "Google sign-in with the service account key: " + c.scrub(err.Error()), err: err}
	}

	if token == "" {
		return errors.New("no access token for the service account key")
	}

	c.token, c.expiry = token, expiry

	return nil
}

// compute is Compute Engine's address of path, in project.
func (c *gcpClient) compute(project, path string) string {
	return gcpComputeEndpoint + "/compute/v1/projects/" + url.PathEscape(project) + path
}

// googleHost: u is on one of the Google APIs the token may go to.
func googleHost(u string) bool {
	for _, e := range []string{gcpComputeEndpoint, gcpCRMEndpoint, gcpBillingEndpoint} {
		if strings.HasPrefix(u, strings.TrimSuffix(e, "/")+"/") {
			return true
		}
	}

	return false
}

// gcpAPIError is Google's error body.
type gcpAPIError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Errors  []struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"errors"`
		Details []struct {
			Type     string            `json:"@type"`
			Reason   string            `json:"reason"`
			Metadata map[string]string `json:"metadata"`
		} `json:"details"`
	} `json:"error"`
}

// do sends one call with the token and the key's project as the quota project, and decodes its
// JSON answer into out.
func (c *gcpClient) do(ctx context.Context, method, u string, body, out any) (int, error) {
	if !googleHost(u) {
		return 0, errors.New("an address outside Google's APIs: refused")
	}

	if err := c.mint(ctx); err != nil {
		return 0, err
	}

	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}

		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return 0, errors.New("a Google Cloud address that is not one")
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Goog-User-Project", c.project)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return 0, &gcpScrubbed{message: c.scrub(err.Error()), err: err}
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, gcpMaxBody))
	if err != nil {
		return resp.StatusCode, &gcpScrubbed{message: c.scrub(err.Error()), err: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, c.apiError(method, u, resp.StatusCode, data)
	}

	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return resp.StatusCode, nil
	}

	return resp.StatusCode, json.Unmarshal(data, out)
}

// apiError reads Google's error body: the reason, the permission it names, an API to enable.
func (c *gcpClient) apiError(method, u string, status int, data []byte) *gcpError {
	var body gcpAPIError

	_ = json.Unmarshal(data, &body) //nolint:errcheck

	b := body.Error
	e := &gcpError{action: cmp.Or(gcpPermissionOf(method, u), method+" "+gcpPathOf(u)), status: status, message: c.scrub(b.Message), project: c.project}

	if m := gcpPermissionPattern.FindStringSubmatch(b.Message); m != nil {
		e.action = m[1]
	}

	e.reason = b.Status
	if len(b.Errors) > 0 && b.Errors[0].Reason != "" {
		e.reason = b.Errors[0].Reason
	}

	for _, d := range b.Details {
		if d.Reason == gcpReasonDisabled {
			e.reason, e.service = gcpReasonDisabled, d.Metadata["service"]
			e.activationURL = c.activation(d.Metadata["activationUrl"])
		}

		if d.Reason == gcpReasonUserProjectDenied {
			e.reason = gcpReasonUserProjectDenied
		}
	}

	if e.reason == "accessNotConfigured" && e.service == "" {
		e.service = gcpServiceOf(u)
	}

	return e
}

// activation is an activation URL with the project's id instead of its number.
func (c *gcpClient) activation(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Hostname(), ".google.com") {
		return ""
	}

	q := u.Query()
	if q.Get("project") != "" {
		q.Set("project", c.project)
	}

	u.RawQuery = q.Encode()

	return u.String()
}

// gcpServiceOf is the API a URL calls.
func gcpServiceOf(u string) string {
	switch {
	case strings.HasPrefix(u, gcpCRMEndpoint):
		return "cloudresourcemanager.googleapis.com"
	case strings.HasPrefix(u, gcpBillingEndpoint):
		return "cloudbilling.googleapis.com"
	}

	return "compute.googleapis.com"
}

// gcpPathOf is a URL's path after the project, for a message: no project, no query.
func gcpPathOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "a call"
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if i := slices.Index(parts, "projects"); i >= 0 && i+2 <= len(parts) {
		return strings.Join(parts[i+2:], "/")
	}

	return u.Path
}

// gcpPermissionOf is the IAM permission a Compute Engine call needs, "" for another API (the
// Resource Manager and Billing calls the app makes need none, or the project's own). It names a
// refusal, and the tests check the printed role against the calls.
func gcpPermissionOf(method, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.Contains(u.Path, "/compute/v1/projects/") {
		return ""
	}

	_, rest, _ := strings.Cut(u.Path, "/compute/v1/projects/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")[1:]

	var collection []string // <collection>[/<name>[/<method>]] under global, a zone or a region

	switch {
	case len(parts) == 0:
		return "compute.projects.get"
	case parts[0] == "aggregated" && len(parts) == 2:
		return "compute." + parts[1] + ".list"
	case parts[0] == "global":
		collection = parts[1:]
	case len(parts) == 1:
		return "compute." + parts[0] + ".list"
	case len(parts) == 2:
		return "compute." + parts[0] + ".get"
	default:
		collection = parts[2:]
	}

	switch {
	case len(collection) == 0:
		return ""
	case collection[0] == "operations" && parts[0] == "global":
		return "compute.globalOperations.get"
	case collection[0] == "operations":
		return "compute.zoneOperations.get"
	case len(collection) == 3 && collection[2] == "serialPort":
		return "compute.instances.getSerialPortOutput"
	case len(collection) == 3 && method == http.MethodPost:
		return "compute." + collection[0] + "." + collection[2]
	case len(collection) == 1 && method == http.MethodPost:
		return "compute." + collection[0] + ".create"
	case len(collection) == 1:
		return "compute." + collection[0] + ".list"
	case method == http.MethodDelete:
		return "compute." + collection[0] + ".delete"
	case method == http.MethodPatch:
		return "compute." + collection[0] + ".update"
	}

	return "compute." + collection[0] + ".get"
}

// get is do for a read.
func (c *gcpClient) get(ctx context.Context, u string, out any) error {
	_, err := c.do(ctx, http.MethodGet, u, nil, out)

	return err
}

// gcpPages reads every page of a Google listing ({…,"nextPageToken"}), each handed to read.
func gcpPages(ctx context.Context, c *gcpClient, u string, read func(page map[string]json.RawMessage) error) error {
	token := ""

	for range gcpMaxPages {
		next := u
		if token != "" {
			sep := "?"
			if strings.Contains(u, "?") {
				sep = "&"
			}

			next += sep + "pageToken=" + url.QueryEscape(token)
		}

		var page map[string]json.RawMessage
		if err := c.get(ctx, next, &page); err != nil {
			return err
		}

		if err := read(page); err != nil {
			return err
		}

		token = ""
		if raw, ok := page["nextPageToken"]; ok {
			_ = json.Unmarshal(raw, &token) //nolint:errcheck
		}

		if token == "" {
			return nil
		}
	}

	return nil
}

// gcpList reads every page of a listing whose items are under key ({"items":[…]}).
func gcpList[T any](ctx context.Context, c *gcpClient, u, key string) ([]T, error) {
	out := []T{}

	err := gcpPages(ctx, c, u, func(page map[string]json.RawMessage) error {
		raw, ok := page[key]
		if !ok {
			return nil
		}

		var items []T
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}

		out = append(out, items...)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// gcpOperation is a zone operation.
type gcpOperation struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  *struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	} `json:"error"`
}

// gcpTransient: worth trying again (rate limit, a server error, no answer).
func gcpTransient(err error) bool {
	var e *gcpError
	if errors.As(err, &e) {
		switch e.reason {
		case "rateLimitExceeded", "userRateLimitExceeded", "backendError", "internalError":
			return true
		}

		return e.status == http.StatusTooManyRequests || e.status >= http.StatusInternalServerError
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// waitOperation follows a zone operation (a global one when zone is "") until it is DONE, on its
// wait method (which blocks up to two minutes, often past the HTTP client's timeout: then it asks
// again). An operation that ends with errors is a *gcpError.
func (c *gcpClient) waitOperation(ctx context.Context, project, zone, name string, interval time.Duration) error {
	if name == "" {
		return nil
	}

	scope := "/global"
	if zone != "" {
		scope = "/zones/" + url.PathEscape(zone)
	}

	u := c.compute(project, scope+"/operations/"+url.PathEscape(name)+"/wait")

	for {
		var op gcpOperation

		_, err := c.do(ctx, http.MethodPost, u, nil, &op)

		switch {
		case err == nil && op.Status == gcpOpDone && op.Error != nil && len(op.Error.Errors) > 0:
			first := op.Error.Errors[0]

			return &gcpError{action: "operation " + op.Name, reason: first.Code, message: c.scrub(first.Message), status: http.StatusOK, project: c.project, zone: zone}
		case err == nil && op.Status == gcpOpDone:
			return nil
		case err != nil && ctx.Err() == nil && (gcpTransient(err) || errors.Is(err, context.DeadlineExceeded)):
		case err != nil:
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// testPermissions is the subset of permissions the key holds on project. It needs no permission
// of its own; it is on the Cloud Resource Manager API.
func (c *gcpClient) testPermissions(ctx context.Context, project string, permissions []string) ([]string, error) {
	var answer struct {
		Permissions []string `json:"permissions"`
	}

	u := gcpCRMEndpoint + "/v1/projects/" + url.PathEscape(project) + ":testIamPermissions"
	if _, err := c.do(ctx, http.MethodPost, u, map[string][]string{"permissions": permissions}, &answer); err != nil {
		return nil, err
	}

	return answer.Permissions, nil
}

// gcpCreatePermissions are what a create needs: the instance's insert, reads, delete, the boot
// disk from the image, the subnet (with an external address when asked) and the operation.
func gcpCreatePermissions(publicIP bool) []string {
	p := []string{
		"compute.instances.create", "compute.instances.get", "compute.instances.delete", "compute.instances.setMetadata",
		"compute.instances.setLabels", "compute.instances.setTags", "compute.disks.create", "compute.images.useReadOnly",
		"compute.images.get", "compute.machineTypes.get", "compute.subnetworks.use", "compute.subnetworks.get",
		"compute.zoneOperations.get",
	}

	if publicIP {
		p = append(p, "compute.subnetworks.useExternalIp")
	}

	return p
}

// gcpOptionsPermissions are the reads the options, the test and a failed run's details make.
func gcpOptionsPermissions() []string {
	return []string{
		"compute.projects.get", "compute.zones.list", "compute.machineTypes.list", "compute.networks.list",
		"compute.subnetworks.list", "compute.images.list", "compute.routers.list", "compute.instances.getSerialPortOutput",
		"resourcemanager.projects.get",
	}
}

// gcpImagePermissions are what an image run needs on top of a create's: the helper's data disk
// (labelled, read by the image, then deleted), its stop and serial console, and the image.
func gcpImagePermissions() []string {
	return []string{
		"compute.disks.setLabels", "compute.disks.get", "compute.disks.use", "compute.disks.useReadOnly", "compute.disks.delete",
		"compute.instances.stop", "compute.instances.getSerialPortOutput", "compute.images.create", "compute.images.delete",
		"compute.images.setLabels", "compute.globalOperations.get",
	}
}

// gcpRolePermissions is the printed role's list: every permission the app may use, once.
func gcpRolePermissions() []string {
	out := []string{}

	for _, p := range slices.Concat(gcpCreatePermissions(true), gcpOptionsPermissions(), gcpImagePermissions(), gcpNetworkPermissions()) {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}

	return out
}

// missingPermissions is what of wanted granted lacks, in wanted's order.
func missingPermissions(granted, wanted []string) []string {
	out := []string{}

	for _, p := range wanted {
		if !slices.Contains(granted, p) {
			out = append(out, p)
		}
	}

	return out
}

type gcpTestResult struct {
	Account   string   `json:"account"`
	Project   string   `json:"project"`
	CanCreate bool     `json:"canCreate"`
	Checked   bool     `json:"checked"`
	Missing   []string `json:"missing"`
	// Credential is the credential JSON to seal again when a Google sign-in changed, else "";
	// RenewHint says how to sign in again once Google refuses it ("" for a key).
	Credential string `json:"credential,omitempty"`
	RenewHint  string `json:"renewHint,omitempty"`
}

// GCPTest checks a service account key. JSON: {"account","project","canCreate","checked",
// "missing"}: a token from the key, the project on Compute Engine (refused when the API is not
// enabled, saying where to enable it), then testIamPermissions with every permission of the
// printed role: missing lists those the key lacks, canCreate is true when none a create needs is
// missing; checked false (and canCreate false) when Resource Manager refuses the check. Google
// has no dry run: this reads only, and creates nothing. credJSON: {"serviceAccountJson",
// "project" (default the key's project_id),"zone"}.
func GCPTest(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newGCPClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gcpOptionsTimeout)
	defer cancel()

	if err := c.readProject(ctx); err != nil {
		return "", err
	}

	result := gcpTestResult{Account: cmp.Or(c.sa.ClientEmail, c.account), Project: c.project, Missing: []string{}, RenewHint: c.renewHint()}

	granted, err := c.testPermissions(ctx, c.project, gcpRolePermissions())
	if err == nil {
		result.Checked = true
		result.Missing = missingPermissions(granted, gcpRolePermissions())
		result.CanCreate = len(missingPermissions(granted, gcpCreatePermissions(false))) == 0
	}

	result.Credential = c.updated()

	return toJSON(result)
}

// readProject reads the project on Compute Engine (the API is enabled, the key sees it) and
// learns its number, to scrub it from messages.
func (c *gcpClient) readProject(ctx context.Context) error {
	var p struct {
		ID string `json:"id"`
	}

	if err := c.get(ctx, c.compute(c.project, ""), &p); err != nil {
		return err
	}

	c.number = p.ID

	return nil
}

// GCPRoleDefinition is the custom role to create (gcloud iam roles create ichorNodeCreator
// --project … --file …) and grant the service account on the project: only the calls the app
// makes, the image run's included. GCP IAM cannot limit a delete to labelled instances: the app
// deletes only the instance its run named after the host name and labelled ichor-run, and the
// helper, the data disk and an unfinished image of its own image run. The Talos and Omni guides'
// roles/compute.instanceAdmin.v1 is wider. The guided network's firewall permissions are in it:
// compute.networks.updatePolicy is what lets the app add rules, and only on the project's
// networks. A placeholder stands for an empty project. The person pastes it.
func GCPRoleDefinition(project string) string {
	project = cmp.Or(strings.TrimSpace(project), "PROJECT_ID")

	var b strings.Builder

	b.WriteString("# gcloud iam roles create ichorNodeCreator --project " + project + " --file this-file.yaml\n")
	b.WriteString("# gcloud projects add-iam-policy-binding " + project + " --member serviceAccount:SERVICE_ACCOUNT_EMAIL" +
		" --role projects/" + project + "/roles/ichorNodeCreator\n")
	b.WriteString("# Signed in with a Google account instead: --member user:YOUR_EMAIL (and roles/serviceusage.serviceUsageConsumer).\n")
	b.WriteString("# Wider, as the Talos and Omni guides say: roles/compute.instanceAdmin.v1. GCP IAM cannot limit a delete to\n")
	b.WriteString("# labelled instances: Ichor deletes only the instance its run named after the host name and labelled ichor-run.\n")
	b.WriteString("title: Ichor node creator\n")
	b.WriteString("description: Lets Ichor create a Talos node VM (with its boot disk) or a Talos image in this project and delete what it created\n")
	b.WriteString("stage: GA\n")
	b.WriteString("includedPermissions:\n")

	for _, p := range gcpRolePermissions() {
		b.WriteString("- " + p + "\n")
	}

	return b.String()
}

// gcpLabelOfVersion and gcpVersionOfLabel write a Talos version as a label value (v1-12-0) and
// read it back.
func gcpLabelOfVersion(version string) string {
	return strings.ToLower(strings.ReplaceAll(version, ".", "-"))
}

func gcpVersionOfLabel(label string) string {
	if m := gcpVersionLabel.FindStringSubmatch(label); m != nil {
		return "v" + m[1] + "." + m[2] + "." + m[3] + m[4]
	}

	return label
}
