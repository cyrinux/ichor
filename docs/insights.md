# Cluster insights

Part of the [Ichor documentation](../README.md#documentation).


Open **Cluster insights** from the overview to compare selected effective settings or
record an incident. These features are read-only and work with `os:reader`.

- **Configuration drift:** compares DNS, NTP, interface MTUs, extension versions,
  Secure Boot, UKI and Talos versions. Without a saved baseline, each role is compared
  with its first reachable node; save a baseline to compare the same nodes over time.
  Differences can be intentional. Failed or unavailable fields stay unknown, and a
  node absent from the baseline is not treated as a configuration change. A DNS, NTP or MTU
  difference has **Fix with a patch…**: it opens that node's machine config with the
  reference's value already in the draft, on the review (`os:admin` to apply).
- **Incident recorder:** collects live Talos events and samples node readiness,
  service state, network link state and counters about every five seconds (plus API
  call time). It stops after ten minutes or when its screen leaves the foreground.
  Review the latest saved timeline when you return; starting a new session replaces
  it. The timeline retains up to 600 entries, with bounded detail per entry, and
  reports discarded older entries. Metric samples can be shown or hidden.
  Evidence cards show readiness, service health and link changes with status icons
  and colors. Metric cards format CPU percentages, device throughput, disk activity
  and average I/O time; network warnings use errors and drops from the sampled
  interval rather than historical totals. Expand **Technical details** for the
  original bounded payload. New samples retain a separate readable summary of up
  to eight disks, eight interfaces and eight collection errors, with omissions
  reported. Older truncated samples remain available as technical details; record
  a new session to get their formatted metrics.
- **Bottleneck metrics:** the node’s **Live** tab now includes CPU I/O wait and VM
  steal time, per-disk throughput/activity/average I/O time, and per-interface
  throughput/errors/drops. Rates use consecutive counters; rebooted nodes, new
  devices and failed reads do not produce misleading rate spikes.

Baselines and recordings are encrypted on the device, excluded from backups, and
separated by cluster and screenshot-mode settings. Delete them from the insights
screen. No machine configurations, kubeconfigs or raw logs are collected. Event
messages and node details can still contain operational information.

The recorder is a foreground troubleshooting tool, not continuous monitoring.
Sampled changes between polls can be missed. Events use node clocks, sampled
changes use the phone clock, and chronological proximity does not establish cause.
Disk activity alone does not establish saturation, especially on parallel devices.
