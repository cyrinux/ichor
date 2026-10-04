# Sysadmin features

Status: **planning**. Checked against the code on 2026-10-04. Each section gets its own folder
when work starts. Conventions: [README.md](README.md#conventions-every-plan-follows).

## S1. etcd care: exists; add node reset and control-plane replacement (M)

**Today (both apps):** `StartEtcdSnapshot` and `EtcdAlarmDisarm` (`etcdbackup.go`),
`EtcdDefragment` (`defrag.go`, followers first, leader last), `EtcdMemberPlan`/`EtcdRemoveMember`/
`EtcdForfeitLeadership` (`etcdmembers.go`), `ETCD_ALARM` background alert. UI: `ui/etcd/*`, `EtcdView.swift`.

**Plan:**
0. **Encrypted snapshots** (age, public keys or passphrase, restorable with `age -d` on any
   Unix machine): [../etcd-encrypted-snapshot/README.md](../etcd-encrypted-snapshot/README.md). First.
1. **Node reset** (`talosctl reset`): `Reset(cfg, ctx, node, graceful, reboot bool, wipe string)`
   with `MachineService.Reset` (graceful = leave etcd first; wipe system/ephemeral/all). Through
   `nodeAction`, typed hostname, os:admin. Refused on the last control plane and when
   `computeMemberPlan` says quorum would be lost.
2. **Replace a control plane** (guided): dead member found by `EtcdStatus` → remove it
   (`EtcdRemoveMember`) → reset/wipe the node if reachable → instructions to boot the new one
   (and S8 join when it exists) → wait until the new member shows up and is healthy. A checklist
   screen built on the D1 run screen.
3. **NOSPACE playbook**: when the `NOSPACE` alarm is active, one button: snapshot → defrag (rolling)
   → disarm → recheck, using the existing calls in sequence.

## S2. Alertmanager: partial → full (M)

**Today:** Prometheus queries, presets and discovery (`prom.go`, `prom_source.go`,
`prom_discover.go`, `prom_presets.go`); `prom_discover.go` excludes services named
`alertmanager` on purpose. No alert list, no silences.

**Plan:**
- Discover Alertmanager the same way (service proxy or URL, same auth options as Prometheus),
  `alertmanager_source.go`. Go: `AMAlerts` (`GET /api/v2/alerts?active=true&silenced=false`, grouped
  by alertname, severity, labels), `AMSilences`, `AMSilenceCreate(matchers, duration, comment)`
  (creator `ichor@<device>`), `AMSilenceExpire(id)`. Without Alertmanager: fall back to
  `ALERTS{alertstate="firing"}` through Prometheus (read-only).
- UI: an **Alerts** screen (from the Overview badge): firing alerts by severity, summary/description
  annotations, runbook_url link, "Silence 1h / 4h / until 9:00 / custom" with a reason, active
  silences with Expire. Labels that name a node or pod link to Ichor's screens.
- Background monitor: optional, notify on new critical alerts (dedupe by fingerprint), feeds U1
  actionable notifications ("Silence 1h" button).

## S3. Push without polling (L)

**Today:** Android WorkManager every 15/30/60 min (`MonitorScheduler.kt`), iOS best-effort
BGAppRefresh (`BackgroundMonitor.swift`); active cluster only; silent when unreachable.

**Plan:**
1. **First, cheap wins:** watch all clusters, not only the active one (loop in `MonitorWorker`
   and `Monitor.swift`), and an "unreachable for N runs" alert (opt-in, VPN users would get noise).
2. **Relay (opt-in, separate component):** `ichor-relay`, a small Go binary built from the same
   module (`go/cmd/relay`): runs in the cluster as a Deployment with a read-only RBAC, reuses the
   monitor's snapshot/diff code (port `evaluate` to Go first, shared by the apps through
   gomobile), and pushes through **UnifiedPush** (ntfy or any distributor, FOSS-friendly) and
   optionally an **Alertmanager webhook receiver** (`/alertmanager`). The phone registers its
   UnifiedPush endpoint by scanning a QR the relay prints; payloads are end-to-end encrypted
   with a key exchanged in that QR (relay never sees anything readable on the push server).
   iOS: UnifiedPush does not exist; use ntfy's iOS app via its APNs relay, or skip.
   Helm chart + plain manifest in `deploy/relay/`.
3. The app keeps polling as a fallback and dedupes by alert key.

Open: is a server-side component in scope for an app project? (Proposal: yes, opt-in, documented as such.)

## S4. Packet capture: exists

`pcapcapture.go` (`StartPacketCapture`, filter compiler, size/time caps), pcap viewer and
summaries, `ui/capture/*`, `CaptureView.swift`. Possible later: capture on two nodes at once
(both ends of a flow), ring-buffer mode stopped by a match. No plan now.

## S5. Node network tools: partial → structured tools (M)

**Today:** `NodeConnections` (netstat), `StartDebugShell` (privileged netshoot, os:admin) with
canned snippets (`debugsnippets.go`: dig, ping, mtr, nc, tracepath…), netperf between nodes.

**Plan:** a **Network tools** tab on the node: DNS lookup, ping, TCP port check, traceroute, HTTP
check. Go `NodeNetTool(cfg, ctx, server, node, tool, target string, listener)`: runs the debug
container non-interactively (reuse `debug.go` launch, fixed argv per tool, target validated as
host/IP/port), streams output, parses it into results (`nettools_parse.go`: RTT min/avg/max/loss,
hops with RTT, resolved records, port open/closed/filtered, HTTP status + TLS expiry). Same
cleanup as netperf. Also offered from D8 pull failures and the Hubble flow sheet ("test from this node").

## S6. Storage: partial → alerts and trend (M)

**Today:** `NodeMounts`, `NodeVolumes` (VolumeStatus, user volumes), `NodeDiskUsage`,
`NodeDiskHealth` (SMART, Talos 1.15+); screens on both apps. No alerting.

**Plan:**
1. Monitor: add mount usage for EPHEMERAL, STATE and user volumes to the snapshot; alerts at
   85 % / 95 % (thresholds in settings), and SMART failing/pre-fail when available.
2. Trend: store usage in the U4 history ring; storage screen shows 7-day sparkline and "full in
   ~N days" (linear fit, only when ≥ 3 days of data).
3. **Disk wipe** for re-provisioning (`BlockDeviceWipe`, Talos 1.8+): only on disks not in use by
   any volume (checked through `NodeVolumes`), typed hostname + disk name. os:admin.

## S7. Hardware sensors: missing (S)

Go `NodeSensors`: read `/sys/class/hwmon/*/{name,temp*_input,temp*_label,temp*_crit,fan*_input}`
and `/sys/class/thermal/thermal_zone*/{type,temp}` with the Talos `Read`/`LS` APIs (as `storage.go`
uses `LS`); throttling from `/sys/devices/system/cpu/cpu*/thermal_throttle/*_count` (Intel) when present.
Hardware screen section "Sensors"; Live tab chart; monitor alert above `crit - 5 °C` (opt-in).
VMs usually have none: hide the section then.

## S8. Join a node from the phone: missing (XL)

**Today:** `DiscoverNodes`/`AddContextNodes` (existing members only), `NodeMachineConfig` read-only.

**Plan:**
1. Find nodes in **maintenance mode**: the user enters an IP (or scans a QR shown on the node
   console's dashboard), Go connects with the insecure maintenance client and reads disks,
   interfaces and version (`talosctl get disks --insecure` equivalent).
2. Build the config: take an existing worker's (or control plane's) machine config
   (`machineConfigYAML` with secrets), strip node-specific parts (hostname, static addresses,
   install disk), apply the user's choices (install disk from the list, hostname, optional static IP).
3. `ApplyConfiguration` insecure, then follow the node through install → reboot → join (reuse the
   upgrade follower and `k8s.NodeStatus`), finally add it to the talosconfig (`AddContextNodes`).
   Control plane joins also check etcd membership.
Depends on D3's apply code. Security: the generated config holds cluster secrets; never stored,
only sent to the node over the maintenance API (TLS without client auth, as talosctl does) after
an explicit warning.

## S9. Action audit log: missing (M), do early

Every mutation from the phone is appended to an encrypted local log per cluster: time, action,
target, parameters (masked), result/error, app version. Single hook points: `TalosRepository.call {}`
(Android) and `TalosClient.run` (iOS), with an explicit list of mutating Go functions
(Reboot, Shutdown, ServiceAction, StartUpgrade, etcd actions, Kube*Action/Delete/Trigger/Scale…).
Settings → Activity: list, filter, export as JSON/CSV (share sheet), kept 90 days. Also attached
to the support bundle and the incident summary (U9) when the user chooses.
Optional later: write a Kubernetes Event on the target object (`reason: IchorAction`) so teammates
see it with `kubectl get events`.

## S10. Omni: missing (XL)

Omni-managed clusters expose the Talos API through Omni with an Omni service account
(`omnictl`) instead of a talosconfig with client certs. Plan a spike: (1) import an Omni service
account key, (2) list clusters through the Omni API, (3) open the Talos API through Omni's proxy
with the machinery client (the `client.WithGRPCDialOptions` + Omni auth interceptor used by
omnictl), (4) check which Ichor features still work (most read APIs should). Decide after the spike.
