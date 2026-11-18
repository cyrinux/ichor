package ichorgo

import (
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"time"
)

// The demo node's files: a small invented tree under the browser's starting points, with
// a pod log, the machine config (already redacted), resolv.conf, a /proc table and a
// binary file, so every state of the screens shows.

// demoPodLogDir is a pod's log directory as the kubelet names it: namespace_pod_uid.
const demoPodLogDir = "/var/log/pods/shop_web-7c9d8b6f5-k2x9p_3f1c2a9e-0b7d-4c61-9a2e-5d8f7e6b1c40"

var demoNodeFileTree = map[string]string{
	demoPodLogDir + "/web/0.log": "2026-01-02T03:04:05.000000001Z stdout F listening on :8080\n" +
		"2026-01-02T03:04:06.120000000Z stdout F GET /healthz 200 0.4ms\n" +
		"2026-01-02T03:05:11.873000000Z stderr F upstream timeout talking to checkout:8080, retrying\n" +
		"2026-01-02T03:05:12.004000000Z stdout F GET /cart 200 131ms\n",
	demoPodLogDir + "/istio-proxy/0.log":     "2026-01-02T03:04:04.500000000Z stderr F envoy proxy is ready\n",
	"/var/log/audit/kube/kube-apiserver.log": `{"kind":"Event","level":"Metadata","verb":"get","requestURI":"/api/v1/namespaces/shop/pods"}` + "\n",
	// A binary file: the browser says "binary" and shows no text.
	"/var/log/lastlog": "\x00\x00\x00\x00\x2a\x00demo\x00\x00\x00\x00",
	machineConfigPath: "version: v1alpha1\nmachine:\n  type: worker\n  token: '******'\n  ca:\n    crt: LS0tLS1CRUdJTi...\n    key: \"\"\n" +
		"  install:\n    disk: /dev/sda\n    image: factory.talos.dev/installer/376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba:v1.11.3\n" +
		"cluster:\n  clusterName: demo\n  controlPlane:\n    endpoint: https://demo.invalid:6443\n  token: '******'\n  secretboxEncryptionSecret: '******'\n",
	"/etc/resolv.conf": "nameserver 127.0.0.53\nsearch .\noptions ndots:1\n",
	"/etc/hosts":       "127.0.0.1 localhost\n::1 localhost ip6-localhost\n",
	"/proc/net/route": "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"eth0\t00000000\t0100000A\t0003\t0\t0\t1024\t00000000\t0\t0\t0\n",
	"/run/containerd/containerd.sock.ttrpc": "",
}

// demoNodeFilesModified is the demo files' modification time: a fixed age, so the screens
// say "2 hours ago" whatever the clock.
var demoNodeFilesModified = func() time.Time { return time.Now().Add(-2 * time.Hour) }

// demoNodeFiles lists dir in the demo tree; a directory that is neither in the tree nor a
// starting point is missing, as on a node.
func demoNodeFiles(dir string) (nodeDirListing, error) {
	modified := demoNodeFilesModified().UTC().Format(time.RFC3339)
	children := map[string]nodeFileEntry{}

	for p, content := range demoNodeFileTree {
		rest, ok := strings.CutPrefix(p, strings.TrimSuffix(dir, "/")+"/")
		if !ok {
			continue
		}

		name, _, deeper := strings.Cut(rest, "/")
		e := nodeFileEntry{Name: name, Path: path.Join(dir, name), Type: nodeFileTypeFile, Size: int64(len(content)), Modified: modified}

		if deeper {
			e.Type, e.Size = nodeFileTypeDir, 0
		}

		children[name] = e
	}

	if len(children) == 0 && !slices.Contains(nodeFileRoots, dir) {
		return nodeDirListing{}, errors.New("lstat " + dir + ": no such file or directory")
	}

	return buildNodeDirListingFrom(dir, slices.Collect(maps.Values(children))), nil
}

// buildNodeDirListingFrom sorts entries as a node's listing is sorted.
func buildNodeDirListingFrom(dir string, entries []nodeFileEntry) nodeDirListing {
	out := nodeDirListing{Dir: dir, Roots: nodeFileRoots, Entries: entries}
	if out.Entries == nil {
		out.Entries = []nodeFileEntry{}
	}

	slices.SortFunc(out.Entries, compareNodeEntries)

	return out
}

func demoNodeFileRead(filePath string) (nodeFileText, error) {
	content, ok := demoNodeFileTree[filePath]
	if !ok {
		return nodeFileText{}, errors.New("open " + filePath + ": no such file or directory")
	}

	return readNodeFile(filePath, strings.NewReader(content))
}
