import SwiftUI
import IchorCore

/// KubeVirt's (and Harvester's) virtual machines (os:admin): counts by state, failing and
/// changing ones first, each with its node, address, guest OS and any import or migration in
/// flight; one opens its sheet with the start/stop/restart/pause/migrate actions. Polls while a
/// VM is on its way, and for a while after an action.
struct VirtualMachinesView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<VirtualMachines> = .loading
    @State private var namespace: String?
    /// The VM whose sheet is open ("namespace/name"): the sheet follows the polled status.
    @State private var selected: String?
    /// VMs with an action in flight.
    @State private var running: Set<String> = []
    @State private var resultMessage: String?
    /// Polls until then even before the VM shows a change: KubeVirt takes a moment.
    @State private var boostUntil = Date.distantPast

    private var report: VirtualMachines? {
        if case .loaded(let report, _, _) = state { return report }
        return nil
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            if report.installed {
                list(report)
            } else {
                notInstalled(report)
            }
        }
        .task { await load() }
        .task { await poll() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(report?.harvester == true ? Text("Virtual machines (Harvester)") : Text("Virtual machines"))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(
            item: Binding(
                get: { selected.flatMap { id in report?.vms.first { $0.id == id } } },
                set: { selected = $0?.id })
        ) { vm in
            VirtualMachineSheet(vm: vm, busy: running.contains(vm.id)) { action, force in
                // Back to the list: the result's alert shows there, over the status it changes.
                selected = nil
                Task { await run(action, on: vm, force: force) }
            }
        }
        .messageAlert($resultMessage)
    }

    private func list(_ report: VirtualMachines) -> some View {
        let chosen = namespace.flatMap { report.namespaces.contains($0) ? $0 : nil }
        let rows = report.inNamespace(chosen)
        return List {
            Section {
                VmCountsRow(counts: report.counts)
                if !report.error.isEmpty {
                    ErrorLine(error: String(localized: "Could not read everything: \(report.error)"))
                }
                if report.namespaces.count > 1 { NamespacePicker(namespaces: report.namespaces, namespace: $namespace) }
            }
            Section {
                ForEach(rows) { vm in
                    Button {
                        selected = vm.id
                    } label: {
                        VmRow(vm: vm, busy: running.contains(vm.id))
                    }
                }
            }
        }
        .overlay {
            if rows.isEmpty {
                ContentUnavailableView("No virtual machines.", systemImage: "desktopcomputer")
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    /// No KubeVirt here: what it would show, and where to read about it; not an error.
    private func notInstalled(_ report: VirtualMachines) -> some View {
        ContentUnavailableView {
            Label("KubeVirt is not installed", systemImage: "desktopcomputer")
        } description: {
            Text(
                "KubeVirt runs virtual machines as Kubernetes objects. With it (or Harvester), this screen lists them and starts, stops, restarts, pauses or migrates them."
            )
        } actions: {
            Link(destination: URL(string: "https://kubevirt.io")!) { Text(verbatim: "kubevirt.io") }
            if !report.error.isEmpty {
                ErrorOrNoticeText(message: String(localized: "Could not read everything: \(report.error)"))
            }
        }
        .themedBackground()
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.virtualMachines() })
    }

    /// Reads again every few seconds while a VM is on its way or an action was just asked.
    private func poll() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(5))
            if Date() < boostUntil || report?.busy == true { await load() }
        }
    }

    private func run(_ action: VmAction, on vm: VirtualMachine, force: Bool) async {
        guard let client = model.client, !running.contains(vm.id) else { return }
        running.insert(vm.id)
        defer { running.remove(vm.id) }
        do {
            let result =
                force
                ? try await client.vmStopForce(namespace: vm.namespace, name: vm.name, graceSeconds: 0)
                : try await client.vmAction(namespace: vm.namespace, name: vm.name, action: action)
            if action == .migrate && !result.migration.isEmpty {
                resultMessage = String(localized: "Migration of \(vm.id) started: \(result.migration)")
            } else if force {
                resultMessage = String(localized: "Forced stop requested for \(vm.id)")
            } else {
                resultMessage = String(localized: "\(action.title) requested for \(vm.id)")
            }
            boostUntil = Date().addingTimeInterval(20)
            await load()
        } catch {
            resultMessage = String(localized: "\(action.title) failed on \(vm.id): \(error.localizedDescription)")
        }
    }
}

/// Running, stopped, failing, migrating and importing counts; only the non-zero ones.
private struct VmCountsRow: View {
    let counts: VmCounts

    var body: some View {
        VmCountChips(chips: [
            VmCountChip(
                count: counts.running, label: String(localized: "\(counts.running) running"), color: .statusOK),
            VmCountChip(
                count: counts.stopped, label: String(localized: "\(counts.stopped) stopped"), color: .secondary),
            VmCountChip(
                count: counts.failing, label: String(localized: "\(counts.failing) failing"), color: .statusBad),
            VmCountChip(
                count: counts.migrating, label: String(localized: "\(counts.migrating) migrating"), color: .statusWarn),
            VmCountChip(
                count: counts.importing, label: String(localized: "\(counts.importing) importing a disk"),
                color: .statusWarn),
        ])
    }
}

/// Name and status, then namespace, node, first address and guest OS, then what is in flight.
private struct VmRow: View {
    let vm: VirtualMachine
    let busy: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 8) {
                Text(verbatim: vm.name).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                if busy { ProgressView().controlSize(.small) }
                VmStatusBadge(vm: vm)
            }
            Text(
                verbatim: [vm.namespace, vm.node, vm.firstIP ?? "", vm.guestOS].filter { !$0.isEmpty }.joined(
                    separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if let progress = vmProgressText(vm.progress) {
                Text(verbatim: progress).font(.caption).foregroundStyle(.statusWarn)
            }
        }
        .foregroundStyle(.primary)
    }
}
