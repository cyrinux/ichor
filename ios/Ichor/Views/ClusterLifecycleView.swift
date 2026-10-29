import SwiftUI
import IchorCore

/// Cluster lifecycle (os:admin): the workload clusters a management cluster runs with Cluster
/// API, their machines (by cluster, control plane first) and machine deployments, and the
/// Sidero Metal servers and server classes, problems first in each section. A tap opens the
/// object's summary; nothing is changed here.
struct ClusterLifecycleView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ClusterLifecycle> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { r in
            list(r)
        }
        .navigationTitle(Text("Cluster lifecycle"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        let loaded: LoadState<ClusterLifecycle> = await .from { try await client.clusterLifecycle() }
        state = state.refreshed(with: loaded)
    }

    @ViewBuilder
    private func list(_ r: ClusterLifecycle) -> some View {
        List {
            Section { header(r) }
            if !r.installed || r.isEmpty {
                Section {
                    Text("No Cluster API cluster or Sidero Metal server on this cluster.").foregroundStyle(.secondary)
                }
            } else {
                if !r.clusters.isEmpty {
                    Section("Clusters") {
                        ForEach(r.clusters) { ClusterRow(cluster: $0, version: r.apiVersion(.cluster)) }
                    }
                }
                ForEach(r.machinesByCluster, id: \.id) { group in
                    Section {
                        ForEach(group.machines) { MachineRow(machine: $0, version: r.apiVersion(.machine)) }
                    } header: {
                        Text(verbatim: String(localized: "Machines · \(group.cluster)"))
                    }
                }
                if !r.machineDeployments.isEmpty {
                    Section("Machine deployments") {
                        ForEach(r.machineDeployments) {
                            MachineDeploymentRow(deployment: $0, version: r.apiVersion(.machineDeployment))
                        }
                    }
                }
                if !r.servers.isEmpty {
                    Section("Servers") {
                        ForEach(r.servers) { ServerRow(server: $0) }
                    }
                }
                if !r.serverClasses.isEmpty {
                    Section("Server classes") {
                        ForEach(r.serverClasses) { ServerClassRow(serverClass: $0) }
                    }
                }
            }
        }
        .themedBackground()
    }

    /// What the cluster serves (Cluster API version, Sidero Metal, the Talos providers) and the counts.
    @ViewBuilder
    private func header(_ r: ClusterLifecycle) -> some View {
        if !r.error.isEmpty {
            Text(verbatim: String(localized: "Some sections could not be read: \(r.error)")).font(.callout)
                .foregroundStyle(.secondary)
        }
        let served = [
            r.version.isEmpty ? nil : String(localized: "Cluster API \(r.version)"),
            r.sidero ? String(localized: "Sidero Metal") : nil,
            r.talosControlPlane || r.talosBootstrap ? String(localized: "Talos providers") : nil,
        ].compactMap { $0 }
        if !served.isEmpty {
            Text(verbatim: served.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
        }
        if r.installed && !r.isEmpty {
            HStack(spacing: 12) {
                Text(verbatim: String(localized: "\(r.counts.critical) critical"))
                    .font(.callout.weight(.medium))
                    .foregroundStyle(r.counts.critical > 0 ? Color.statusBad : .secondary)
                Text(verbatim: String(localized: "\(r.counts.warning) warning"))
                    .font(.callout.weight(.medium))
                    .foregroundStyle(r.counts.warning > 0 ? Color.statusWarn : .secondary)
                Text(verbatim: String(localized: "\(r.counts.ok) ok"))
                    .font(.callout.weight(.medium)).foregroundStyle(.secondary)
                Spacer()
            }
        }
        if r.truncated {
            Text("Long list: only the first 200 objects of each kind are listed, problems first.")
                .font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// One row: health dot and name, the details that matter, then (for a problem) the failure or
/// the conditions that are not True, in full; pushes the object summary.
private struct RowShell: View {
    let kind: LifecycleKind
    let version: String
    let namespace: String
    let name: String
    let title: String
    let health: ServiceHealth
    let line: [String?]
    var problem: String = ""

    var body: some View {
        NavigationLink {
            KubeObjectView(
                resource: KubeAPIResource(
                    group: kind.group, version: version, resource: kind.resource, kind: kind.kind,
                    namespaced: kind.namespaced),
                namespace: kind.namespaced ? namespace : "",
                name: name,
            )
        } label: {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: health).padding(.top, 5)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title).font(.subheadline.monospaced()).lineLimit(1)
                    let details = line.compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
                    if !details.isEmpty {
                        Text(verbatim: details).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                    }
                    if health.needsAttention && !problem.isEmpty {
                        Text(verbatim: problem).font(.caption)
                            .foregroundStyle(health == .critical ? Color.statusBad : .secondary)
                    }
                }
            }
        }
    }
}

/// A workload cluster: its phase, control plane and workers ready of desired, endpoint and versions.
private struct ClusterRow: View {
    let cluster: CapiCluster
    let version: String

    var body: some View {
        let c = cluster
        let workers = c.workers.flatMap { $0.desired > 0 || $0.ready > 0 ? $0.ratio : nil }
        RowShell(
            kind: .cluster, version: version, namespace: c.namespace, name: c.name, title: c.name, health: c.health,
            line: [
                c.phase,
                c.controlPlane.map { String(localized: "control plane \($0.ratio)") },
                workers.map { String(localized: "workers \($0)") },
                c.endpoint,
                c.versions.joined(separator: ", "),
                c.paused ? String(localized: "paused") : nil,
            ], problem: c.problem)
    }
}

/// A Machine: its phase, role, node, version and addresses, and how long it has been stuck.
private struct MachineRow: View {
    let machine: CapiMachine
    let version: String

    var body: some View {
        let m = machine
        RowShell(
            kind: .machine, version: version, namespace: m.namespace, name: m.name, title: m.name, health: m.health,
            line: [
                m.phase,
                m.controlPlane ? String(localized: "control plane") : nil,
                m.node,
                m.version,
                m.addresses.joined(separator: ", "),
                !m.stuck
                    ? nil
                    : m.stuckSince > 0
                        ? String(localized: "stuck since \(relativeTime(m.stuckSince))") : String(localized: "stuck"),
            ], problem: m.problem)
    }
}

/// A MachineDeployment: its phase, its replicas ready and available of desired, and version.
private struct MachineDeploymentRow: View {
    let deployment: CapiMachineDeployment
    let version: String

    var body: some View {
        let d = deployment
        let ready = String(d.ready)
        let available = String(d.available)
        let desired = String(d.desired)
        RowShell(
            kind: .machineDeployment, version: version, namespace: d.namespace, name: d.name, title: d.name,
            health: d.health,
            line: [
                d.phase,
                String(localized: "\(ready) ready, \(available) available of \(desired)"),
                d.version,
            ])
    }
}

/// A bare-metal server: accepted, cordoned, in use, clean; its power, classes, CPU and memory.
private struct ServerRow: View {
    let server: SideroServer

    var body: some View {
        let s = server
        RowShell(
            kind: .server, version: LifecycleKind.sideroVersion, namespace: "", name: s.name, title: s.title,
            health: s.health,
            line: [
                s.hostname.isEmpty || s.name == s.hostname ? nil : s.name,
                s.accepted ? String(localized: "accepted") : String(localized: "not accepted"),
                s.cordoned ? String(localized: "cordoned") : nil,
                s.inUse ? String(localized: "in use") : nil,
                s.isClean ? String(localized: "clean") : nil,
                s.power.isEmpty ? nil : String(localized: "power \(s.power)"),
                s.serverClasses.isEmpty
                    ? nil : String(localized: "classes: \(s.serverClasses.joined(separator: ", "))"),
                s.addresses.joined(separator: ", "),
                s.cpu,
                s.memoryBytes > 0 ? ByteCountFormatter.string(fromByteCount: s.memoryBytes, countStyle: .memory) : nil,
            ])
    }
}

/// A ServerClass: how many of its servers are free and in use; none free is a warning.
private struct ServerClassRow: View {
    let serverClass: SideroServerClass

    var body: some View {
        let sc = serverClass
        let available = String(sc.available)
        let inUse = String(sc.inUse)
        RowShell(
            kind: .serverClass, version: LifecycleKind.sideroVersion, namespace: "", name: sc.name, title: sc.name,
            health: sc.exhausted ? .warning : .ok, line: [String(localized: "\(available) available, \(inUse) in use")])
    }
}
