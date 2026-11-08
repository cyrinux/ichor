package ichorgo

import (
	"net/netip"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
)

const (
	caControlPlane1 = "192.0.2.11"
	caControlPlane2 = "192.0.2.12"
	caWorker        = "192.0.2.21"
)

// caCluster is a fake cluster of two control planes and a worker sharing one secrets bundle:
// each holds its real machine config, the control planes report the issuing CA, and
// cluster discovery lists all three. It returns the fake, a talosconfig naming every node,
// and the PEM of the cluster's CA.
func caCluster(t *testing.T) (*fakeTalos, string, []byte) {
	t.Helper()

	in, err := generate.NewInput("test", "https://192.0.2.10:6443", "1.34.0")
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeTalos()
	nodes := []struct {
		addr string
		typ  machine.Type
	}{{caControlPlane1, machine.TypeControlPlane}, {caControlPlane2, machine.TypeControlPlane}, {caWorker, machine.TypeWorker}}

	caPEM := in.Options.SecretsBundle.Certs.OS.Crt

	for _, n := range nodes {
		f.addNode(t, n.addr, "v1.14.2", n.typ)

		cfg, err := in.Config(n.typ)
		if err != nil {
			t.Fatal(err)
		}

		raw, err := cfg.Bytes()
		if err != nil {
			t.Fatal(err)
		}

		provider, err := configloader.NewFromBytes(raw)
		if err != nil {
			t.Fatal(err)
		}

		f.put(n.addr, config.NewMachineConfigWithID(provider, config.ActiveID))

		if n.typ == machine.TypeControlPlane {
			root := secrets.NewOSRoot(secrets.OSRootID)
			root.TypedSpec().IssuingCA = in.Options.SecretsBundle.Certs.OS
			f.put(n.addr, root)
		}
	}

	for _, n := range nodes {
		m := cluster.NewMember(cluster.NamespaceName, "host-"+n.addr)
		m.TypedSpec().Hostname = caHostOf(n.addr)
		m.TypedSpec().MachineType = n.typ
		m.TypedSpec().Addresses = []netip.Addr{netip.MustParseAddr(n.addr)}
		f.put(caControlPlane1, m)
	}

	return f, f.start(t, caControlPlane1, caControlPlane2, caWorker), caPEM
}

// caHostOf is the hostname fakeTalos.addNode gives a node.
func caHostOf(addr string) string {
	out := []byte("host-" + addr)
	for i, c := range out {
		if c == '.' {
			out[i] = '-'
		}
	}

	return string(out)
}
