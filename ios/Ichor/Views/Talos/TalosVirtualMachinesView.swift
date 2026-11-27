import SwiftUI
import IchorCore

/// A Talos VM with the node that runs it: the sheet's item (two nodes may run VMs of one name).
struct TalosVMRef: Identifiable, Hashable {
    let node: String
    let hostname: String
    let vm: TalosVM

    var id: String { "\(node)/\(vm.name)" }
}

/// The virtual machines the Talos hypervisor (Talos 1.15) runs (os:reader): counts by state,
/// then one section per node (just `node`'s VMs when set), each VM with its state, resources and
/// first address; one opens its sheet. A node whose Talos lacks the hypervisor says what it
/// needs. Read-only: power, console and editing come later, in the sheet.
struct TalosVirtualMachinesView: View {
    /// One node's VMs; nil: every node of the cluster.
    var node: String?
    var hostname = ""

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<TalosClusterVMs> = .loading
    /// The VM whose sheet is open ("node/name"): the sheet follows the polled list.
    @State private var selected: String?

    private var report: TalosClusterVMs? {
        if case .loaded(let report, _, _) = state { return report }
        return nil
    }

    private var refs: [TalosVMRef] {
        (report?.nodes ?? []).flatMap { entry in
            entry.report.vms.map { TalosVMRef(node: entry.node, hostname: entry.title, vm: $0) }
        }
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            list(report)
        }
        .task { await load() }
        .task { await poll() }
        .navigationTitle(Text("Talos virtual machines"))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(
            item: Binding(
                get: { selected.flatMap { id in refs.first { $0.id == id } } },
                set: { selected = $0?.id })
        ) { ref in
            TalosVirtualMachineSheet(ref: ref)
        }
    }

    private func list(_ report: TalosClusterVMs) -> some View {
        let shown = report.shown
        return List {
            Section {
                TalosVMCountChips(vms: report.all)
                if node != nil, !hostname.isEmpty {
                    Text(verbatim: hostname).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
            }
            TalosVMProblemsSection(report: report) { ref in
                selected = ref.id
            }
            ForEach(shown) { entry in
                Section {
                    nodeNotice(entry)
                    ForEach(entry.report.vms, id: \.name) { vm in
                        Button {
                            selected = TalosVMRef(node: entry.node, hostname: entry.title, vm: vm).id
                        } label: {
                            TalosVMRow(vm: vm)
                        }
                    }
                } header: {
                    // One node's screen: its name is in the first section already.
                    if node == nil { Text(verbatim: entry.title) }
                }
            }
        }
        .overlay {
            if shown.isEmpty {
                ContentUnavailableView("No virtual machines.", systemImage: "desktopcomputer")
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    /// Why a node shows no VM, or what part of it could not be read.
    @ViewBuilder private func nodeNotice(_ entry: TalosNodeVMsEntry) -> some View {
        if !entry.error.isEmpty {
            ErrorOrNoticeText(message: String(localized: "Could not read everything: \(entry.error)"))
        } else if !entry.report.supported {
            VersionNoticeRow(
                text: FeatureSupport(supported: false, reason: entry.report.reason).localizedNotice
                    ?? String(localized: "Not available on this node's Talos version"))
        } else if !entry.report.errors.isEmpty {
            let errors = entry.report.errors.keys.sorted().compactMap { entry.report.errors[$0] }
            ErrorOrNoticeText(
                message: String(localized: "Could not read everything: \(errors.joined(separator: "; "))"))
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await fetch(client) })
    }

    private func fetch(_ client: TalosClient) async throws -> TalosClusterVMs {
        guard let node else { return try await client.clusterVirtualMachines() }
        let report = try await client.nodeVirtualMachines(node: node)
        return TalosClusterVMs.ofNode(node, hostname: hostname, report: report)
    }

    /// Reads again every few seconds while a VM is on its way.
    private func poll() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(5))
            if report?.busy == true { await load() }
        }
    }
}

/// Running, stopped, failing and pending counts, the KubeVirt list's chips.
struct TalosVMCountChips: View {
    let vms: [TalosVM]

    var body: some View {
        let c = TalosVMCounts(vms)
        VmCountChips(chips: [
            VmCountChip(count: c.running, label: String(localized: "\(c.running) running"), color: .statusOK),
            VmCountChip(count: c.stopped, label: String(localized: "\(c.stopped) stopped"), color: .secondary),
            VmCountChip(count: c.failing, label: String(localized: "\(c.failing) failing"), color: .statusBad),
            VmCountChip(count: c.changing, label: String(localized: "\(c.changing) pending"), color: .statusWarn),
        ])
    }
}

/// "1 running · 1 stopped · 1 failing": the node hub's row under its title.
func talosVMSummary(_ vms: [TalosVM]) -> String {
    let c = TalosVMCounts(vms)
    return [
        c.running > 0 ? String(localized: "\(c.running) running") : nil,
        c.stopped > 0 ? String(localized: "\(c.stopped) stopped") : nil,
        c.failing > 0 ? String(localized: "\(c.failing) failing") : nil,
        c.changing > 0 ? String(localized: "\(c.changing) pending") : nil,
    ]
    .compactMap { $0 }.joined(separator: " · ")
}

/// "4 vCPU · 8 GB", nil when the spec sets neither.
func talosVMResources(_ vm: TalosVM) -> String? {
    let parts = [
        vm.cpus > 0 ? String(localized: "\(vm.cpus) vCPU") : nil,
        vm.memoryBytes > 0 ? formatBytes(vm.memoryBytes) : nil,
    ]
    .compactMap { $0 }
    return parts.isEmpty ? nil : parts.joined(separator: "  ·  ")
}

/// Name and state, then CPUs, memory and the first address; the error under it, in red.
private struct TalosVMRow: View {
    let vm: TalosVM

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 8) {
                Text(verbatim: vm.name).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                VmStateBadge(label: vm.stateLabel, state: vm.vmState)
            }
            let detail = [talosVMResources(vm), vm.firstAddress].compactMap { $0 }
            if !detail.isEmpty {
                Text(verbatim: detail.joined(separator: "  ·  ")).font(.caption).foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            if !vm.error.isEmpty {
                Text(verbatim: vm.error).font(.caption).foregroundStyle(.statusBad).lineLimit(2)
            }
        }
        .foregroundStyle(.primary)
    }
}
