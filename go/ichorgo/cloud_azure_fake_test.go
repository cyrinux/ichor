package ichorgo

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Sample ids of the documentation's shape, an invented secret and invented resources.
const (
	azureTestSub      = "00000000-0000-0000-0000-000000000000"
	azureTestTenant   = "00000000-0000-0000-0000-000000000001"
	azureTestClientID = "00000000-0000-0000-0000-000000000002"
	azureTestSecret   = "sample~client~secret~value"
	azureTestToken    = "sample-arm-access-token"
	azureTestCred     = `{"tenantId":"` + azureTestTenant + `","clientId":"` + azureTestClientID + `","clientSecret":"` + azureTestSecret +
		`","subscriptionId":"` + azureTestSub + `","location":"westeurope"}`

	azureTestGroup    = "/subscriptions/" + azureTestSub + "/resourceGroups/talos/providers/"
	azureTestImage    = azureTestGroup + "Microsoft.Compute/images/talos-by-hand"
	azureTestGallery  = azureTestGroup + "Microsoft.Compute/galleries/talos"
	azureTestGalDef   = azureTestGallery + "/images/talos-arm64"
	azureTestGalImage = azureTestGalDef + "/versions/1.14.2"
	azureTestVNet     = azureTestGroup + "Microsoft.Network/virtualNetworks/talos-vnet"
	azureTestSubnet   = azureTestVNet + "/subnets/nodes"
	azureTestNSG      = azureTestGroup + "Microsoft.Network/networkSecurityGroups/talos-nodes"
	azureTestNSGOther = azureTestGroup + "Microsoft.Network/networkSecurityGroups/other-place"
	azureTestVM       = azureTestGroup + "Microsoft.Compute/virtualMachines/node-c"
	azureTestNIC      = azureTestGroup + "Microsoft.Network/networkInterfaces/node-c-nic"
	azureTestIP       = azureTestGroup + "Microsoft.Network/publicIPAddresses/node-c-ip"
)

// fakeARM is the part of Entra ID, Resource Manager and the Retail Prices API the app calls. It
// records every token request, every ARM call, every write and every delete.
type fakeARM struct {
	mu  sync.Mutex
	url string

	noCreateRight     bool   // the principal may read only
	denyDelete        bool   // notActions takes the VM delete back
	noPermissionsRead bool   // the permissions listing is refused
	createStatus      int    // the status the VM PUT answers, after creating when >= 500
	endState          string // the VM's provisioning state, Succeeded by default
	imageGeneration   string // the managed image's, V1 by default
	existingVM        bool   // a VM node-c of another run is there
	noPrices          bool
	slowDelete        bool // an accepted delete never finishes
	onRunning         func()

	tokenForms   []url.Values
	unauthorized int
	calls        []string // "METHOD path"
	puts         map[string]map[string]any
	putOrder     []string
	deleted      []string
	resources    map[string]map[string]any
	priceFilter  string
}

func startFakeARM(t *testing.T, f *fakeARM) {
	t.Helper()

	f.puts, f.resources = map[string]map[string]any{}, map[string]map[string]any{}

	if f.existingVM {
		f.resources[azureTestVM] = map[string]any{"tags": map[string]any{azureTagRun: "another-run"}}
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	f.url = srv.URL

	client, arm, authority, tlsConfig, prices := cloudHTTP, azureARMEndpoint, azureAuthority, azureTLS, azurePricesEndpoint
	cloudHTTP = func() *http.Client { return srv.Client() }
	azureARMEndpoint = func(string) string { return srv.URL }
	azureAuthority = func(_, tenant string) string { return srv.URL + "/" + tenant + "/v2.0" }
	azureTLS = func() *tls.Config {
		return &tls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs} //nolint:gosec,forcetypeassert // a test server
	}
	azurePricesEndpoint = func() string { return srv.URL }

	t.Cleanup(func() {
		cloudHTTP, azureARMEndpoint, azureAuthority, azureTLS, azurePricesEndpoint = client, arm, authority, tlsConfig, prices
	})
}

func armFail(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"code":%q,"message":%q}}`, code, message)
}

func armJSON(w http.ResponseWriter, status int, v any) {
	data, _ := json.Marshal(v)
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func (f *fakeARM) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := r.URL.Path

	switch {
	case strings.HasSuffix(path, "/v2.0/.well-known/openid-configuration"):
		tenant := strings.Split(path, "/")[1]
		armJSON(w, http.StatusOK, map[string]string{"issuer": f.url + "/" + tenant + "/v2.0", "token_endpoint": f.url + "/" + tenant + "/oauth2/v2.0/token"})

		return
	case strings.HasSuffix(path, "/oauth2/v2.0/token"):
		f.token(w, r)

		return
	case path == "/api/retail/prices":
		f.prices(w, r)

		return
	}

	if r.Header.Get("Authorization") != "Bearer "+azureTestToken {
		f.unauthorized++
		armFail(w, http.StatusUnauthorized, "InvalidAuthenticationToken", "The access token is invalid.")

		return
	}

	if strings.HasPrefix(path, "/operations/delete-") && f.slowDelete {
		w.Header().Set("Location", f.url+path+"?api-version="+azureComputeVersion)
		w.WriteHeader(http.StatusAccepted)

		return
	}

	if strings.HasPrefix(path, "/operations/") {
		armJSON(w, http.StatusOK, map[string]string{"status": azureOpSucceeded})

		return
	}

	if r.URL.Query().Get("api-version") == "" {
		armFail(w, http.StatusBadRequest, "MissingApiVersionParameter", "The api-version query parameter is required")

		return
	}

	f.calls = append(f.calls, r.Method+" "+path)

	switch r.Method {
	case http.MethodGet:
		f.get(w, r)
	case http.MethodPut:
		f.put(w, r)
	case http.MethodDelete:
		f.delete(w, path)
	default:
		armFail(w, http.StatusMethodNotAllowed, "MethodNotAllowed", r.Method)
	}
}

func (f *fakeARM) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	f.tokenForms = append(f.tokenForms, r.PostForm)

	if r.PostForm.Get("client_id") != azureTestClientID || r.PostForm.Get("client_secret") != azureTestSecret {
		armJSON(w, http.StatusUnauthorized, map[string]string{
			"error":             "invalid_client",
			"error_description": "AADSTS7000215: Invalid client secret provided for app '" + r.PostForm.Get("client_id") + "' in tenant '" + azureTestTenant + "'. Trace ID: sample",
		})

		return
	}

	armJSON(w, http.StatusOK, map[string]any{"token_type": "Bearer", "expires_in": 3599, "access_token": azureTestToken})
}

func (f *fakeARM) prices(w http.ResponseWriter, r *http.Request) {
	f.priceFilter = r.URL.Query().Get("$filter")

	if f.noPrices {
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	item := func(price float64, product, meter string) map[string]any {
		return map[string]any{"retailPrice": price, "unitOfMeasure": "1 Hour", "currencyCode": "USD", "productName": product, "meterName": meter}
	}

	armJSON(w, http.StatusOK, map[string]any{"Items": []any{
		item(0.096, "Virtual Machines BS Series Windows", "B2s"),
		item(0.0096, "Virtual Machines BS Series", "B2s Spot"),
		item(0.0192, "Virtual Machines BS Series", "B2s Low Priority"),
		item(0.048, "Virtual Machines BS Series", "B2s"),
	}})
}

// permissions is what the principal may do, by the fake's flags.
func (f *fakeARM) permissions(w http.ResponseWriter) {
	if f.noPermissionsRead {
		armFail(w, http.StatusForbidden, azureCodeAuthorizationFailed, "The client '"+azureTestClientID+"' with object id '"+azureTestClientID+
			"' does not have authorization to perform action 'Microsoft.Authorization/permissions/read' over scope '/subscriptions/"+azureTestSub+"'")

		return
	}

	perm := map[string]any{"actions": []string{"Microsoft.Compute/*", "Microsoft.Network/*", "Microsoft.Resources/subscriptions/*/read"}, "notActions": []string{}}

	switch {
	case f.noCreateRight:
		perm["actions"] = []string{"*/read"}
	case f.denyDelete:
		perm = map[string]any{"actions": []string{"*"}, "notActions": []string{"Microsoft.Compute/virtualMachines/delete"}}
	}

	armJSON(w, http.StatusOK, map[string]any{"value": []any{perm, map[string]any{"actions": []string{"Microsoft.Authorization/*/read"}}}})
}

func azureTestSKU(name, arch, generations, vcpus, memory string, restricted bool) map[string]any {
	sku := map[string]any{"resourceType": "virtualMachines", "name": name, "capabilities": []map[string]string{
		{"name": "vCPUs", "value": vcpus}, {"name": "MemoryGB", "value": memory},
		{"name": "HyperVGenerations", "value": generations}, {"name": "CpuArchitectureType", "value": arch},
	}}

	if restricted {
		sku["restrictions"] = []map[string]any{{"type": "Location", "reasonCode": "NotAvailableForSubscription"}}
	}

	return sku
}

// static is every read the fake answers that is not a resource the run made.
func (f *fakeARM) static(r *http.Request) (any, bool) {
	sub := "/subscriptions/" + azureTestSub
	located := func(id, name, location string, props map[string]any) map[string]any {
		return map[string]any{"id": id, "name": name, "location": location, "properties": props}
	}
	nsg := func(id, name, location, source string) map[string]any {
		rule := map[string]any{"properties": map[string]any{
			"direction": "Inbound", "access": "Allow", "sourceAddressPrefix": source, "destinationPortRanges": []string{"50000-50001", "6443"},
		}}

		return located(id, name, location, map[string]any{"securityRules": []any{rule}})
	}
	subnet := map[string]any{"id": azureTestSubnet, "name": "nodes", "properties": map[string]any{
		"addressPrefix": "10.0.0.0/24", "networkSecurityGroup": map[string]string{"id": azureTestNSG},
	}}
	vnet := located(azureTestVNet, "talos-vnet", "westeurope", map[string]any{"subnets": []any{subnet}})
	image := located(azureTestImage, "talos-by-hand", "westeurope", map[string]any{
		"hyperVGeneration": cmpOr(f.imageGeneration, azureGenV1), "storageProfile": map[string]any{"osDisk": map[string]string{"osType": "Linux"}},
	})
	galDef := located(azureTestGalDef, "talos-arm64", "westeurope", map[string]any{"osType": "Linux", "hyperVGeneration": azureGenV2, "architecture": "Arm64"})
	galImage := located(azureTestGalImage, "1.14.2", "westeurope", map[string]any{"publishingProfile": map[string]any{"targetRegions": []any{map[string]string{"name": "West Europe"}}}})
	galImage["tags"] = map[string]string{azureTagTalosVersion: "v1.14.2", azureTagSchematic: "sample-schematic"}

	skus := []any{
		azureTestSKU("Standard_B2s", "x64", "V1,V2", "2", "4", false),
		azureTestSKU("Standard_NC6_restricted", "x64", "V1,V2", "6", "56", true),
		map[string]any{"resourceType": "disks", "name": "Premium_LRS"},
	}
	if r.URL.Query().Get("$skiptoken") == "2" {
		skus = []any{azureTestSKU("Standard_D2ps_v5", "Arm64", "V2", "2", "8", false), azureTestSKU("Standard_A1_v2", "x64", "V1", "1", "2", false)}
	}

	list := func(items ...any) map[string]any { return map[string]any{"value": items} }

	answers := map[string]any{
		"/subscriptions": list(map[string]string{"subscriptionId": azureTestSub, "displayName": "Scratch", "state": "Enabled"}),
		sub:              map[string]string{"subscriptionId": azureTestSub, "displayName": "Scratch", "state": "Enabled"},
		sub + "/locations": list(
			map[string]any{"name": "westeurope", "displayName": "West Europe", "metadata": map[string]string{"regionType": "Physical"}},
			map[string]any{"name": "europe", "displayName": "Europe", "metadata": map[string]string{"regionType": "Logical"}},
			map[string]any{"name": "northeurope", "displayName": "North Europe", "metadata": map[string]string{"regionType": "Physical"}},
		),
		sub + "/resourcegroups": list(map[string]string{"name": "talos", "location": "westeurope"}, map[string]string{"name": "other", "location": "northeurope"}),
		sub + "/providers/Microsoft.Network/virtualNetworks": list(vnet,
			located(azureTestGroup+"Microsoft.Network/virtualNetworks/far", "far", "northeurope", map[string]any{})),
		sub + "/providers/Microsoft.Network/networkSecurityGroups": list(
			nsg(azureTestNSG, "talos-nodes", "westeurope", "Internet"),
			nsg(azureTestGroup+"Microsoft.Network/networkSecurityGroups/closed", "closed", "westeurope", "10.0.0.0/8"),
			nsg(azureTestNSGOther, "other-place", "northeurope", "*"),
		),
		sub + "/providers/Microsoft.Compute/images": list(image,
			located(azureTestGroup+"Microsoft.Compute/images/windows", "windows", "westeurope", map[string]any{"storageProfile": map[string]any{"osDisk": map[string]string{"osType": "Windows"}}})),
		sub + "/providers/Microsoft.Compute/galleries": list(located(azureTestGallery, "talos", "westeurope", map[string]any{})),
		azureTestGallery + "/images":                   list(galDef),
		azureTestGalDef + "/versions":                  list(galImage),
		azureTestImage:                                 image,
		azureTestGalDef:                                galDef,
		azureTestGalImage:                              galImage,
		azureTestSubnet:                                subnet,
		azureTestVNet:                                  vnet,
		azureTestNSG:                                   nsg(azureTestNSG, "talos-nodes", "westeurope", "Internet"),
		azureTestNSGOther:                              nsg(azureTestNSGOther, "other-place", "northeurope", "*"),
	}

	if r.URL.Path == sub+"/providers/Microsoft.Compute/skus" {
		if r.URL.Query().Get("$filter") != "location eq 'westeurope'" {
			return list(), true
		}

		page := map[string]any{"value": skus}
		if r.URL.Query().Get("$skiptoken") == "" {
			page["nextLink"] = f.url + r.URL.Path + "?api-version=" + azureSKUsVersion + "&%24filter=" + url.QueryEscape("location eq 'westeurope'") + "&%24skiptoken=2"
		}

		return page, true
	}

	answer, ok := answers[r.URL.Path]

	return answer, ok
}

func (f *fakeARM) get(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.HasSuffix(path, "/providers/Microsoft.Authorization/permissions") {
		f.permissions(w)

		return
	}

	if answer, ok := f.static(r); ok {
		armJSON(w, http.StatusOK, answer)

		return
	}

	body, ok := f.resources[path]
	if !ok {
		armFail(w, http.StatusNotFound, azureCodeNotFound, "The Resource '"+path[strings.Index(path, "/providers/")+11:]+"' under resource group 'talos' was not found.")

		return
	}

	switch path {
	case azureTestVM:
		armJSON(w, http.StatusOK, f.vm(body))
	case azureTestNIC:
		armJSON(w, http.StatusOK, map[string]any{"id": path, "properties": map[string]any{"ipConfigurations": []any{
			map[string]any{"properties": map[string]string{"privateIPAddress": "10.0.0.20"}},
		}}})
	case azureTestIP:
		armJSON(w, http.StatusOK, map[string]any{"id": path, "properties": map[string]string{"ipAddress": joinNewNode}})
	default:
		armJSON(w, http.StatusOK, body)
	}
}

func (f *fakeARM) vm(body map[string]any) map[string]any {
	status := func(code, message string) map[string]string {
		return map[string]string{"code": code, "message": message}
	}
	state, statuses := azureOpSucceeded, []any{status("ProvisioningState/succeeded", ""), status("PowerState/running", "VM running")}

	if f.endState == "Failed" {
		state, statuses = "Failed", []any{status("ProvisioningState/failed/OSProvisioningTimedOut", "OS Provisioning for VM 'node-c' did not finish in the allotted time.")}
	} else if f.onRunning != nil {
		f.onRunning()
		f.onRunning = nil
	}

	return map[string]any{"id": azureTestVM, "name": "node-c", "tags": body["tags"], "properties": map[string]any{
		"provisioningState": state, "instanceView": map[string]any{"statuses": statuses},
	}}
}

func (f *fakeARM) put(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		armFail(w, http.StatusBadRequest, "InvalidRequestContent", err.Error())

		return
	}

	path := r.URL.Path
	f.puts[path] = body
	f.putOrder = append(f.putOrder, path)

	if path == azureTestVM && f.createStatus != 0 {
		code := "InvalidParameter"
		if f.createStatus >= http.StatusInternalServerError {
			f.resources[path], code = body, "InternalServerError"
		}

		armFail(w, f.createStatus, code, "something broke")

		return
	}

	f.resources[path] = body

	if path == azureTestNIC {
		w.Header().Set("Azure-AsyncOperation", f.url+"/operations/nic-1?api-version="+azureNetworkVersion)
	}

	armJSON(w, http.StatusCreated, map[string]any{"id": path, "name": path[strings.LastIndex(path, "/")+1:]})
}

// delete removes a resource; a VM takes its NIC and address with it (deleteOption Delete).
func (f *fakeARM) delete(w http.ResponseWriter, path string) {
	if _, ok := f.resources[path]; !ok {
		w.WriteHeader(http.StatusNoContent)

		return
	}

	f.deleted = append(f.deleted, path)

	if f.slowDelete {
		// Azure keeps the resource while the delete runs.
		w.Header().Set("Location", f.url+"/operations/delete-1?api-version="+azureComputeVersion)
		w.WriteHeader(http.StatusAccepted)

		return
	}

	delete(f.resources, path)

	if path == azureTestVM {
		delete(f.resources, azureTestNIC)
		delete(f.resources, azureTestIP)
	}

	w.Header().Set("Location", f.url+"/operations/delete-1?api-version="+azureComputeVersion)
	w.WriteHeader(http.StatusAccepted)
}
