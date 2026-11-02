package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// azureLeaks is what no message may carry: the secret, the token, the credential's ids.
func azureLeaks(s string) bool {
	for _, secret := range []string{azureTestSecret, azureTestToken, azureTestTenant, azureTestClientID, azureTestSub} {
		if strings.Contains(strings.ToLower(s), strings.ToLower(secret)) {
			return true
		}
	}

	return false
}

func TestAzureTokenForARM(t *testing.T) {
	cases := []struct {
		environment string
		wantScope   string
	}{
		{"", "https://management.azure.com/.default"},
		{"AzureChinaCloud", "https://management.chinacloudapi.cn/.default"},
		{"AzureUSGovernmentCloud", "https://management.usgovcloudapi.net/.default"},
	}

	for _, c := range cases {
		t.Run(c.wantScope, func(t *testing.T) {
			f := &fakeARM{}
			startFakeARM(t, f)

			cred := strings.Replace(azureTestCred, `"location"`, `"environment":"`+c.environment+`","location"`, 1)
			if _, err := AzureTest(cred); err != nil {
				t.Fatal(err)
			}

			if _, err := AzureOptions("", "", cred); err != nil {
				t.Fatal(err)
			}

			// One token per facade call, for ARM's audience: never an AKS server's.
			if len(f.tokenForms) != 2 {
				t.Fatalf("token requests = %d", len(f.tokenForms))
			}

			for _, form := range f.tokenForms {
				if form.Get("scope") != c.wantScope || form.Get("grant_type") != "client_credentials" || form.Get("client_id") != azureTestClientID {
					t.Errorf("token form = %v", form)
				}
			}

			// The fake refuses a call without the bearer: none was.
			if f.unauthorized != 0 || len(f.calls) < 10 {
				t.Errorf("unauthorized = %d, calls = %v", f.unauthorized, f.calls)
			}
		})
	}
}

func TestAzureTest(t *testing.T) {
	withRG := strings.Replace(azureTestCred, `"location"`, `"resourceGroup":"talos","location"`, 1)

	cases := []struct {
		name     string
		cred     string
		fake     *fakeARM
		want     azureTestResult
		wantCall string
		fail     string
	}{
		{name: "may create", cred: azureTestCred, fake: &fakeARM{}, want: azureTestResult{CanCreate: true, Checked: true},
			wantCall: "GET /subscriptions/" + azureTestSub + "/providers/Microsoft.Authorization/permissions"},
		{name: "on the resource group", cred: withRG, fake: &fakeARM{}, want: azureTestResult{CanCreate: true, Checked: true},
			wantCall: "GET /subscriptions/" + azureTestSub + "/resourceGroups/talos/providers/Microsoft.Authorization/permissions"},
		{name: "reads only", cred: azureTestCred, fake: &fakeARM{noCreateRight: true}, want: azureTestResult{Checked: true}},
		{name: "notActions take the delete back", cred: azureTestCred, fake: &fakeARM{denyDelete: true}, want: azureTestResult{Checked: true}},
		{name: "permissions unreadable: cannot tell", cred: azureTestCred, fake: &fakeARM{noPermissionsRead: true}, want: azureTestResult{}},
		{name: "wrong secret", cred: strings.Replace(azureTestCred, azureTestSecret, "wrong-secret-value", 1), fake: &fakeARM{}, fail: "check the tenant, client id and secret"},
		{name: "no credential", cred: "", fake: &fakeARM{}, fail: "add a service principal first"},
		{name: "a tenant that is not a GUID", cred: strings.Replace(azureTestCred, azureTestTenant, "evil.example.com/x", 1), fake: &fakeARM{}, fail: "tenant id is not a GUID"},
		{name: "an unknown cloud", cred: strings.Replace(azureTestCred, `"location"`, `"environment":"Elsewhere","location"`, 1), fake: &fakeARM{}, fail: "is not an Azure cloud"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeARM(t, c.fake)

			out, err := AzureTest(c.cred)
			if c.fail != "" {
				if err == nil || !strings.Contains(err.Error(), c.fail) || azureLeaks(err.Error()) || strings.Contains(err.Error(), "wrong-secret-value") {
					t.Fatalf("err = %v, want %q in it", err, c.fail)
				}

				return
			}

			got := decodeJSON[azureTestResult](t, out, err)
			if got.Tenant != azureTestTenant || got.Subscription != azureTestSub || got.SubscriptionName != "Scratch" ||
				got.CanCreate != c.want.CanCreate || got.Checked != c.want.Checked {
				t.Errorf("got %+v, want %+v", got, c.want)
			}

			if c.wantCall != "" && !slices.Contains(c.fake.calls, c.wantCall) {
				t.Errorf("calls = %v, want %s", c.fake.calls, c.wantCall)
			}

			if len(c.fake.puts) != 0 || len(c.fake.deleted) != 0 {
				t.Errorf("a test wrote: %v %v", c.fake.putOrder, c.fake.deleted)
			}
		})
	}
}

func TestAzurePermissionMatch(t *testing.T) {
	const write = "Microsoft.Compute/virtualMachines/write"

	cases := []struct {
		name  string
		perms []azurePermission
		want  bool
	}{
		{"owner", []azurePermission{{Actions: []string{"*"}}}, true},
		{"provider wildcard", []azurePermission{{Actions: []string{"Microsoft.Compute/*"}}}, true},
		{"type wildcard, any case", []azurePermission{{Actions: []string{"microsoft.compute/virtualmachines/*"}}}, true},
		{"reads only", []azurePermission{{Actions: []string{"*/read"}}}, false},
		{"another provider", []azurePermission{{Actions: []string{"Microsoft.Network/*"}}}, false},
		{"notActions win in their entry", []azurePermission{{Actions: []string{"*"}, NotActions: []string{"Microsoft.Compute/*/write"}}}, false},
		{"another entry allows it", []azurePermission{{Actions: []string{"*"}, NotActions: []string{write}}, {Actions: []string{write}}}, true},
		{"nothing", nil, false},
	}

	for _, c := range cases {
		if got := azureAllowed(c.perms, write); got != c.want {
			t.Errorf("%s: allowed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAzureOptions(t *testing.T) {
	cases := []struct {
		name         string
		cred         string
		wantWarnings []string
	}{
		{name: "the credential's location", cred: azureTestCred},
		{name: "no location: the first resource group's", cred: strings.Replace(azureTestCred, `,"location":"westeurope"`, "", 1),
			wantWarnings: []string{"no location given: listing westeurope, resource group talos's"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeARM(t, &fakeARM{})

			out, err := AzureOptions(cfg, "fake", c.cred)
			o := decodeJSON[azureOptions](t, out, err)

			var sizes []string
			for _, s := range o.Sizes {
				sizes = append(sizes, s.Name+"/"+s.Arch+"/"+strings.Join(s.Generations, ","))
			}

			// Both pages, the restricted size and the disk SKU dropped, by vCPUs then memory.
			if want := []string{"Standard_A1_v2/amd64/V1", "Standard_B2s/amd64/V1,V2", "Standard_D2ps_v5/arm64/V2"}; !slices.Equal(sizes, want) || o.Sizes[1].MemoryGB != 4 {
				t.Errorf("sizes = %v, want %v", sizes, want)
			}

			if o.Subscription != azureTestSub || o.Location != "westeurope" || len(o.Subscriptions) != 1 || o.Subscriptions[0].Name != "Scratch" ||
				len(o.ResourceGroups) != 2 || len(o.Locations) != 2 || o.Locations[1] != (azureLocation{Name: "westeurope", DisplayName: "West Europe"}) {
				t.Errorf("subscription/location/groups = %+v", o)
			}

			if len(o.Networks) != 1 || o.Networks[0].ResourceGroup != "talos" || len(o.Networks[0].Subnets) != 1 ||
				o.Networks[0].Subnets[0] != (azureSubnet{ID: azureTestSubnet, Name: "nodes", CIDR: "10.0.0.0/24", NSGID: azureTestNSG}) {
				t.Errorf("networks = %+v (the location's only)", o.Networks)
			}

			if len(o.SecurityGroups) != 2 || o.SecurityGroups[0].Name != "closed" || o.SecurityGroups[0].WideOpen ||
				o.SecurityGroups[1].Name != "talos-nodes" || !o.SecurityGroups[1].WideOpen {
				t.Errorf("security groups = %+v", o.SecurityGroups)
			}

			want := []azureImage{
				{ID: azureTestGalImage, Name: "talos/talos-arm64/1.14.2", ResourceGroup: "talos", Source: azureImageGallery, Arch: bootArchARM64,
					Generation: azureGenV2, TalosVersion: "v1.14.2", Schematic: "sample-schematic"},
				{ID: azureTestImage, Name: "talos-by-hand", ResourceGroup: "talos", Source: azureImageManaged, Arch: bootArchAMD64, Generation: azureGenV1},
			}
			if !slices.Equal(o.Images, want) {
				t.Errorf("images = %+v, want %+v (tagged first, no Windows)", o.Images, want)
			}

			if !slices.Equal(o.Warnings, append([]string{}, c.wantWarnings...)) {
				t.Errorf("warnings = %q, want %q", o.Warnings, c.wantWarnings)
			}

			for _, key := range []string{`"memoryGB":4,`, `"generations":["V1","V2"]`, `"wideOpen":true`, `"nsgId":"`, `"source":"gallery"`} {
				if !strings.Contains(out, key) {
					t.Errorf("%s not in %s", key, out)
				}
			}
		})
	}
}

func TestAzureOptionsWarnsWithoutTheVersion(t *testing.T) {
	startFakeARM(t, &fakeARM{})

	c, err := newAzureClient(azureTestCred)
	if err != nil {
		t.Fatal(err)
	}

	o, err := c.options(context.Background(), "v1.15.0")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(o.Warnings, "no image tagged with Talos v1.15.0: pick one of your own or make one") {
		t.Errorf("warnings = %q", o.Warnings)
	}
}

func TestAzurePrice(t *testing.T) {
	f := &fakeARM{}
	startFakeARM(t, f)

	out, err := AzurePrice(azureTestCred, "westeurope", "Standard_B2s")
	if err != nil || out != `{"hourly":"0.0480","currency":"USD"}` {
		t.Errorf("price = %s, %v (the Linux pay-as-you-go item, not Windows, Spot or Low Priority)", out, err)
	}

	if want := "serviceName eq 'Virtual Machines' and armRegionName eq 'westeurope' and armSkuName eq 'Standard_B2s' and priceType eq 'Consumption'"; f.priceFilter != want {
		t.Errorf("filter = %q", f.priceFilter)
	}

	f.noPrices = true

	if out, err := AzurePrice("", "westeurope", "Standard_B2s"); err != nil || out != `{"hourly":""}` {
		t.Errorf("unavailable price = %s, %v", out, err)
	}

	f.priceFilter = ""

	if out, err := AzurePrice(`{"environment":"AzureChinaCloud"}`, "chinanorth3", "Standard_B2s"); err != nil || out != `{"hourly":""}` || f.priceFilter != "" {
		t.Errorf("sovereign price = %s, %v, asked %q", out, err, f.priceFilter)
	}

	if _, err := AzurePrice("", "westeurope", "B2s' or 1 eq 1"); err == nil {
		t.Error("a size that is not one went into the filter")
	}
}

func TestAzureRoleDefinition(t *testing.T) {
	var role azureRole
	if err := json.Unmarshal([]byte(AzureRoleDefinition(azureTestSub, "talos")), &role); err != nil {
		t.Fatal(err)
	}

	if role.Name != "Ichor node creator" || !role.IsCustom || len(role.NotActions) != 0 ||
		!slices.Equal(role.AssignableScopes, []string{"/subscriptions/" + azureTestSub + "/resourceGroups/talos"}) {
		t.Errorf("role = %+v", role)
	}

	for _, want := range append(azureCreateActions(true), "Microsoft.Network/networkSecurityGroups/join/action", "Microsoft.Authorization/permissions/read") {
		if !slices.Contains(role.Actions, want) {
			t.Errorf("%s not in the role", want)
		}
	}

	if placeholders := AzureRoleDefinition("", ""); !strings.Contains(placeholders, `"/subscriptions/SUBSCRIPTION_ID/resourceGroups/RESOURCE_GROUP"`) {
		t.Errorf("placeholders = %s", placeholders)
	}
}

func TestAzureErrorsCarryNoSecret(t *testing.T) {
	c, err := newAzureClient(azureTestCred)
	if err != nil {
		t.Fatal(err)
	}

	c.token = azureTestToken

	got := c.scrub("The client '" + azureTestClientID + "' with object id '11111111-1111-1111-1111-111111111111' may not, in " + strings.ToUpper(azureTestTenant) +
		", subscription /subscriptions/" + azureTestSub + ", secret " + azureTestSecret + ", token " + azureTestToken)
	if azureLeaks(got) || strings.Contains(got, "11111111") {
		t.Errorf("scrubbed = %q", got)
	}

	if _, err := newAzureClient(`{"tenantId":"` + azureTestTenant + `","clientSecret":"` + azureTestSecret); err == nil || strings.Contains(err.Error(), azureTestSecret) {
		t.Errorf("broken JSON: %v", err)
	}

	refusal := &azureError{action: "Microsoft.Compute/virtualMachines/write", code: azureCodeAuthorizationFailed, status: http.StatusForbidden}
	if !strings.Contains(refusal.Error(), "Azure refused Microsoft.Compute/virtualMachines/write: give the service principal the printed role") {
		t.Errorf("refusal = %q", refusal.Error())
	}
}

func azureTestRun(cfg, contextName, role string, rec *joinRecorder) *azureCreate {
	return &azureCreate{
		join: &nodeJoin{
			config: cfg, context: contextName, listener: maskedJoinListener{rec}, interval: joinPollInterval,
			req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "node-c", installDisk: "/dev/sda"},
		},
		spec: azureCreateSpec{
			ResourceGroup: "talos", Location: "westeurope", Size: "Standard_B2s", ImageID: azureTestImage, SubnetID: azureTestSubnet,
			SecurityGroupID: azureTestNSG, AssociatePublicIP: true, Hostname: "node-c", Role: role, TemplateNode: joinTemplateNode, InstallDisk: "/dev/sda",
		},
		credJSON: azureTestCred, poll: joinPollInterval,
	}
}

// azureAt reads a nested value of a decoded body.
func azureAt(v any, keys ...any) any {
	for _, k := range keys {
		switch key := k.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[key]
		case int:
			l, _ := v.([]any)
			if key >= len(l) {
				return nil
			}

			v = l[key]
		}
	}

	return v
}

func TestAzureCreateJoins(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeARM{onRunning: hetznerNodeJoins(t, talos, machine.TypeWorker)}
	startFakeARM(t, f)

	rec := newJoinRecorder()
	azureTestRun(cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	node, msg, events := rec.wait(t)
	if msg != "" || node != joinNewNode {
		t.Fatalf("node %q, error %q, events %+v", node, msg, events)
	}

	want := []string{cloudPhaseCreating, cloudPhaseStarting, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	if !slices.Equal(f.putOrder, []string{azureTestIP, azureTestNIC, azureTestVM}) {
		t.Fatalf("PUTs = %v", f.putOrder)
	}

	vm, nic, ip := f.puts[azureTestVM], f.puts[azureTestNIC], f.puts[azureTestIP]
	tags, _ := vm["tags"].(map[string]any)

	if tags["Name"] != "node-c" || tags[azureTagRun] == nil || tags[azureTagRun] == "" || tags[azureTagCluster] == "" || tags[azureTagCluster] == "fake" ||
		azureAt(nic, "tags", azureTagRun) != tags[azureTagRun] || azureAt(ip, "tags", azureTagRun) != tags[azureTagRun] {
		t.Errorf("tags = %v / %v / %v", tags, nic["tags"], ip["tags"])
	}

	checks := map[string][2]any{
		"vmSize":         {azureAt(vm, "properties", "hardwareProfile", "vmSize"), "Standard_B2s"},
		"image":          {azureAt(vm, "properties", "storageProfile", "imageReference", "id"), azureTestImage},
		"disk delete":    {azureAt(vm, "properties", "storageProfile", "osDisk", "deleteOption"), "Delete"},
		"disk size":      {azureAt(vm, "properties", "storageProfile", "osDisk", "diskSizeGB"), float64(azureDefaultDiskGB)},
		"computer name":  {azureAt(vm, "properties", "osProfile", "computerName"), "node-c"},
		"no password":    {azureAt(vm, "properties", "osProfile", "linuxConfiguration", "disablePasswordAuthentication"), true},
		"nic":            {azureAt(vm, "properties", "networkProfile", "networkInterfaces", 0, "id"), azureTestNIC},
		"nic delete":     {azureAt(vm, "properties", "networkProfile", "networkInterfaces", 0, "properties", "deleteOption"), "Delete"},
		"location":       {vm["location"], "westeurope"},
		"subnet":         {azureAt(nic, "properties", "ipConfigurations", 0, "properties", "subnet", "id"), azureTestSubnet},
		"nsg":            {azureAt(nic, "properties", "networkSecurityGroup", "id"), azureTestNSG},
		"public address": {azureAt(nic, "properties", "ipConfigurations", 0, "properties", "publicIPAddress", "id"), azureTestIP},
		"address delete": {azureAt(nic, "properties", "ipConfigurations", 0, "properties", "publicIPAddress", "properties", "deleteOption"), "Delete"},
		"ip sku":         {azureAt(ip, "sku", "name"), "Standard"},
	}

	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %v, want %v", name, c[0], c[1])
		}
	}

	customData, _ := azureAt(vm, "properties", "osProfile", "customData").(string)
	if data, err := base64.StdEncoding.DecodeString(customData); err != nil || !strings.Contains(string(data), "node-c") {
		t.Errorf("custom data = %q, %v", data, err)
	}

	if key, _ := azureAt(vm, "properties", "osProfile", "linuxConfiguration", "ssh", "publicKeys", 0, "keyData").(string); !strings.HasPrefix(key, "ssh-ed25519 ") {
		t.Errorf("ssh key = %q", key)
	}

	if len(f.deleted) != 0 || len(talos.applies) != 0 {
		t.Errorf("deleted = %v, applies = %+v after a success", f.deleted, talos.applies)
	}

	// The printed role holds every call the run made.
	var role azureRole
	if err := json.Unmarshal([]byte(AzureRoleDefinition(azureTestSub, "talos")), &role); err != nil {
		t.Fatal(err)
	}

	for _, call := range f.calls {
		method, path, _ := strings.Cut(call, " ")
		if action := azureActionOf(method, path); !slices.Contains(role.Actions, action) {
			t.Errorf("%s needs %s, not in the role", call, action)
		}
	}

	for _, e := range events {
		if azureLeaks(e.Message) {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", "azure-vm-create")
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "vm=/subscriptions/…/resourceGroups/talos/providers/Microsoft.Compute/virtualMachines/node-c") ||
		!strings.Contains(entries[0].Params, "size=Standard_B2s location=westeurope") || azureLeaks(entries[0].Params) || strings.Contains(entries[0].Params, "v1alpha1") {
		t.Errorf("audit = %+v", entries)
	}
}

// On Omni the run hands over to the Omni watch once the VM runs: the machine appears by its host
// name (see TestAWSCreateWaitsForOmni for why the whole Omni run is not played).
func TestAzureCreateWaitsForOmni(t *testing.T) {
	rec := newJoinRecorder()
	h := azureTestRun("", "", "", rec)

	calls := 0
	h.omniHostnames = func(context.Context, *session) ([]string, error) {
		calls++
		if calls < 3 {
			return []string{"other"}, nil
		}

		return []string{"other", "NODE-C"}, nil
	}

	run := &cloudCreateRun{provider: h, join: h.join, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := run.waitOmni(ctx, nil); err != nil || calls != 3 {
		t.Errorf("waitOmni = %v after %d calls", err, calls)
	}
}

// azureBigEdits makes the custom data larger than Azure takes.
var azureBigEdits = `[{"doc":0,"path":["machine","sysctls","net.ichor.padding"],"op":"put","type":"string","value":"` + strings.Repeat("x", azureUserDataLimit) + `"}]`

func TestAzureCreateFailures(t *testing.T) {
	cases := []struct {
		name        string
		fake        *fakeARM
		mutate      func(*azureCreate)
		wantInError string
		wantPuts    int
		wantDeleted []string
	}{
		{name: "reads only: refused before any write", fake: &fakeARM{noCreateRight: true}, wantInError: "may not Microsoft.Network/networkInterfaces/write in resource group talos: paste the printed role"},
		{name: "notActions take the delete back: refused before any write", fake: &fakeARM{denyDelete: true}, wantInError: "may not Microsoft.Compute/virtualMachines/delete"},
		{name: "permissions unreadable: refused before any write", fake: &fakeARM{noPermissionsRead: true}, wantInError: "may not read its own permissions"},
		{
			name: "a V2 image on a V1 size: refused before any write", fake: &fakeARM{imageGeneration: azureGenV2},
			mutate: func(h *azureCreate) { h.spec.Size = "Standard_A1_v2" }, wantInError: "this image is V2, the size runs V1 only: pick another",
		},
		{
			name: "an arm64 image on an amd64 size: refused before any write", fake: &fakeARM{},
			mutate: func(h *azureCreate) { h.spec.ImageID = azureTestGalImage }, wantInError: "this image is arm64, the size is amd64",
		},
		{
			name: "a security group of another location: refused before any write", fake: &fakeARM{},
			mutate: func(h *azureCreate) { h.spec.SecurityGroupID = azureTestNSGOther }, wantInError: "is in northeurope, the VM in westeurope",
		},
		{name: "a VM of that name: refused before any write", fake: &fakeARM{existingVM: true}, wantInError: "a VM named node-c exists in talos"},
		{
			name: "custom data over 48 KiB: refused before any write", fake: &fakeARM{},
			mutate: func(h *azureCreate) { h.spec.ExtraEdits = azureBigEdits }, wantInError: "over the 48 KiB Azure takes",
		},
		{
			name: "a refused VM create: the NIC and the address made before it are deleted by name", fake: &fakeARM{createStatus: http.StatusBadRequest},
			wantInError: "Network interface /subscriptions/…/resourceGroups/talos/providers/Microsoft.Network/networkInterfaces/node-c-nic was deleted",
			wantPuts:    3, wantDeleted: []string{azureTestNIC, azureTestIP},
		},
		{
			name: "a VM create without a clear answer: found by name and tag, and deleted with its NIC", fake: &fakeARM{createStatus: http.StatusInternalServerError},
			wantInError: "VM /subscriptions/…/resourceGroups/talos/providers/Microsoft.Compute/virtualMachines/node-c was deleted",
			wantPuts:    3, wantDeleted: []string{azureTestVM},
		},
		{
			name: "the VM fails to provision: an error, and it is deleted", fake: &fakeARM{endState: "Failed"},
			wantInError: "VM node-c failed: OS Provisioning for VM 'node-c' did not finish", wantPuts: 3, wantDeleted: []string{azureTestVM},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeARM(t, c.fake)

			rec := newJoinRecorder()

			h := azureTestRun(cfg, "fake", joinRoleWorker, rec)
			if c.mutate != nil {
				c.mutate(h)
			}

			h.start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.wantInError) || azureLeaks(msg) {
				t.Errorf("error = %q, want %q in it", msg, c.wantInError)
			}

			if len(c.fake.putOrder) != c.wantPuts {
				t.Errorf("PUTs = %v, want %d", c.fake.putOrder, c.wantPuts)
			}

			if !slices.Equal(c.fake.deleted, c.wantDeleted) {
				t.Errorf("deleted = %v, want %v", c.fake.deleted, c.wantDeleted)
			}

			if _, left := c.fake.resources[azureTestNIC]; left && c.wantPuts > 0 {
				t.Error("the NIC is left behind")
			}

			entries := readAudit(t, "fake", "azure-vm-create")
			if len(entries) != 1 || entries[0].Outcome == auditOK || azureLeaks(entries[0].Params) {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestAzureCreateStopDeletes(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	started := make(chan struct{})
	f := &fakeARM{onRunning: func() { close(started) }}
	startFakeARM(t, f)

	rec := newJoinRecorder()
	run := azureTestRun(cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the VM was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "VM /subscriptions/…/resourceGroups/talos/providers/Microsoft.Compute/virtualMachines/node-c was deleted") ||
		!slices.Equal(f.deleted, []string{azureTestVM}) || len(f.resources) != 0 {
		t.Errorf("error = %q, deleted = %v, left = %v", msg, f.deleted, f.resources)
	}
}

// A delete Azure accepted but did not finish is reported as unconfirmed, never as done: the VM
// may still be billed.
func TestAzureCreateStopUnfinishedDelete(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	before := azureDeleteWait
	azureDeleteWait = 300 * time.Millisecond

	t.Cleanup(func() { azureDeleteWait = before })

	started := make(chan struct{})
	f := &fakeARM{slowDelete: true, onRunning: func() { close(started) }}
	startFakeARM(t, f)

	rec := newJoinRecorder()
	run := azureTestRun(cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the VM was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "virtualMachines/node-c could not be deleted") || !strings.Contains(msg, "still billed") ||
		strings.Contains(msg, "was deleted") || len(f.deleted) != 1 {
		t.Errorf("error = %q, deleted = %v", msg, f.deleted)
	}
}

func TestAzureWaitRunningPrivateAddress(t *testing.T) {
	f := &fakeARM{}
	startFakeARM(t, f)

	f.resources[azureTestVM] = map[string]any{}
	f.resources[azureTestNIC] = map[string]any{}

	rec := newJoinRecorder()
	h := azureTestRun("", "fake", joinRoleWorker, rec)
	h.spec.AssociatePublicIP, h.vmID = false, azureTestVM

	client, err := newAzureClient(azureTestCred)
	if err != nil {
		t.Fatal(err)
	}

	h.client = client

	if err := h.waitRunning(context.Background()); err != nil || h.join.node != "10.0.0.20" {
		t.Fatalf("waitRunning = %v, node %q", err, h.join.node)
	}

	rec.OnDone("", "")

	_, _, events := rec.wait(t)
	if last := events[len(events)-1].Message; !strings.Contains(last, "private address 10.0.0.20") || !strings.Contains(last, "route") {
		t.Errorf("progress = %q", last)
	}
}

func TestAzureDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := AzureOptions(cfg, "", "")
	if o := decodeJSON[azureOptions](t, out, err); len(o.Sizes) == 0 || len(o.Networks) == 0 || len(o.Images) != 1 || o.Images[0].TalosVersion != demoTalosVersion ||
		!strings.HasPrefix(o.Images[0].ID, "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/talos/providers/") {
		t.Errorf("demo options = %+v", o)
	}

	rec := newJoinRecorder()
	StartAzureCreate(cfg, "", azureTestCred, `{"resourceGroup":"talos","location":"westeurope","size":"Standard_B2s","imageId":"`+azureTestImage+
		`","subnetId":"`+azureTestSubnet+`","hostname":"node-c"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo create = %q", msg)
	}
}
