package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const workloadLogsPodsPath = "/api/v1/namespaces/shop/pods"

// workloadLogsAPI is a Deployment "api" of three pods, each with its own log.
func workloadLogsAPI(t *testing.T, logs map[string]string) *fakeKubeAPI {
	t.Helper()

	var items []string

	for _, pod := range []string{"api-1", "api-2", "api-3"} {
		items = append(items, fmt.Sprintf(`{"metadata":{"namespace":"shop","name":%q},"spec":{"containers":[{"name":"app"},{"name":"sidecar"}]},"status":{"phase":"Running"}}`, pod))
	}

	answers := map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/api": `{"metadata":{"name":"api","namespace":"shop"},"spec":{"selector":{"matchLabels":{"app":"api"}}}}`,
		"GET " + workloadLogsPodsPath:                       `{"metadata":{},"items":[` + strings.Join(items, ",") + `]}`,
	}

	for pod, log := range logs {
		answers["GET "+workloadLogsPodsPath+"/"+pod+"/log"] = log
	}

	return newFakeKubeAPI(t, answers)
}

func readWorkloadLogs(t *testing.T, f *fakeKubeAPI, filter string, regex bool, since, tail int) workloadLogs {
	t.Helper()

	out, err := KubeWorkloadLogs(f.kubeconfigFor(f.URL), "admin@test", "", "Deployment", "shop", "api", "", filter, regex, since, tail)
	if err != nil {
		t.Fatal(err)
	}

	var logs workloadLogs
	if err := json.Unmarshal([]byte(out), &logs); err != nil {
		t.Fatal(err)
	}

	return logs
}

func threePodLogs() map[string]string {
	return map[string]string{
		"api-1": "2026-10-29T10:00:00.000000001Z starting\n2026-10-29T10:00:03Z request timeout\n",
		"api-2": "2026-10-29T10:00:01Z starting\n2026-10-29T10:00:04Z {\"level\":\"error\",\"msg\":\"db Timeout\"}\n",
		"api-3": "2026-10-29T10:00:02Z starting\n2026-10-29T10:00:05Z done\n",
	}
}

func workloadLogTexts(logs workloadLogs) string {
	texts := make([]string, 0, len(logs.Lines))
	for _, l := range logs.Lines {
		texts = append(texts, l.Pod+":"+l.Text)
	}

	return strings.Join(texts, "|")
}

func TestKubeWorkloadLogsMergesByTime(t *testing.T) {
	logs := readWorkloadLogs(t, workloadLogsAPI(t, threePodLogs()), "", false, 0, 0)

	want := `api-1:starting|api-2:starting|api-3:starting|api-1:request timeout|api-2:{"level":"error","msg":"db Timeout"}|api-3:done`
	if got := workloadLogTexts(logs); got != want {
		t.Errorf("merged lines:\n got %s\nwant %s", got, want)
	}

	if logs.Matched != 6 || logs.Scanned != 6 || logs.Truncated {
		t.Errorf("matched %d, scanned %d, truncated %v", logs.Matched, logs.Scanned, logs.Truncated)
	}

	if len(logs.Pods) != 3 || logs.Pods[0].Name != "api-1" || logs.Pods[2].Color != 2 || logs.Pods[1].Lines != 2 {
		t.Errorf("pods = %+v", logs.Pods)
	}

	if logs.Pods[0].Container != "app" {
		t.Errorf("container = %q: a pod of several containers reads its first", logs.Pods[0].Container)
	}

	if logs.Lines[0].Time != time.Date(2026, 10, 29, 10, 0, 0, 1, time.UTC).UnixMilli() {
		t.Errorf("time = %d", logs.Lines[0].Time)
	}

	if logs.Lines[4].Level != "error" {
		t.Errorf("level of the JSON line = %q", logs.Lines[4].Level)
	}
}

func TestKubeWorkloadLogsFilter(t *testing.T) {
	cases := []struct {
		name, filter string
		regex        bool
		want         int
	}{
		{"plain ignores case", "TIMEOUT", false, 2},
		{"plain is literal", "time.*", false, 0},
		{"regex", `request\s+time`, true, 1},
		{"regex is case-sensitive unless asked", "timeout", true, 1},
		{"regex flag", "(?i)timeout", true, 2},
	}

	f := workloadLogsAPI(t, threePodLogs())

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := readWorkloadLogs(t, f, c.filter, c.regex, 0, 0)
			if logs.Matched != c.want || len(logs.Lines) != c.want || logs.Scanned != 6 {
				t.Errorf("matched %d, lines %d, scanned %d, want %d of 6", logs.Matched, len(logs.Lines), logs.Scanned, c.want)
			}
		})
	}
}

func TestKubeWorkloadLogsRefusesABadFilter(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, pattern := range []string{"(unclosed", strings.Repeat("a", logFilterMaxPattern+1)} {
		if _, err := KubeWorkloadLogs(cfg, "", "", "Deployment", "demo", "worker", "", pattern, true, 0, 0); err == nil {
			t.Errorf("pattern of %d bytes accepted", len(pattern))
		}
	}

	if _, err := KubeWorkloadLogs(cfg, "", "", "Job", "demo", "worker", "", "", false, 0, 0); err == nil {
		t.Error("a Job accepted")
	}

	if _, err := KubeWorkloadLogs(cfg, "", "", "Deployment", "demo", "worker", "BAD NAME", "", false, 0, 0); err == nil {
		t.Error("an invalid container accepted")
	}
}

func TestKubeWorkloadLogsAsksTheKubelet(t *testing.T) {
	f := workloadLogsAPI(t, threePodLogs())
	readWorkloadLogs(t, f, "", false, 30, 99999)

	var query url.Values

	for _, r := range f.recorded() {
		if r.path == workloadLogsPodsPath+"/api-1/log" {
			query, _ = url.ParseQuery(r.query)
		}
	}

	if query.Get("timestamps") != "true" || query.Get("sinceSeconds") != "1800" || query.Get("container") != "app" {
		t.Errorf("log query = %v", query)
	}

	if query.Get("tailLines") != fmt.Sprint(workloadLogMaxTail) {
		t.Errorf("tailLines = %q, want the cap", query.Get("tailLines"))
	}

	for _, r := range f.recorded() {
		if r.path == workloadLogsPodsPath {
			list, _ := url.ParseQuery(r.query)
			if list.Get("labelSelector") != "app=api" || list.Get("limit") != fmt.Sprint(workloadLogMaxPods) {
				t.Errorf("pods query = %v", list)
			}
		}
	}
}

func TestKubeWorkloadLogsContinuationKeepsThePreviousTime(t *testing.T) {
	logs := readWorkloadLogs(t, workloadLogsAPI(t, map[string]string{
		"api-1": "2026-10-29T10:00:00Z panic: boom\nno timestamp here\n2026-10-29T10:00:09Z after\n",
		"api-2": "2026-10-29T10:00:05Z middle\n",
		"api-3": "",
	}), "", false, 0, 0)

	want := "api-1:panic: boom|api-1:no timestamp here|api-2:middle|api-1:after"
	if got := workloadLogTexts(logs); got != want {
		t.Errorf("got %s", got)
	}
}

func TestKubeWorkloadLogsOnePodThatFailsLeavesTheOthers(t *testing.T) {
	f := workloadLogsAPI(t, threePodLogs())
	// Still creating its container, or a kubelet that does not answer.
	f.answerWith("GET "+workloadLogsPodsPath+"/api-2/log", http.StatusBadRequest, `{"kind":"Status","message":"container is waiting to start"}`)
	f.answerWith("GET "+workloadLogsPodsPath+"/api-3/log", http.StatusInternalServerError, `{"kind":"Status","message":"kubelet unreachable"}`)

	logs := readWorkloadLogs(t, f, "", false, 0, 0)
	if logs.Scanned != 2 || len(logs.Pods) != 3 || logs.Pods[1].Lines != 0 {
		t.Errorf("scanned %d, pods %+v", logs.Scanned, logs.Pods)
	}
}

func TestKubeWorkloadLogsRefusedIsAnError(t *testing.T) {
	f := workloadLogsAPI(t, threePodLogs())
	for _, pod := range []string{"api-1", "api-2", "api-3"} {
		f.answerWith("GET "+workloadLogsPodsPath+"/"+pod+"/log", http.StatusForbidden, `{"kind":"Status","message":"pods/log is forbidden"}`)
	}

	_, err := KubeWorkloadLogs(f.kubeconfigFor(f.URL), "admin@test", "", "Deployment", "shop", "api", "", "", false, 0, 0)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v, want the refusal", err)
	}
}

func TestKubeWorkloadLogsEveryPodFailingIsAnError(t *testing.T) {
	f := workloadLogsAPI(t, nil)
	for _, pod := range []string{"api-1", "api-2", "api-3"} {
		f.answerWith("GET "+workloadLogsPodsPath+"/"+pod+"/log", http.StatusInternalServerError, `{"kind":"Status","message":"kubelet unreachable"}`)
	}

	if _, err := KubeWorkloadLogs(f.kubeconfigFor(f.URL), "admin@test", "", "Deployment", "shop", "api", "", "", false, 0, 0); err == nil {
		t.Error("nothing read and no error")
	}
}

func TestMergeWorkloadLogsCapsTheAnswerKeepingTheNewest(t *testing.T) {
	var log strings.Builder

	line := strings.Repeat("x", 1024)
	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)

	for i := range 5000 {
		fmt.Fprintf(&log, "%s %d %s\n", start.Add(time.Duration(i)*time.Second).Format(time.RFC3339), i, line)
	}

	logs := mergeWorkloadLogs([]podLogText{{name: "api-1", container: "app", log: log.String()}}, func(string) bool { return true }, 0)

	if !logs.Truncated {
		t.Error("not truncated")
	}

	if logs.Matched != 5000 || logs.Scanned != 5000 {
		t.Errorf("matched %d, scanned %d: the counts are of the whole read", logs.Matched, logs.Scanned)
	}

	size := 0
	for _, l := range logs.Lines {
		size += len(l.Text)
	}

	if size > workloadLogMaxBytes {
		t.Errorf("%d bytes of text", size)
	}

	if last := logs.Lines[len(logs.Lines)-1].Text; !strings.HasPrefix(last, "4999 ") {
		t.Errorf("newest line dropped: %.10s", last)
	}
}

func TestKubeWorkloadLogsReportsMorePodsThanRead(t *testing.T) {
	f := workloadLogsAPI(t, threePodLogs())
	f.answerQuery = func(path, _ string) (string, bool) {
		if path != workloadLogsPodsPath {
			return "", false
		}

		return `{"metadata":{"continue":"more"},"items":[{"metadata":{"namespace":"shop","name":"api-1"},"spec":{"containers":[{"name":"app"}]},"status":{"phase":"Running"}}]}`, true
	}

	if logs := readWorkloadLogs(t, f, "", false, 0, 0); !logs.Truncated {
		t.Error("more pods than read, and not truncated")
	}
}

func TestKubeWorkloadLogsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	read := func(filter string, since int) workloadLogs {
		out, err := KubeWorkloadLogs(cfg, "", "", "Deployment", "demo", "worker", "", filter, false, since, 0)
		if err != nil {
			t.Fatal(err)
		}

		var logs workloadLogs
		if err := json.Unmarshal([]byte(out), &logs); err != nil {
			t.Fatal(err)
		}

		return logs
	}

	all := read("", 0)
	if len(all.Pods) != 2 || all.Scanned == 0 {
		t.Fatalf("demo = %+v", all)
	}

	if filtered := read("timeout", 0); filtered.Matched == 0 || filtered.Matched >= all.Scanned {
		t.Errorf("demo filter matched %d of %d", filtered.Matched, all.Scanned)
	}

	if recent := read("", 15); recent.Scanned == 0 || recent.Scanned >= all.Scanned {
		t.Errorf("demo since 15 min scanned %d of %d", recent.Scanned, all.Scanned)
	}
}

func TestKubePodLogsFiltered(t *testing.T) {
	f := workloadLogsAPI(t, threePodLogs())
	cfg := f.kubeconfigFor(f.URL)

	out, err := KubePodLogsFiltered(cfg, "admin@test", "", "shop", "api-1", "", false, 0, "TIMEOUT", false, 30)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "request timeout") || strings.Contains(out, "starting") {
		t.Errorf("filtered log = %q", out)
	}

	var query url.Values

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/api-1/log") {
			query, _ = url.ParseQuery(r.query)
		}
	}

	if query.Get("sinceSeconds") != "1800" {
		t.Errorf("query = %v", query)
	}

	if _, err := KubePodLogsFiltered(cfg, "admin@test", "", "shop", "api-1", "", false, 0, "(unclosed", true, 0); err == nil {
		t.Error("bad regex accepted")
	}

	// No filter, no bound: the plain read, as KubePodLogs.
	plain, err := KubePodLogsFiltered(cfg, "admin@test", "", "shop", "api-1", "", false, 0, "", false, 0)
	if err != nil || plain != threePodLogs()["api-1"] {
		t.Errorf("plain = %q, %v", plain, err)
	}

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err = KubePodLogsFiltered(demo, "", "", "shop", "web-1", "", true, 0, "panic", false, 0)
	if err != nil || !strings.Contains(out, "panic:") || strings.Contains(out, "starting") {
		t.Errorf("demo filtered = %q, %v", out, err)
	}
}
