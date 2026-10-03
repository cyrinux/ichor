package ichorgo

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

func TestBuildKubeSpanNode(t *testing.T) {
	handshake := time.Unix(1_800_000_000, 0)
	peers := []kubespanPeerInput{
		{id: "pk-b", spec: kubespan.PeerStatusSpec{Label: "cp-2", State: kubespan.PeerStateDown, Endpoint: netip.MustParseAddrPort("10.0.0.3:51820")}},
		{id: "pk-a", spec: kubespan.PeerStatusSpec{
			Label: "cp-1", State: kubespan.PeerStateUp, Endpoint: netip.MustParseAddrPort("10.0.0.2:51820"),
			ReceiveBytes: 10, TransmitBytes: 20, LastHandshakeTime: handshake,
		}},
		{id: "pk-c", spec: kubespan.PeerStatusSpec{Label: "w-1"}},
	}

	got := buildKubeSpanNode("10.0.0.9", peers, nil)

	if !got.Enabled || got.Error != "" || len(got.Peers) != 3 {
		t.Fatalf("got %+v", got)
	}

	// Sorted by label; endpoint/handshake rendered; unknown state for zero values.
	p := got.Peers[0]
	if p.Label != "cp-1" || p.State != "up" || p.Endpoint != "10.0.0.2:51820" || p.LastHandshake != handshake.Unix() || p.Rx != 10 || p.Tx != 20 {
		t.Errorf("peer[0] = %+v", p)
	}

	if got.Peers[1].State != "down" || got.Peers[2].State != "unknown" || got.Peers[2].Endpoint != "" || got.Peers[2].LastHandshake != 0 {
		t.Errorf("peers = %+v", got.Peers)
	}

	if got.Up != 1 || got.Down != 1 {
		t.Errorf("up=%d down=%d", got.Up, got.Down)
	}
}

func TestBuildKubeSpanNodeDisabledAndError(t *testing.T) {
	if got := buildKubeSpanNode("n", nil, nil); got.Enabled || got.Peers == nil {
		t.Errorf("no peers means KubeSpan disabled: %+v", got)
	}

	if got := buildKubeSpanNode("n", nil, errors.New("down")); got.Error == "" {
		t.Errorf("error not reported: %+v", got)
	}
}
