# Network tools, packet capture and network test

Part of the [Ichor documentation](../README.md#documentation).

## Network tools

**Node menu → Network tools** runs one check from that node and shows the result as fields,
not as terminal output. It uses the same privileged `nicolaka/netshoot` container as the debug
shell, so it needs os:admin; the first run pulls the image.

| Tool | What it shows |
|---|---|
| DNS | The records (A, AAAA, CNAME, MX, NS, PTR, SRV, TXT) with their TTL, the answer's status, the resolver that answered and the query time. Leave the resolver empty for the node's own, or give one such as the cluster DNS. |
| Ping | Packets sent and received, loss, and min / avg / max round trip. |
| Port | Whether a TCP `host:port` is open, closed or filtered. |
| Trace | The path, hop by hop, with loss and average latency (`mtr`, else `tracepath`). |
| HTTP/TLS | The status after redirects and the total time. For https, whether the certificate verifies, its subject, issuer and days left. |

- **Targets:** checked before anything runs: no spaces, no shell characters, no leading dash.
  The last ten are offered again per cluster, on that phone only.
- **More:** a run can be cancelled. The raw output sits under each result, and **Share** sends
  the result as text.

## Packet capture

**Node → Capture** is `talosctl pcap`: pick an interface, an optional tcpdump filter, how long
and how big, and watch the packets arrive. The file is saved on the phone and opens in the app
or in Wireshark.

- **Two nodes at once:** pick a **Second node** (or, on the KubeSpan map, tap a link and then
  **Capture both ends**).
  - **The run:** both nodes are captured with the same filter. Each node gets its own interface
    and its own size limit, and both stop together.
  - **The file:** one file, packets in time order, each marked with its node. It is pcapng
    inside, and Wireshark shows the node as the interface.
  - **Timing:** each packet carries the time of the node that captured it, so a difference
    between the two clocks shifts one node's packets against the other's, and a reply can show
    before its request.
  - **The clock note:** at the start, each node's clock is compared with its NTP server, like
    `talosctl time`. The capture screen and the saved file then say how many ms apart the two
    clocks are, as a warning from 5 ms. The gap is recorded in the file.

## Network test

**Kubernetes → Network** (also under **Cluster insights → Network**) measures the network from a
client node to a server node with netperf, the way `cilium connectivity perf` does, on any CNI:
TCP throughput (`TCP_STREAM`), then TCP round trips (`TCP_RR`: p50/p90/p99 latency), pod to pod
and, optionally, host to host. The test keeps running when you leave the screen, with its progress and Stop in the
notification or Live Activity.

- **What runs:** a namespace `ichor-netperf-<random>`, a `netserver` pod on the server node and
  one short-lived `netperf` pod per measurement on the client node, with the
  `quay.io/cilium/network-perf` image pinned by digest (pulled from quay.io by the nodes). The
  namespace is deleted at the end, also when the test fails or is stopped; one left behind by a
  killed app is deleted by the next test.
- **Pod security:** the pods run as non-root, without capabilities, with a read-only root file
  system and the `RuntimeDefault` seccomp profile, so pod to pod passes the `restricted` level.
  The host network variant creates the namespace with
  `pod-security.kubernetes.io/enforce=privileged`.
- **Firewall:** host to host uses TCP ports 12866 (control) and 12867 (data) on the server node.
  With an ingress firewall on the nodes, allow them between nodes, or the host measurements
  report that netserver does not answer.
- **Load:** each measurement saturates the link between the two nodes for its duration (5, 10 or
  20 s); run it when that is acceptable.
- **Saved results:** the last 20 finished tests of each cluster (stopped ones too, with what they
  measured) are listed under the setup, to compare over time. They are kept encrypted on the
  phone, outside backups, apart for the privacy mask; open one to delete it (or swipe it away on iOS).
- **Charts:** with two saved tests or more between the chosen client and server, an *Over time*
  card charts their pod-to-pod throughput and p50 latency (tap a point to see that test). Each
  latency result also draws where the round trips fell, from the fastest to the slowest, with
  p50 to p99 as a bar. The KubeSpan map shows the last pod-to-pod throughput measured
  between two nodes on their link, and the link's sheet the rest of that test.
- **Why a KubeSpan peer is down:** on the KubeSpan *Peers* tab, tap a peer to see why in plain
  words. The reasons cover:
  - no endpoint to try;
  - a handshake that went stale, with the endpoints tried;
  - an endpoint that keeps changing;
  - a different KubeSpan MTU on the other node;
  - endpoint filters that may exclude every address.

  The raw fields are under *Details*. A node Omni manages shows its SideroLink connection.
  Nothing secret is read: neither the WireGuard key nor the KubeSpan shared secret.
