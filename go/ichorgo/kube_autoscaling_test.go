package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	apisAllAutoscalers = `{"groups":[` +
		`{"name":"autoscaling","preferredVersion":{"version":"v2"}},` +
		`{"name":"keda.sh","preferredVersion":{"version":"v1alpha1"}},` +
		`{"name":"autoscaling.k8s.io","preferredVersion":{"version":"v1"}},` +
		`{"name":"karpenter.sh","preferredVersion":{"version":"v1"}}]}`
	forbiddenStatus = `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`
)

func autoscalingFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "autoscaling/"+name))
}

// autoscalingAnswers is a cluster running all four autoscalers.
func autoscalingAnswers(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"GET /apis": apisAllAutoscalers,
		"GET /apis/autoscaling/v2/horizontalpodautoscalers":         autoscalingFixture(t, "hpas.json"),
		"GET /apis/keda.sh/v1alpha1/scaledobjects":                  autoscalingFixture(t, "scaledobjects.json"),
		"GET /apis/keda.sh/v1alpha1/scaledjobs":                     autoscalingFixture(t, "scaledjobs.json"),
		"GET /apis/autoscaling.k8s.io/v1/verticalpodautoscalers":    autoscalingFixture(t, "vpas.json"),
		"GET /apis/apps/v1/namespaces/billing/deployments/invoices": autoscalingFixture(t, "deployment-invoices.json"),
		"GET /apis/karpenter.sh/v1/nodepools":                       autoscalingFixture(t, "nodepools.json"),
		"GET /apis/karpenter.sh/v1/nodeclaims":                      autoscalingFixture(t, "nodeclaims.json"),
	}
}

// autoscalingReadFrom is readAutoscaling over the fake API f.
func autoscalingReadFrom(t *testing.T, f *fakeKubeAPI) autoscalingReport {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readAutoscaling(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func TestAutoscalingHPAs(t *testing.T) {
	r := autoscalingReadFrom(t, newFakeKubeAPI(t, autoscalingAnswers(t)))

	if !r.HPA.Installed || r.HPA.Version != "v2" || r.HPA.Error != "" || len(r.HPA.Items) != 4 {
		t.Fatalf("hpa group %+v", r.HPA.autoscalingGroupState)
	}

	// Problems first: not ready, at max, then the healthy ones by namespace.
	var names []string
	for _, h := range r.HPA.Items {
		names = append(names, h.Namespace+"/"+h.Name)
	}
	if !slices.Equal(names, []string{"billing/invoices", "shop/web", "media/keda-hpa-transcoder", "shop/api"}) {
		t.Fatalf("order %v", names)
	}

	web := r.HPA.Items[1]
	if !web.AtMax || !web.Conditions.ScalingLimited || web.State != autoscaleAtMax || web.Min != 2 || web.Max != 6 ||
		web.LastScaleAt != time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("web %+v", web)
	}
	if want := []hpaMetric{{Type: "Resource", Name: "cpu", Current: "94%", Target: "70%"}}; !slices.Equal(web.Metrics, want) {
		t.Errorf("web metrics %+v", web.Metrics)
	}

	// The metrics pipeline is down: the message stays, current reads "-", min defaults to 1.
	invoices := r.HPA.Items[0]
	if invoices.State != autoscaleNotReady || invoices.Conditions.ScalingActive || invoices.Min != 1 ||
		!strings.Contains(invoices.Conditions.Message, "unable to get metrics") || invoices.Metrics[0].Current != "-" {
		t.Errorf("invoices %+v", invoices)
	}

	// KEDA's HPA is linked to its ScaledObject by owner.
	if keda := r.HPA.Items[2]; keda.ManagedBy != autoscaledByKEDA || keda.ScaledObject != "transcoder" {
		t.Errorf("keda hpa %+v", keda)
	}

	api := r.HPA.Items[3]
	if want := []hpaMetric{
		{Type: "Pods", Name: "http_requests_per_second", Current: "62", Target: "100"},
		{Type: "ContainerResource", Name: "memory of app", Current: "301Mi", Target: "512Mi"},
	}; !slices.Equal(api.Metrics, want) || api.State != autoscaleOK {
		t.Errorf("api %+v", api)
	}
}

func TestAutoscalingKEDA(t *testing.T) {
	r := autoscalingReadFrom(t, newFakeKubeAPI(t, autoscalingAnswers(t)))

	if !r.KEDA.Installed || r.KEDA.Version != "v1alpha1" || r.KEDA.Error != "" || len(r.KEDA.ScaledObjects) != 3 {
		t.Fatalf("keda group %+v, %d objects", r.KEDA.autoscalingGroupState, len(r.KEDA.ScaledObjects))
	}

	byName := map[string]scaledObjectRow{}
	for _, s := range r.KEDA.ScaledObjects {
		byName[s.Name] = s
	}

	// Paused at a count; its fallback and a kafka topic, never the password next to it.
	up := byName["uploader"]
	if !up.Paused || up.PausedReplicas == nil || *up.PausedReplicas != 5 || up.State != autoscalePaused ||
		up.FallbackReplicas != 2 || up.Min != 0 || up.Max != 20 || up.Triggers[0] != (kedaTrigger{Type: "kafka", Summary: "uploads"}) {
		t.Errorf("uploader %+v", up)
	}

	// The HPA it owns gives current and desired; hpaName points at it.
	tr := byName["transcoder"]
	if tr.HPAName != "keda-hpa-transcoder" || tr.Current != 4 || tr.Desired != 5 || !tr.Ready || !tr.Active ||
		tr.State != autoscaleOK || tr.Target != (autoscaleTarget{Kind: "Deployment", Name: "transcoder"}) ||
		tr.Triggers[0].Summary != "sum(rate(jobs_queued_total[2m]))" {
		t.Errorf("transcoder %+v", tr)
	}

	// Defaults (min 0, max 100, kind Deployment), a cron window, and the reason it is not ready.
	oh := byName["office-hours"]
	if oh.State != autoscaleNotReady || oh.Max != kedaDefaultMax || oh.Target.Kind != "Deployment" ||
		oh.Triggers[0].Summary != "0 8 * * 1-5 → 0 19 * * 1-5 Europe/Berlin" || !strings.Contains(oh.Message, "scaleTargetRef") {
		t.Errorf("office-hours %+v", oh)
	}

	if r.KEDA.ScaledObjects[0].Name != "office-hours" {
		t.Errorf("not ready first, got %s", r.KEDA.ScaledObjects[0].Name)
	}

	if len(r.KEDA.ScaledJobs) != 1 || r.KEDA.ScaledJobs[0].Triggers[0].Summary != "thumbnails" || r.KEDA.ScaledJobs[0].MaxReplicaCount != 20 {
		t.Errorf("scaled jobs %+v", r.KEDA.ScaledJobs)
	}

	out, err := toJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "never-shown") {
		t.Error("a trigger secret reached the report")
	}
}

func TestAutoscalingVPA(t *testing.T) {
	r := autoscalingReadFrom(t, newFakeKubeAPI(t, autoscalingAnswers(t)))

	if !r.VPA.Installed || r.VPA.Error != "" || len(r.VPA.Items) != 2 {
		t.Fatalf("vpa group %+v", r.VPA)
	}

	// No recommendation yet comes first, with its reason and no containers (not zeros).
	vault := r.VPA.Items[0]
	if vault.Name != "vault" || vault.RecommendationProvided || vault.State != autoscaleNotReady ||
		vault.Message != "No pods match this VPA object" || len(vault.Containers) != 0 || vault.UpdateMode != "Auto" {
		t.Errorf("vault %+v", vault)
	}

	inv := r.VPA.Items[1]
	if !inv.RecommendationProvided || inv.UpdateMode != "Off" || len(inv.Containers) != 2 {
		t.Fatalf("invoices %+v", inv)
	}

	want := vpaContainer{
		Name: "app", CPUTarget: "250m", MemoryTarget: "384Mi", CPULower: "120m", CPUUpper: "900m",
		MemoryLower: "256Mi", MemoryUpper: "1Gi", CPURequest: "100m", MemoryRequest: "256Mi",
	}
	if inv.Containers[0] != want {
		t.Errorf("app %+v", inv.Containers[0])
	}

	// A container the template does not have (an injected sidecar) has no live requests.
	if side := inv.Containers[1]; side.CPURequest != "" || side.CPUTarget != "15m" {
		t.Errorf("sidecar %+v", side)
	}
}

func TestAutoscalingKarpenter(t *testing.T) {
	r := autoscalingReadFrom(t, newFakeKubeAPI(t, autoscalingAnswers(t)))

	if !r.Karpenter.Installed || r.Karpenter.Version != "v1" || r.Karpenter.Error != "" {
		t.Fatalf("karpenter %+v", r.Karpenter.autoscalingGroupState)
	}

	if len(r.Karpenter.NodePools) != 2 || r.Karpenter.NodePools[0].Name != "gpu" {
		t.Fatalf("pools %+v", r.Karpenter.NodePools)
	}

	gpu, general := r.Karpenter.NodePools[0], r.Karpenter.NodePools[1]
	if gpu.Ready || gpu.Message != "NodeClassReady=False" || gpu.AtLimit || gpu.Resources.Nodes != 1 {
		t.Errorf("gpu %+v", gpu)
	}

	if !general.AtLimit || !general.Ready || general.Resources.Nodes != 2 || general.Weight != 10 ||
		!slices.Equal(general.CapacityTypes, []string{"spot", "on-demand"}) || len(general.Disruption.Budgets) != 2 ||
		general.Disruption.Budgets[1].Schedule != "0 9 * * mon-fri" || general.Disruption.ConsolidateAfter != "1m" {
		t.Errorf("general %+v", general)
	}

	// The claim without a node for long is pending and first; the drifted one next.
	claims := r.Karpenter.NodeClaims
	if len(claims) != 3 || claims[0].Name != "general-x7k2p" || claims[1].Name != "general-4mq9d" {
		t.Fatalf("claims %+v", claims)
	}

	if p := claims[0]; !p.Pending || !p.Launched || p.Registered || p.Message != "Node not registered with cluster" ||
		p.InstanceType != "m7g.2xlarge" || p.CapacityType != "spot" || p.Pool != "general" {
		t.Errorf("pending %+v", p)
	}

	if d := claims[1]; !d.Drifted || !d.Ready || d.Pending || d.NodeName != "node-10-0-12-7" {
		t.Errorf("drifted %+v", d)
	}
}

func TestAutoscalingWorkloads(t *testing.T) {
	r := autoscalingReadFrom(t, newFakeKubeAPI(t, autoscalingAnswers(t)))

	var got []string
	for _, w := range r.Workloads {
		got = append(got, w.By+":"+w.Namespace+"/"+w.Kind+"/"+w.Name+"="+w.State)
	}

	// One entry per (workload, autoscaler kind); KEDA's own HPA is not a second entry.
	want := []string{
		"hpa:billing/Deployment/invoices=notReady",
		"keda:shop/Deployment/backoffice=notReady",
		"vpa:vault/StatefulSet/vault=notReady",
		"hpa:shop/Deployment/web=atMax",
		"keda:media/Deployment/uploader=paused",
		"vpa:billing/Deployment/invoices=ok",
		"keda:media/Deployment/transcoder=ok",
		"hpa:shop/StatefulSet/api=ok",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("workloads\n got %v\nwant %v", got, want)
	}

	if web := r.Workloads[3]; web.Autoscaler != "web" || web.Min != 2 || web.Max != 6 || web.Current != 6 {
		t.Errorf("web %+v", web)
	}
}

// TestAutoscalingOnlyHPA: a cluster without KEDA, VPA or Karpenter answers installed false
// for the three, no error, and still lists its HPAs.
func TestAutoscalingOnlyHPA(t *testing.T) {
	r := autoscalingReadFrom(t, newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"autoscaling","preferredVersion":{"version":"v2"}}]}`,
		"GET /apis/autoscaling/v2/horizontalpodautoscalers": autoscalingFixture(t, "hpas.json"),
	}))

	if len(r.HPA.Items) != 4 || r.HPA.Error != "" {
		t.Fatalf("hpa %+v", r.HPA.autoscalingGroupState)
	}

	for name, g := range map[string]autoscalingGroupState{
		"keda": r.KEDA.autoscalingGroupState, "vpa": r.VPA.autoscalingGroupState, "karpenter": r.Karpenter.autoscalingGroupState,
	} {
		if g.Installed || g.Error != "" || g.Version != "" {
			t.Errorf("%s %+v", name, g)
		}
	}

	out, err := toJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, empty := range []string{`"scaledObjects":[]`, `"nodeClaims":[]`, `"items":[]`} {
		if !strings.Contains(out, empty) {
			t.Errorf("%s missing from %s", empty, out)
		}
	}
}

// TestAutoscalingNodeClaimsRefused keeps the pools and every other group when the claims
// are off limits: the 403 is Karpenter's error, the call does not fail.
func TestAutoscalingNodeClaimsRefused(t *testing.T) {
	f := newFakeKubeAPI(t, autoscalingAnswers(t))
	f.answerWith("GET /apis/karpenter.sh/v1/nodeclaims", http.StatusForbidden, forbiddenStatus)

	r := autoscalingReadFrom(t, f)

	if r.Karpenter.Error == "" || len(r.Karpenter.NodePools) != 2 || len(r.Karpenter.NodeClaims) != 0 {
		t.Fatalf("karpenter %+v", r.Karpenter)
	}

	if r.HPA.Error != "" || r.KEDA.Error != "" || r.VPA.Error != "" || len(r.HPA.Items) != 4 || len(r.VPA.Items) != 2 {
		t.Errorf("other groups hurt: hpa %q keda %q vpa %q", r.HPA.Error, r.KEDA.Error, r.VPA.Error)
	}
}

// TestAutoscalingKarpenterVersions reads v1beta1 through the same structs and refuses a
// version without NodePools as an error, not a failed call.
func TestAutoscalingKarpenterVersions(t *testing.T) {
	beta := autoscalingReadFrom(t, newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"karpenter.sh","preferredVersion":{"version":"v1beta1"}}]}`,
		"GET /apis/autoscaling/v2/horizontalpodautoscalers": `{"items":[]}`,
		"GET /apis/karpenter.sh/v1beta1/nodepools":          autoscalingFixture(t, "nodepools.json"),
		"GET /apis/karpenter.sh/v1beta1/nodeclaims":         autoscalingFixture(t, "nodeclaims.json"),
	}))
	if beta.Karpenter.Version != "v1beta1" || beta.Karpenter.Error != "" || len(beta.Karpenter.NodePools) != 2 {
		t.Errorf("v1beta1 %+v", beta.Karpenter)
	}

	old := autoscalingReadFrom(t, newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"karpenter.sh","preferredVersion":{"version":"v1alpha5"}}]}`,
		"GET /apis/autoscaling/v2/horizontalpodautoscalers": `{"items":[]}`,
	}))
	if !old.Karpenter.Installed || !strings.Contains(old.Karpenter.Error, "v1alpha5") {
		t.Errorf("v1alpha5 %+v", old.Karpenter.autoscalingGroupState)
	}
}

func TestAutoscalingClip(t *testing.T) {
	rows := make([]hpaRow, autoscalingRowsMax+3)

	kept, cut := clipRows(rows)
	if len(kept) != autoscalingRowsMax || !cut {
		t.Errorf("kept %d cut %v", len(kept), cut)
	}

	if kept, cut := clipRows(rows[:5]); len(kept) != 5 || cut {
		t.Errorf("small list clipped: %d %v", len(kept), cut)
	}
}

func TestAutoscalingDemo(t *testing.T) {
	out, err := toJSON(demoAutoscaling())
	if err != nil {
		t.Fatal(err)
	}

	var r autoscalingReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	if len(r.HPA.Items) == 0 || len(r.KEDA.ScaledObjects) == 0 || len(r.KEDA.ScaledJobs) == 0 || len(r.VPA.Items) == 0 ||
		len(r.Karpenter.NodePools) == 0 || len(r.Karpenter.NodeClaims) == 0 || len(r.Workloads) == 0 {
		t.Fatalf("a group is empty: %s", out)
	}

	states := map[string]bool{}
	for _, w := range r.Workloads {
		states[w.State] = true
	}
	for _, want := range []string{autoscaleNotReady, autoscaleAtMax, autoscaleLimited, autoscaleFallback, autoscalePaused, autoscaleOK} {
		if !states[want] {
			t.Errorf("demo has no %s workload", want)
		}
	}

	if !r.Karpenter.NodePools[0].AtLimit || !r.Karpenter.NodeClaims[0].Pending {
		t.Errorf("demo pool at limit / pending claim not first: %+v %+v", r.Karpenter.NodePools[0], r.Karpenter.NodeClaims[0])
	}

	// The demo is answered without a cluster.
	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	got, err := KubeAutoscaling(demo, demoKubeContext, "")
	if err != nil || !strings.Contains(got, `"workloads"`) {
		t.Errorf("demo read %q, %v", got, err)
	}
}
