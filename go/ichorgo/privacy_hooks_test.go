package ichorgo

import (
	"strings"
	"testing"
)

// Each fake embeds its listener interface (nil) and records only what the masked wrappers
// forward; rec is shared so one assertion covers every listener.
type rec struct{ got []string }

func (r *rec) add(s ...string) { r.got = append(r.got, s...) }

type (
	recNetPerf struct {
		NetPerfListener
		*rec
	}
	recHubble struct {
		HubbleListener
		*rec
	}
	recEvent struct {
		EventListener
		*rec
	}
	recLog struct {
		LogListener
		*rec
	}
	recHealth struct {
		HealthListener
		*rec
	}
	recDiagnosis struct {
		DiagnosisListener
		*rec
	}
	recSnapshot struct {
		SnapshotListener
		*rec
	}
	recDebug struct {
		DebugListener
		*rec
	}
	recMaintenance struct {
		MaintenanceListener
		*rec
	}
	recCapture struct {
		CaptureListener
		*rec
	}
	recSupport struct {
		SupportListener
		*rec
	}
	recUpgrade struct {
		UpgradeListener
		*rec
	}
)

func (f recNetPerf) OnProgress(j string)                    { f.add(j) }
func (f recNetPerf) OnDone(report, e string)                { f.add(report, e) }
func (f recHubble) OnUpdate(j string)                       { f.add(j) }
func (f recHubble) OnDone(e string)                         { f.add(e) }
func (f recEvent) OnEvent(j string)                         { f.add(j) }
func (f recEvent) OnDone(e string)                          { f.add(e) }
func (f recLog) OnLine(l string)                            { f.add(l) }
func (f recLog) OnDone(e string)                            { f.add(e) }
func (f recHealth) OnProgress(node, m string)               { f.add(node, m) }
func (f recHealth) OnDone(e string)                         { f.add(e) }
func (f recDiagnosis) OnAnswer(t string)                    { f.add(t) }
func (f recDiagnosis) OnDone(e string)                      { f.add(e) }
func (f recSnapshot) OnDone(_ string, _ int64, _, e string) { f.add(e) }
func (f recDebug) OnStatus(m string)                        { f.add(m) }
func (f recDebug) OnExit(_ int, e string)                   { f.add(e) }
func (f recMaintenance) OnProgress(j string)                { f.add(j) }
func (f recMaintenance) OnDone(e string)                    { f.add(e) }
func (f recCapture) OnPacket(j string)                      { f.add(j) }
func (f recCapture) OnDone(_ string, _, _ int64, e string)  { f.add(e) }
func (f recSupport) OnProgress(j string)                    { f.add(j) }
func (f recSupport) OnDone(_ string, _ int64, e string)     { f.add(e) }
func (f recUpgrade) OnProgress(j string)                    { f.add(j) }
func (f recUpgrade) OnDone(version, e string)               { f.add(version, e) }

// Whatever a run reports back to the app goes through the screenshot mask: a learnt
// hostname never reaches a listener, in a JSON payload or in an error message.
func TestMaskedListenersHideLearntNames(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })
	privacy.learnHost("talos-cp1", "controlplane")

	const (
		host = "talos-cp1"
		js   = `{"node":"talos-cp1","message":"talos-cp1 is draining"}`
		text = "talos-cp1: connection refused"
	)

	r := &rec{}
	want := 0
	call := func(n int, f func()) {
		want += n
		f()
	}

	call(3, func() {
		l := maskedNetPerfListener{recNetPerf{rec: r}}
		l.OnProgress(js)
		l.OnDone(js, text)
	})
	call(2, func() { l := maskedHubbleListener{recHubble{rec: r}}; l.OnUpdate(js); l.OnDone(text) })
	call(2, func() { l := maskedEventListener{recEvent{rec: r}}; l.OnEvent(js); l.OnDone(text) })
	call(2, func() { l := maskedLogListener{recLog{rec: r}}; l.OnLine(text); l.OnDone(text) })
	call(3, func() { l := maskedHealthListener{recHealth{rec: r}}; l.OnProgress(host, text); l.OnDone(text) })
	call(2, func() { l := maskedDiagnosisListener{recDiagnosis{rec: r}}; l.OnAnswer(text); l.OnDone(text) })
	call(1, func() { maskedSnapshotListener{recSnapshot{rec: r}}.OnDone("/data/snap.db", 1, "abc", text) })
	call(2, func() { l := maskedDebugListener{recDebug{rec: r}}; l.OnStatus(text); l.OnExit(1, text) })
	call(2, func() { l := maskedMaintenanceListener{recMaintenance{rec: r}}; l.OnProgress(js); l.OnDone(text) })
	call(2, func() {
		l := maskedCaptureListener{recCapture{rec: r}}
		l.OnPacket(js)
		l.OnDone("/data/c.pcap", 1, 2, text)
	})
	call(2, func() {
		l := maskedSupportListener{recSupport{rec: r}}
		l.OnProgress(js)
		l.OnDone("/data/b.zip", 1, text)
	})
	call(3, func() { l := maskedUpgradeListener{recUpgrade{rec: r}}; l.OnProgress(js); l.OnDone("v1.2.3", text) })

	if len(r.got) != want {
		t.Fatalf("forwarded %d values, want %d: %q", len(r.got), want, r.got)
	}

	for i, s := range r.got {
		if strings.Contains(s, host) {
			t.Errorf("value %d leaks the hostname: %q", i, s)
		}
	}

	// The mask rewrites names, it does not drop what was said.
	if !strings.Contains(strings.Join(r.got, "\n"), "connection refused") || !strings.Contains(strings.Join(r.got, "\n"), "v1.2.3") {
		t.Errorf("the messages lost their content: %q", r.got)
	}
}

// With the mask off, the wrappers are transparent.
func TestMaskedListenersPassThroughWhenOff(t *testing.T) {
	SetPrivacyMask(false, "")

	r := &rec{}
	l := maskedHealthListener{recHealth{rec: r}}
	l.OnProgress("talos-cp1", "ready")
	l.OnDone("talos-cp1: timeout")

	if got := strings.Join(r.got, "|"); got != "talos-cp1|ready|talos-cp1: timeout" {
		t.Fatalf("got %q", got)
	}
}
