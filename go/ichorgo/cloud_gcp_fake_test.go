package ichorgo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// An invented project, key and resources.
const (
	gcpTestProject = "example-project"
	gcpTestNumber  = "123456789012"
	gcpTestEmail   = "node-creator@example-project.iam.gserviceaccount.com"
	gcpTestKeyID   = "0123456789abcdef0123456789abcdef01234567"
	gcpTestToken   = "ya29.sample-compute-token"
	gcpTestZone    = "europe-west1-b"
	gcpTestRegion  = "europe-west1"

	gcpTestBase   = "/compute/v1/projects/" + gcpTestProject
	gcpTestLink   = "https://www.googleapis.com/compute/v1/projects/" + gcpTestProject
	gcpTestSubnet = gcpTestLink + "/regions/" + gcpTestRegion + "/subnetworks/nodes"
)

var (
	gcpTestKeyOnce sync.Once
	gcpTestKeyPEM  string
)

// gcpTestKeyJSON is a service account key of a throwaway RSA key, made once.
func gcpTestKeyJSON(t *testing.T) string {
	t.Helper()

	gcpTestKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}

		der, _ := x509.MarshalPKCS8PrivateKey(key)
		gcpTestKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	})

	data, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": gcpTestProject, "private_key_id": gcpTestKeyID, "private_key": gcpTestKeyPEM,
		"client_email": gcpTestEmail, "client_id": "100000000000000000001",
	})

	return string(data)
}

// gcpTestCred is credJSON with the test key and the zone.
func gcpTestCred(t *testing.T) string {
	t.Helper()

	data, _ := json.Marshal(map[string]string{"serviceAccountJson": gcpTestKeyJSON(t), "zone": gcpTestZone})

	return string(data)
}

// fakeGCE is the part of Google's token endpoint, Compute Engine, Cloud Resource Manager and the
// Cloud Billing Catalog the app calls. It records every token, every Compute call, every insert
// and every delete.
type fakeGCE struct {
	mu  sync.Mutex
	url string

	noCreateRight   bool   // testIamPermissions leaves the instance and disk create out
	apiDisabled     bool   // Compute Engine is not enabled
	crmDisabled     bool   // Cloud Resource Manager is not enabled
	billingDisabled bool   // Cloud Billing is not enabled
	createStatus    int    // the status the insert answers, after creating when >= 500
	firstInsertLost bool   // the first insert creates the instance and answers 503
	endState        string // the instance's status after the insert, RUNNING by default
	privateOnly     bool   // the instance gets no external address
	archMismatch    bool   // the image is ARM64, without UEFI_COMPATIBLE
	existing        bool   // an instance node-c of another run is there
	nat             bool   // the default network has a Cloud NAT
	slowDelete      bool   // an accepted delete never finishes, and the instance stays
	onRunning       func()
	image           *fakeGCEImage   // the image run's helper, data disk and images (cloud_gcp_image_test.go)
	network         *fakeGCENetwork // the default network and its firewall rules (cloud_gcp_network_test.go)

	tokens        int
	badScope      int
	unauthorized  int
	noQuotaHeader int
	calls         []string // "METHOD path", Compute Engine only
	inserted      []map[string]any
	requestIDs    []string
	deleted       []string
	instances     map[string]map[string]any
	ops           map[string]map[string]any
}

func startFakeGCE(t *testing.T, f *fakeGCE) {
	t.Helper()

	f.instances, f.ops = map[string]map[string]any{}, map[string]map[string]any{}

	if f.existing {
		f.instances["node-c"] = map[string]any{"labels": map[string]any{gcpLabelRun: "another-run"}}
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	f.url = srv.URL

	client, token, compute, crm, billing := cloudHTTP, gcpTokenTarget, gcpComputeEndpoint, gcpCRMEndpoint, gcpBillingEndpoint
	cloudHTTP = func() *http.Client { return srv.Client() }
	gcpTokenTarget = func(string) string { return srv.URL + "/token" }
	gcpComputeEndpoint, gcpCRMEndpoint, gcpBillingEndpoint = srv.URL, srv.URL, srv.URL

	t.Cleanup(func() {
		cloudHTTP, gcpTokenTarget, gcpComputeEndpoint, gcpCRMEndpoint, gcpBillingEndpoint = client, token, compute, crm, billing
	})
}

func gceJSON(w http.ResponseWriter, status int, v any) {
	data, _ := json.Marshal(v)
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func gceFail(w http.ResponseWriter, status int, reason, message string) {
	gceJSON(w, status, map[string]any{"error": map[string]any{
		"code": status, "message": message, "errors": []any{map[string]string{"domain": "global", "reason": reason, "message": message}},
	}})
}

// gceDisabled is Google's answer for an API not enabled, naming the project by its number.
func gceDisabled(w http.ResponseWriter, service, name string) {
	gceJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{
		"code":    403,
		"message": name + " API has not been used in project " + gcpTestNumber + " before or it is disabled.",
		"errors":  []any{map[string]string{"domain": "usageLimits", "reason": "accessNotConfigured", "message": "disabled"}},
		"status":  "PERMISSION_DENIED",
		"details": []any{map[string]any{
			"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": gcpReasonDisabled, "domain": "googleapis.com",
			"metadata": map[string]string{
				"service": service, "consumer": "projects/" + gcpTestNumber,
				"activationUrl": "https://console.developers.google.com/apis/api/" + service + "/overview?project=" + gcpTestNumber,
			},
		}},
	}})
}

func (f *fakeGCE) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := r.URL.Path

	if path == "/token" {
		f.token(w, r)

		return
	}

	if r.Header.Get("Authorization") != "Bearer "+gcpTestToken {
		f.unauthorized++
		gceFail(w, http.StatusUnauthorized, "authError", "Request had invalid authentication credentials.")

		return
	}

	if r.Header.Get("X-Goog-User-Project") != gcpTestProject {
		f.noQuotaHeader++
	}

	switch {
	case strings.HasSuffix(path, ":testIamPermissions"):
		f.permissions(w, r)
	case path == "/v1/projects":
		f.projects(w)
	case path == "/v1/services/"+gcpComputeService+"/skus":
		f.skus(w, r)
	case strings.HasPrefix(path, "/compute/v1/projects/debian-cloud/") && f.image != nil:
		f.calls = append(f.calls, r.Method+" "+path)
		f.image.debian(w, strings.TrimPrefix(path, "/compute/v1/projects/debian-cloud/global/images/family/"))
	case strings.HasPrefix(path, gcpTestBase):
		f.calls = append(f.calls, r.Method+" "+path)

		if f.apiDisabled {
			gceDisabled(w, "compute.googleapis.com", "Compute Engine")

			return
		}

		f.compute(w, r, strings.TrimPrefix(path, gcpTestBase))
	default:
		gceFail(w, http.StatusNotFound, gcpReasonNotFound, "no such path "+path)
	}
}

// token answers the JWT bearer grant, for an assertion of the test key with the cloud-platform
// scope only.
func (f *fakeGCE) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	f.tokens++

	parts := strings.Split(r.PostForm.Get("assertion"), ".")
	if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || len(parts) != 3 {
		gceJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})

		return
	}

	var claims struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
	}

	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if json.Unmarshal(payload, &claims) != nil || claims.Scope != gcpScope || claims.Iss != gcpTestEmail {
		f.badScope++
		gceJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_scope"})

		return
	}

	gceJSON(w, http.StatusOK, map[string]any{"access_token": gcpTestToken, "expires_in": 3599, "token_type": "Bearer"})
}

func (f *fakeGCE) permissions(w http.ResponseWriter, r *http.Request) {
	if f.crmDisabled {
		gceDisabled(w, "cloudresourcemanager.googleapis.com", "Cloud Resource Manager")

		return
	}

	var body struct {
		Permissions []string `json:"permissions"`
	}

	_ = json.NewDecoder(r.Body).Decode(&body)

	granted := slices.DeleteFunc(body.Permissions, func(p string) bool {
		return (f.noCreateRight && (p == "compute.instances.create" || p == "compute.disks.create")) ||
			(f.image != nil && f.image.noImageRight && p == "compute.images.create") ||
			(f.network != nil && slices.Contains(f.network.missing, p))
	})

	gceJSON(w, http.StatusOK, map[string]any{"permissions": granted})
}

func (f *fakeGCE) projects(w http.ResponseWriter) {
	if f.crmDisabled {
		gceDisabled(w, "cloudresourcemanager.googleapis.com", "Cloud Resource Manager")

		return
	}

	gceJSON(w, http.StatusOK, map[string]any{"projects": []any{
		map[string]string{"projectId": "sample-other", "name": "Other sample"},
		map[string]string{"projectId": gcpTestProject, "name": "Example project"},
	}})
}

func gceSKU(description, region string, units string, nanos int64, usage string) map[string]any {
	return map[string]any{
		"description": description, "category": map[string]string{"resourceFamily": "Compute", "usageType": usage},
		"serviceRegions": []string{region},
		"pricingInfo": []any{map[string]any{"pricingExpression": map[string]any{"tieredRates": []any{
			map[string]any{"unitPrice": map[string]any{"currencyCode": "USD", "units": units, "nanos": nanos}},
		}}}},
	}
}

// gceStorageSKU is a storage SKU priced per GiB-month.
func gceStorageSKU(description, region string, nanos int64) map[string]any {
	s := gceSKU(description, region, "0", nanos, "OnDemand")
	s["category"] = map[string]string{"resourceFamily": "Storage", "usageType": "OnDemand"}

	return s
}

func (f *fakeGCE) skus(w http.ResponseWriter, r *http.Request) {
	if f.billingDisabled {
		gceDisabled(w, "cloudbilling.googleapis.com", "Cloud Billing")

		return
	}

	if r.URL.Query().Get("pageToken") == "" {
		gceJSON(w, http.StatusOK, map[string]any{"skus": []any{
			gceSKU("Spot Preemptible E2 Instance Core running in Belgium", gcpTestRegion, "0", 7000000, "Preemptible"),
			gceSKU("E2 Instance Core running in Americas", "us-central1", "0", 21000000, "OnDemand"),
			gceSKU("N2D Instance Core running in Belgium", gcpTestRegion, "0", 30000000, "OnDemand"),
			gceSKU("E2 Instance Core running in Belgium", gcpTestRegion, "0", 22000000, "OnDemand"),
			gceStorageSKU("Balanced PD Capacity in Americas", "us-central1", 100000000),
			gceStorageSKU("Balanced PD Capacity in Belgium", gcpTestRegion, 110000000),
			gceStorageSKU("Storage Image", "global", 50000000),
		}, "nextPageToken": "2"})

		return
	}

	gceJSON(w, http.StatusOK, map[string]any{"skus": []any{gceSKU("E2 Instance Ram running in Belgium", gcpTestRegion, "0", 3000000, "OnDemand")}})
}

func gceMachineType(name string, cpus, memoryMB int, arch, deprecated string) map[string]any {
	t := map[string]any{"name": name, "guestCpus": cpus, "memoryMb": memoryMB, "architecture": arch}
	if deprecated != "" {
		t["deprecated"] = map[string]string{"state": deprecated}
	}

	return t
}

// static is every read the fake answers that is not an instance or an operation.
func (f *fakeGCE) static(r *http.Request, rest string) (any, bool) {
	imageArch, features := "X86_64", []any{map[string]string{"type": "VIRTIO_SCSI_MULTIQUEUE"}}
	if f.archMismatch {
		imageArch = "ARM64"
	}

	byHand := map[string]any{
		"name": "talos-by-hand", "selfLink": gcpTestLink + "/global/images/talos-by-hand", "status": "READY", "architecture": imageArch,
		"diskSizeGb": "10", "guestOsFeatures": features,
	}
	labelled := map[string]any{
		"name": "talos-v1-14-2-arm64", "selfLink": gcpTestLink + "/global/images/talos-v1-14-2-arm64", "status": "READY", "architecture": "ARM64",
		"diskSizeGb": "10", "guestOsFeatures": []any{map[string]string{"type": gcpFeatureUEFI}},
		"labels": map[string]string{gcpLabelTalosVersion: "v1-14-2", gcpLabelSchematic: "0123456789abcdef", "os": "talos"},
	}
	subnet := map[string]any{
		"name": "nodes", "selfLink": gcpTestSubnet, "region": gcpTestLink + "/regions/" + gcpTestRegion,
		"network": gcpTestLink + "/global/networks/default", "ipCidrRange": "10.132.0.0/20",
	}
	far := map[string]any{
		"name": "far", "selfLink": gcpTestLink + "/regions/us-central1/subnetworks/far", "region": gcpTestLink + "/regions/us-central1",
		"network": gcpTestLink + "/global/networks/default", "ipCidrRange": "10.128.0.0/20",
	}

	types := []any{gceMachineType("e2-medium", 2, 4096, "X86_64", ""), gceMachineType("n1-old-1", 1, 3840, "", "DEPRECATED")}
	if r.URL.Query().Get("pageToken") == "2" {
		types = []any{gceMachineType("t2a-standard-2", 2, 8192, "ARM64", ""), gceMachineType("e2-micro", 2, 1024, "", "")}
	}

	routers := []any{}
	if f.nat {
		routers = append(routers, map[string]any{"network": gcpTestLink + "/global/networks/default", "nats": []any{map[string]string{"name": "nat"}}})
	}

	list := func(items ...any) map[string]any { return map[string]any{"items": items} }
	zone := "/zones/" + gcpTestZone

	answers := map[string]any{
		"": map[string]string{"name": gcpTestProject, "id": gcpTestNumber},
		"/zones": list(
			map[string]string{"name": "us-central1-a", "region": gcpTestLink + "/regions/us-central1", "status": "UP"},
			map[string]string{"name": "europe-west1-d", "region": gcpTestLink + "/regions/europe-west1", "status": "DOWN"},
			map[string]string{"name": "europe-west1-c", "region": gcpTestLink + "/regions/europe-west1", "status": "UP"},
			map[string]string{"name": gcpTestZone, "region": gcpTestLink + "/regions/europe-west1", "status": "UP"},
		),
		"/global/networks": list(
			map[string]any{"name": "default", "selfLink": gcpTestLink + "/global/networks/default", "autoCreateSubnetworks": true},
			map[string]any{"name": "empty", "selfLink": gcpTestLink + "/global/networks/empty"},
		),
		"/aggregated/subnetworks": map[string]any{"items": map[string]any{
			"regions/" + gcpTestRegion: map[string]any{"subnetworks": []any{subnet}},
			"regions/us-central1":      map[string]any{"subnetworks": []any{far}},
			"regions/asia-east1":       map[string]any{"warning": map[string]string{"code": "NO_RESULTS_ON_PAGE"}},
		}},
		"/regions/" + gcpTestRegion + "/subnetworks/nodes": subnet,
		"/regions/us-central1/subnetworks/far":             far,
		"/regions/" + gcpTestRegion + "/routers":           list(routers...),
		"/global/firewalls":                                list(),
		"/global/images":                                   list(byHand, labelled, map[string]any{"name": "talos-uploading", "status": "PENDING"}),
		"/global/images/talos-by-hand":                     byHand,
		"/global/images/talos-v1-14-2-arm64":               labelled,
		zone + "/machineTypes/e2-medium":                   gceMachineType("e2-medium", 2, 4096, "X86_64", ""),
		zone + "/machineTypes/t2a-standard-2":              gceMachineType("t2a-standard-2", 2, 8192, "ARM64", ""),
		zone + "/machineTypes/e2-small":                    gceMachineType("e2-small", 2, 2048, "X86_64", ""),
		zone + "/machineTypes/t2a-standard-1":              gceMachineType("t2a-standard-1", 1, 4096, "ARM64", ""),
	}

	if rest == zone+"/machineTypes" {
		page := map[string]any{"items": types}
		if r.URL.Query().Get("pageToken") == "" {
			page["nextPageToken"] = "2"
		}

		return page, true
	}

	answer, ok := answers[rest]

	return answer, ok
}

func (f *fakeGCE) compute(w http.ResponseWriter, r *http.Request, rest string) {
	instances := "/zones/" + gcpTestZone + "/instances"

	if f.image != nil && f.image.handle(f, w, r, rest) {
		return
	}

	if f.network != nil && f.network.handle(f, w, r, rest) {
		return
	}

	switch {
	case r.Method == http.MethodPost && rest == instances:
		f.insert(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(rest, "/zones/"+gcpTestZone+"/operations/") && strings.HasSuffix(rest, "/wait"):
		name := strings.TrimSuffix(strings.TrimPrefix(rest, "/zones/"+gcpTestZone+"/operations/"), "/wait")
		if op, ok := f.ops[name]; ok {
			gceJSON(w, http.StatusOK, op)
		} else {
			gceFail(w, http.StatusNotFound, gcpReasonNotFound, "no operation "+name)
		}
	case strings.HasSuffix(rest, "/serialPort"):
		gceJSON(w, http.StatusOK, map[string]string{"contents": strings.Repeat("boot line\n", 400) + "[talos] no config in user-data: boot failed"})
	case strings.HasPrefix(rest, instances+"/"):
		f.instance(w, r, strings.TrimPrefix(rest, instances+"/"))
	case r.Method == http.MethodGet:
		if answer, ok := f.static(r, rest); ok {
			gceJSON(w, http.StatusOK, answer)

			return
		}

		gceFail(w, http.StatusNotFound, gcpReasonNotFound, "The resource 'projects/"+gcpTestProject+rest+"' was not found")
	default:
		gceFail(w, http.StatusMethodNotAllowed, "badRequest", r.Method)
	}
}

func (f *fakeGCE) operation(kind string, done bool) map[string]any {
	name := kind + "-" + strconv.Itoa(len(f.ops)+1)
	op := map[string]any{"name": name, "status": "RUNNING"}

	if done {
		op["status"] = gcpOpDone
	}

	f.ops[name] = op

	return op
}

func (f *fakeGCE) insert(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gceFail(w, http.StatusBadRequest, "invalid", err.Error())

		return
	}

	id := r.URL.Query().Get("requestId")
	retry := slices.Contains(f.requestIDs, id)

	f.inserted = append(f.inserted, body)
	f.requestIDs = append(f.requestIDs, id)

	name, _ := body["name"].(string)

	if f.createStatus != 0 {
		reason := "invalid"
		if f.createStatus >= http.StatusInternalServerError {
			f.instances[name], reason = map[string]any{"labels": body["labels"]}, "backendError"
		}

		gceFail(w, f.createStatus, reason, "something broke")

		return
	}

	if !retry {
		f.instances[name] = map[string]any{"labels": body["labels"]}
	}

	if f.firstInsertLost && !retry {
		gceFail(w, http.StatusServiceUnavailable, "backendError", "the answer was lost")

		return
	}

	gceJSON(w, http.StatusOK, f.operation("insert", true))
}

func (f *fakeGCE) instance(w http.ResponseWriter, r *http.Request, name string) {
	body, ok := f.instances[name]
	if !ok {
		gceFail(w, http.StatusNotFound, gcpReasonNotFound, "The resource 'projects/"+gcpTestProject+"/zones/"+gcpTestZone+"/instances/"+name+"' was not found")

		return
	}

	if r.Method == http.MethodDelete {
		f.deleted = append(f.deleted, name)

		if !f.slowDelete {
			delete(f.instances, name)
		}

		gceJSON(w, http.StatusOK, f.operation("delete", !f.slowDelete))

		return
	}

	status := "RUNNING"
	if f.endState != "" {
		status = f.endState
	} else if f.onRunning != nil {
		f.onRunning()
		f.onRunning = nil
	}

	nic := map[string]any{"networkIP": "10.132.0.20"}
	if !f.privateOnly {
		nic["accessConfigs"] = []any{map[string]string{"type": "ONE_TO_ONE_NAT", "name": "External NAT", "natIP": joinNewNode}}
	}

	gceJSON(w, http.StatusOK, map[string]any{
		"name": name, "status": status, "labels": body["labels"], "networkInterfaces": []any{nic},
		"statusMessage": fmt.Sprintf("instance %s stopped by the guest", name),
	})
}
