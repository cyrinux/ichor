# Cilium: see why traffic is dropped, like Hubble UI, from the phone

Status: **study**: nothing implemented. Remaining before phase 1: check the open questions
below against a real Cilium cluster (read-only exec into an agent pod).

## Goal

When Cilium is the CNI, answer "why can't A reach B?" from the phone: list the packets Cilium
dropped (network policy first, but every drop reason), who sent them to whom on which port,
in which direction, and **which policy is responsible** or which policy isolates the pod. That is
what Hubble UI shows as red edges and `hubble observe --verdict DROPPED` prints, but with the
policy attribution Hubble leaves out for the common default-deny case.

## What exists today

- `kubeClient.exec` / `execWith` (`kube_exec.go`): `kubectl exec` over WebSocket with fixed
  argv, a timeout and an output cap. This is the only transport the plan needs in phase 1.
- `listDSPods(ctx, k, selector)`: pods by label selector across all namespaces.
- `readAPIGroups` (`GET /apis`): CRD detection, as Argo CD and the data services use it.
- `kubeReadJSON`, `kubeTarget`, demo fallbacks, `maskResult` / `unmaskContext`, `just probe`.
- The inventory already recognises Cilium by image (`quay.io/cilium/cilium` → catalog id `cilium`).
- Pod-per-node runs (netperf, public IPs) and the Argo CD network view (box/edge graph with
  health colours) for the UI.

## Detection

1. Agent pods: `listDSPods(k, "k8s-app=cilium")` across **all namespaces**. Cilium is not
   always in `kube-system` (Helm installs into any namespace). Keep only `Running` pods with a
   `cilium-agent` container; record `spec.nodeName` → pod, namespace and image tag (version).
2. Hubble on/off: ConfigMap `cilium-config` in the agents' namespace, key `enable-hubble`
   (`"true"`). Also read `hubble-event-buffer-capacity` (default 4095 flows per node) to tell the
   user how far back the history goes.
3. Policy CRDs: `cilium.io/v2` in `readAPIGroups` → `ciliumnetworkpolicies`,
   `ciliumclusterwidenetworkpolicies` (plus plain `networking.k8s.io/v1` NetworkPolicies).

No agent pods → the feature is hidden. Agents but Hubble off → phase 1 fallback (below).

## Where the flows come from

| Option | How | Verdict |
|--------|-----|---------|
| **A. `hubble observe` inside each agent** | `exec` in `cilium-agent`: `hubble observe --verdict DROPPED --last N -o jsonpb`. The `hubble` CLI ships in the Cilium image and talks to the agent's local socket (`/var/run/cilium/hubble.sock`): no TLS, no Relay, no extra credential. One exec per node, in parallel, merged by time. | **Chosen for phase 1.** Same pattern as the rest of the app, read-only, works without Relay. |
| B. Hubble Relay over port-forward | Port-forward (WebSocket `portforward.k8s.io`) to `hubble-relay:4245`, then the gRPC `observer.Observer/GetFlows` stream through it. One cluster-wide stream, real live mode. | Later (phase 3) if A is too slow on big clusters. Costs: gRPC over a custom dialer, the observer/flow protos as a Go dependency (size), Relay is optional and often serves mTLS. |
| C. Prometheus drop metrics | `cilium_drop_count_total{reason,direction}` (agent) / `hubble_drop_total` | Only counts, no who→whom. Possible "drops are rising" badge or alert, not the debugger. |
| D. `cilium-dbg monitor --type drop -j` in the agent | Raw datapath drop events, works with Hubble **off**. Streams forever, so it is run for a fixed few seconds. | **Fallback** when `enable-hubble` is false: live only (no history), endpoint IDs instead of names, so less useful; worth it because the alternative is nothing. |
| E. Packet capture (existing Talos capture screen) | pcap on the node | Shows packets, not verdicts: a policy drop is invisible. Not suitable. |

### Command details (option A)

```
hubble observe --verdict DROPPED --last 200 -o jsonpb [--since 15m] [--namespace NS]
```

- `-o jsonpb`: one `GetFlowsResponse` per line, `{"flow":{...},"node_name":"...","time":"..."}`.
- Fixed argv only. Namespace/pod filters come from lists the app read itself and are checked
  with `validateKubeName`, never free text (same rule as `exec` today).
- `execWith` with a larger cap (≈ 2 MiB; a dropped flow is ≈ 1.5–3 KB of JSON) and a 10–15 s
  timeout. Every node runs in parallel, and one failing node becomes a per-node error, not a
  failed screen.
- Also run `--verdict AUDIT` in the same pass when a policy is in audit mode, which shows
  "would be dropped" traffic (useful before enforcing a policy).
- `--type policy-verdict` is a later refinement (it adds the matched policy and the
  `policy_match_type` for allowed traffic).

## Data model (Go → JSON for the apps)

Flows are **grouped**, because one blocked client retries many times: key = (source workload,
destination workload, dest port/proto, direction, drop reason). Each group keeps count,
first/last seen, the nodes that reported it and one sample flow.

```go
type ciliumDrops struct {
    Installed   bool              `json:"installed"`
    Hubble      bool              `json:"hubble"`       // enable-hubble
    Version     string            `json:"version"`      // agent image tag
    BufferFlows int               `json:"bufferFlows"`  // per-node ring size → "history ≈ N flows"
    Groups      []dropGroup       `json:"groups"`
    NodeErrors  map[string]string `json:"nodeErrors"`   // node → why its agent could not be read
}

type dropGroup struct {
    Source, Destination dropPeer
    Port      uint32 `json:"port"`
    Protocol  string `json:"protocol"`  // TCP, UDP, ICMPv4, …
    Direction string `json:"direction"` // INGRESS / EGRESS
    Reason    string `json:"reason"`    // drop_reason_desc, e.g. POLICY_DENIED
    Audit     bool   `json:"audit"`     // AUDIT verdict: would have been dropped
    Count     int    `json:"count"`
    FirstSeen, LastSeen int64
    Nodes     []string        `json:"nodes"`
    DeniedBy  []policyRef     `json:"deniedBy"`  // from the flow, explicit deny rules only
    Isolating []policyRef     `json:"isolating"` // computed, see below
    Hint      string          `json:"hint"`      // one plain sentence
}

type dropPeer struct {
    Namespace, Pod, Workload string // workload via owner refs / appcatalog name trimming
    Identity  uint32
    Labels    []string // k8s:app=…, reserved:world, reserved:host…
    IP        string
    DNSNames  []string // flow.source_names / destination_names (FQDN policies)
    Reserved  string   // "world", "host", "kube-apiserver", "remote-node"… when not a pod
}
```

Flow fields used (Cilium `api/v1/flow/flow.proto`, checked against v1.19.5): `verdict`,
`drop_reason_desc`, `traffic_direction`, `source`/`destination` (`namespace`, `pod_name`,
`labels`, `identity`, `workloads`), `IP`, `l4`, `source_names`/`destination_names`,
`is_reply`, `ingress_denied_by`/`egress_denied_by` (`Policy{name, namespace, kind, labels}`),
`node_name`, `time`.

## Explaining a drop (the part Hubble UI does not do)

1. **Reason in plain words**: map `drop_reason_desc` to a sentence, e.g.
   `POLICY_DENIED` → "no policy allows this", `POLICY_DENY` → "an explicit deny rule matched",
   `AUTH_REQUIRED`, `STALE_OR_UNROUTABLE_IP`, `UNSUPPORTED_L3_PROTOCOL`, `CT_MAP_INSERTION_FAILED`,
   `NO_MAPPING_FOR_NAT_SERVICE`… Unknown reasons are shown as is.
2. **Explicit deny**: `*_denied_by` names the policy. Show it.
3. **Default deny (the usual case)**: `*_denied_by` is **empty**. Hubble only says
   "policy-verdict:none". Ichor computes it instead: for an INGRESS drop, list the
   NetworkPolicies (`podSelector` + `policyTypes` Ingress), CiliumNetworkPolicies
   (`endpointSelector` with `ingress`/`ingressDeny`) and CiliumClusterwideNetworkPolicies that
   **select the destination pod**. Selecting it is what turned on default-deny. EGRESS works the
   same way with the source pod. For each one, show its rules that mention the port. That gives
   "`db` only accepts 5432 from `app=api`; `frontend` is not `app=api`."
   - Label selector matching (`matchLabels` + `matchExpressions` In/NotIn/Exists/DoesNotExist)
     is a small pure function to write and unit-test.
   - Cilium labels on flows are prefixed (`k8s:app=api`, `k8s:io.kubernetes.pod.namespace=…`).
     Strip `k8s:` before matching, and match the namespace through
     `io.kubernetes.pod.namespace` the way Cilium does.
   - Phase 1 reports **which policies select the pod**, not a full simulation of the rule
     (CIDR, FQDN, entities, L7). That is enough to point at the right YAML.
4. **Hint**, e.g. "Add an ingress rule to `db-allow-api` for `app=frontend` on TCP 5432" or
   "This is the DNS egress rule: allow `kube-dns` on UDP 53" (the classic drop when a namespace
   gets an egress default-deny).

## UI (Android + iOS)

- **Entry points**:
  - A *Network drops* card under Kubernetes/Workloads, visible when Cilium is detected, with
    the drop count in the last fetch.
  - The pod sheet: "Dropped traffic", filtered on the pod (`--pod ns/name`).
  - The Argo CD network view: a red edge when drops hit the app's pods.
- **List**: groups sorted by last seen, `frontend → db :5432/TCP · ingress · policy denied ×37`.
  Filters: namespace, direction, reason, hide `reserved:world` noise.
- **Detail sheet**: both peers with labels, reason sentence, denied-by/isolating policies
  (tap → YAML viewer if one exists, else name/namespace), hint, nodes, first/last seen, raw
  JSON of the sample flow (copyable, for a GitHub issue).
- **Live**: phase 1 is pull-to-refresh plus an optional 5 s auto-refresh while the screen is
  open, which re-runs `--last` with `--since <last time>`. A true follow comes in phase 3.
- **Hubble off**: an explanation card ("enable `hubble.enabled` in the Cilium Helm values")
  plus the option D live capture button.
- **AI diagnosis**: add the top drop groups to the diagnosis context, through the same masking
  as the rest (names → placeholders).

## Demo and tests

- Demo cluster already has Cilium pods: add `demoCiliumDrops()` with three groups (default-deny
  ingress to a DB, an egress DNS drop, an explicit `ingressDeny`) and an audit one.
- Fixtures: real `hubble observe -o jsonpb` lines with **neutral names** (the repo is public),
  `testdata/cilium/*.jsonl`. Unit tests: parsing, grouping, reason mapping, selector matching,
  isolating-policy search, per-node error isolation.
- `just probe cilium-drops`, like `just probe argocd`.

## Security and limits

- Read-only: `exec` of a fixed `hubble observe` argv in the agent container (os:admin
  kubeconfig, already used by netperf/Argo CD). No new credential, nothing created in the
  cluster, unlike netperf.
- Flows contain IPs, pod names and DNS names. They stay on the phone except through AI
  diagnosis, which masks them.
- History is short: each agent keeps a ring of `hubble-event-buffer-capacity` flows **of every
  verdict**, so on a busy node old drops are gone within minutes. Say so in the UI ("showing
  what the agents still remember").
- An ingress drop is recorded on the destination's node and an egress drop on the source's
  node, which is why phase 1 asks every agent.

## Phases

1. **Go core**: detection, option A fan-out, parsing, grouping, reason sentences, demo, probe.
2. **Attribution**: read NP/CNP/CCNP, selector matching, `isolating` + hint. Android screen
   then iOS (pod sheet entry, card, list, detail).
3. **Live and scale**: Hubble Relay over port-forward + gRPC `GetFlows` follow, option D
   fallback when Hubble is off, red edges in the Argo CD network view.
4. **Optional**: background alert on a drop-rate jump (option C metrics or periodic phase-1 poll).

## Open questions (to check on a real cluster, read-only)

- `hubble` CLI present in the agent image for the versions we support (1.14+?), and the exact
  `-o jsonpb` line shape there.
- Does Talos' exec latency × N nodes stay under ~3 s for `--last 200`?
- `enable-hubble` key name and default in `cilium-config` across versions.
- For an explicit `ingressDeny` CNP, is `ingress_denied_by` filled with name + namespace + kind
  (CNP vs CCNP) as the proto says?
- Cilium < 1.15 names the debug CLI `cilium` instead of `cilium-dbg` (option D only).
