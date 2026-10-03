package ichorgo

// debugSnippet is a ready-made command for the debug shell, for typing on a phone.
// Run snippets are complete and sent with Enter; the others end where the argument
// goes (a host, a subnet…), so the user types it and presses Enter.
type debugSnippet struct {
	Group   string `json:"group"`
	Label   string `json:"label"`
	Command string `json:"command"`
	Run     bool   `json:"run"`
}

// Groups, in display order; the apps translate their titles.
const (
	snippetsInterfaces   = "interfaces"
	snippetsControlPlane = "control_plane"
	snippetsDNS          = "dns"
	snippetsReachability = "reachability"
	snippetsMTU          = "mtu"
	snippetsTLS          = "tls"
	snippetsFirewall     = "firewall"
	snippetsKubeSpan     = "kubespan"
	snippetsCapture      = "capture"
	snippetsNode         = "node"
	snippetsThroughput   = "throughput"
)

// The commands assume the nicolaka/netshoot image and the privileged profile, which
// shares the node's network and PID namespaces: 127.0.0.1 is the node itself, and its
// filesystem is under /proc/1/root (Talos has no shell or coreutils of its own).
var debugSnippets = []debugSnippet{
	{snippetsInterfaces, "addresses", "ip -br -c a", true},
	{snippetsInterfaces, "routes", "ip r; ip -6 r", true},
	{snippetsInterfaces, "links", "ip -br -c l", true},
	{snippetsInterfaces, "listening", "ss -tulpn", true},
	{snippetsInterfaces, "neighbours", "ip neigh", true},
	{snippetsInterfaces, "resolv.conf", "cat /etc/resolv.conf", true},

	{snippetsControlPlane, "apiserver", "curl -sk https://127.0.0.1:6443/readyz?verbose", true},
	{snippetsControlPlane, "kubelet", "curl -s 127.0.0.1:10248/healthz; echo", true},
	{snippetsControlPlane, "talos ports", "nmap -Pn -p 50000,50001,6443,2379,2380,10250 127.0.0.1", true},
	{snippetsControlPlane, "etcd peer", "nc -zvw2 ", false},

	{snippetsDNS, "cluster DNS", "dig +short kubernetes.default.svc.cluster.local @10.96.0.10", true},
	{snippetsDNS, "host DNS", "dig +short siderolabs.com @127.0.0.53", true},
	{snippetsDNS, "upstream", "dig +short siderolabs.com @1.1.1.1", true},
	{snippetsDNS, "lookup", "dig +short ", false},
	{snippetsDNS, "trace", "dig +trace ", false},

	{snippetsReachability, "internet", "ping -c3 1.1.1.1", true},
	{snippetsReachability, "ping", "ping -c3 ", false},
	{snippetsReachability, "path", "mtr -rwzc 10 ", false},
	{snippetsReachability, "tcp port", "nc -zvw2 ", false},
	{snippetsReachability, "sweep subnet", "fping -agq ", false},

	{snippetsMTU, "path MTU", "tracepath -n ", false},
	{snippetsMTU, "1500 no-frag", "ping -M do -s 1472 -c3 ", false},

	{snippetsTLS, "cert dates", `cert() { openssl s_client -connect "$1:${2:-443}" -servername "$1" </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer -dates; }; cert `, false},
	{snippetsTLS, "https headers", "curl -vkI https://", false},

	{snippetsFirewall, "nftables", "nft list ruleset | less", true},
	{snippetsFirewall, "iptables", "iptables-save | less", true},
	{snippetsFirewall, "ipvs", "ipvsadm -Ln", true},
	{snippetsFirewall, "conntrack", "conntrack -S; conntrack -C", true},

	{snippetsKubeSpan, "wireguard link", "ip -d link show kubespan", true},
	{snippetsKubeSpan, "wireguard port", "ss -ulpn | grep 51820", true},

	{snippetsCapture, "dns", "tcpdump -ni any -c 50 port 53", true},
	{snippetsCapture, "host", "tcpdump -ni any -c 100 host ", false},
	{snippetsCapture, "termshark", "termshark -i any", true},

	{snippetsNode, "load & pressure", "cat /proc/loadavg /proc/pressure/cpu /proc/pressure/memory /proc/pressure/io", true},
	{snippetsNode, "memory", "free -m", true},
	{snippetsNode, "disks", "lsblk -f; df -h /proc/1/root/var", true},
	{snippetsNode, "kernel log", "dmesg -T | tail -50", true},
	{snippetsNode, "processes", "top", true},

	{snippetsThroughput, "iperf server", "iperf3 -s", true},
	{snippetsThroughput, "iperf client", "iperf3 -c ", false},
	{snippetsThroughput, "speedtest", "speedtest-cli --simple", true},
}

// DebugSnippets lists the debug shell's ready-made commands, as JSON
// [{"group","label","command","run"}] in display order, so both apps offer the same.
// Only the error is masked: the commands hold fixed well-known addresses, never the
// cluster's, and masking them in screenshot mode would break them.
func DebugSnippets() (out string, err error) {
	defer maskErr(&err)

	return toJSON(debugSnippets)
}
