package ichorgo

import (
	"errors"
	"net/http"
	"slices"
	"testing"
)

func TestClampSnapLen(t *testing.T) {
	// 0 means whole packets: an unset, negative or oversized length all ask for that.
	for in, want := range map[int]int{0: 0, -5: 0, maxSnapLen: 0, maxSnapLen + 1: 0, 1: 64, 64: 64, 1500: 1500, maxSnapLen - 1: maxSnapLen - 1} {
		if got := clampSnapLen(in); got != want {
			t.Errorf("clampSnapLen(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDrainMessage(t *testing.T) {
	pods := []drainPod{{Name: "a", State: podGone}, {Name: "b", State: podBlocked}, {Name: "c"}, {Name: "d", State: podGone}}

	if got := drainMessage(pods); got != "2 of 4 pods evicted, 1 waiting for a PodDisruptionBudget" {
		t.Errorf("got %q", got)
	}

	if got := drainMessage(pods[2:]); got != "1 of 2 pods evicted" {
		t.Errorf("got %q", got)
	}

	if got := drainMessage(nil); got != "0 of 0 pods evicted" {
		t.Errorf("got %q", got)
	}
}

// A reboot keeps the warnings about the node and its cluster, not the ones about upgrading:
// they are told apart by identity, whatever their wording.
func TestRebootWarnings(t *testing.T) {
	up := upgradePlan{upgradeOnly: []string{"the installer image is custom"}}
	up.Warnings = []string{noDrainWarning + ": pods are stopped", "the installer image is custom", "etcd has 2 healthy members"}

	if got := rebootWarnings(up); !slices.Equal(got, []string{"etcd has 2 healthy members"}) {
		t.Errorf("got %q", got)
	}

	if got := rebootWarnings(upgradePlan{}); got == nil || len(got) != 0 {
		t.Errorf("no warnings must be an empty list for the JSON, got %#v", got)
	}
}

func TestLeaseConflict(t *testing.T) {
	if err := leaseConflict(&kubeAPIError{Code: http.StatusConflict}); !errors.Is(err, errLeaseConflict) {
		t.Errorf("a 409 is a lost race for the lock, got %v", err)
	}

	other := &kubeAPIError{Code: http.StatusForbidden, Message: "no"}
	if err := leaseConflict(other); !errors.Is(err, other) {
		t.Errorf("other API errors pass through, got %v", err)
	}

	if err := leaseConflict(nil); err != nil {
		t.Errorf("nil stays nil, got %v", err)
	}
}

func TestLogFile(t *testing.T) {
	if got := logFile("kubelet.log", nil); got != nil {
		t.Errorf("an empty log is no file, got %v", got)
	}

	got := logFile("kubelet.log", []byte("line\n"))
	if len(got) != 1 || got[0].name != "kubelet.log" || string(got[0].content) != "line\n" {
		t.Errorf("got %v", got)
	}

	// An oversized log keeps its end, the part worth reading.
	big := make([]byte, supportLogBytes+3)
	copy(big[len(big)-3:], "end")

	got = logFile("big.log", big)
	if len(got) != 1 || len(got[0].content) != supportLogBytes || string(got[0].content[supportLogBytes-3:]) != "end" {
		t.Errorf("got %d bytes", len(got[0].content))
	}
}

func TestPeerFromProbeUnreachable(t *testing.T) {
	peer := peerFromProbe("10.0.0.7", nodeProbe{versionErr: errors.New("connection refused"), statusErr: errors.New("connection refused")})

	if peer.node != "10.0.0.7" || peer.reachable || peer.ready || peer.err == "" {
		t.Errorf("got %+v", peer)
	}
}
