import SwiftUI
import IchorCore

/// A node's hub: a header (address, role, uptime, cordon), then Status (the tabs, one tap away),
/// Inspect, Troubleshoot, Operate and Power as list sections, so nothing hides behind a menu.
struct NodeDetailView: View {
    let ref: NodeRef

    @Environment(AppModel.self) private var model
    @Environment(LockStore.self) private var lockStore
    @Environment(\.dismiss) private var dismiss
    @State private var powerAction: PowerAction?
    @State private var running = false
    @State private var resultMessage: String?
    @State private var succeeded = false
    /// The screen a row pushes.
    @State private var open: NodeHubScreen?
    /// The cordon / uncordon choice is shown (the node's current state is not known here).
    @State private var showingCordon = false
    @State private var showingReset = false
    /// The cluster's history over `period` (uptime strip); nil without one.
    @State private var history: HistoryQueryResult?
    @State private var period = HistoryPeriod.week
    /// Closes the hub after a power action when it is not a pushed screen (beside the nodes list
    /// on an iPad, where dismiss would close the list too); nil: dismiss.
    private let onDone: (() -> Void)?

    init(ref: NodeRef, initialAction: PowerAction? = nil, initialCordon: Bool = false, onDone: (() -> Void)? = nil) {
        self.ref = ref
        self.onDone = onDone
        _powerAction = State(initialValue: initialAction)
        _showingCordon = State(initialValue: initialCordon)
    }

    var body: some View {
        List {
            header
            NodeStatusSection(ref: ref, open: $open)
            NodeInspectSection(node: ref.address, open: $open)
            NodeTroubleshootSection(node: ref.address, open: $open)
            NodeOperateSection(
                node: ref.address, hostname: ref.hostname, maintenanceRunning: maintenanceRunning, open: $open,
                showingReset: $showingReset, showingCordon: $showingCordon)
            NodePowerSection(node: ref.address, powerAction: $powerAction)
        }
        .themedBackground()
        .navigationTitle(ref.hostname)
        .navigationBarTitleDisplayMode(.inline)
        // What this node's Talos version can do: gates the rows once known.
        .task { await model.loadFeatures(node: ref.address) }
        // Masked like the screen in screenshot mode: reloaded when it changes.
        .task(id: "\(period.rawValue)#\(model.dataGeneration)") {
            history = await HistoryStore.query(
                cluster: model.activeSummary.map(monitorClusterKey) ?? "", period: period)
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if running {
                    ProgressView()
                } else {
                    Menu {
                        // No tab: the link opens this hub.
                        ShareLinkButton(target: .node(address: ref.address, hostname: ref.hostname, tab: ""))
                        PinButton(
                            target: .node(address: ref.address, hostname: ref.hostname, tab: ""), label: ref.hostname)
                        Button {
                            copyAddress()
                        } label: {
                            Label("Copy IP", systemImage: "doc.on.doc")
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                    }
                }
            }
        }
        .navigationDestination(item: $open) { $0.view(ref) }
        .confirmationDialog(
            String(localized: "Kubernetes scheduling on \(ref.hostname)"), isPresented: $showingCordon,
            titleVisibility: .visible
        ) {
            Button("Cordon", role: .destructive) { Task { await cordon(true) } }
            Button("Uncordon") { Task { await cordon(false) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("A cordoned node gets no new pods; the pods it runs stay. Uncordon it to schedule pods on it again.")
        }
        .sheet(isPresented: $showingReset) {
            NodeResetSheet(node: ref.address, hostname: ref.hostname) { request in
                showingReset = false
                Task { await performReset(request) }
            }
        }
        .sheet(item: $powerAction) { action in
            PowerSheet(action: action, hostname: ref.hostname, role: ref.role) { request in
                powerAction = nil
                Task { await perform(request) }
            }
        }
        .alert(resultMessage ?? "", isPresented: $resultMessage.isPresent()) {
            Button("OK") {
                guard succeeded else { return }
                if let onDone { onDone() } else { dismiss() }
            }
        }
    }

    /// Address (long press or swipe to copy), role, cordon, Unreachable with why and when it
    /// last answered for a node known not to answer, and the uptime strip.
    private var header: some View {
        let reachability = model.reachability(of: ref.address)
        return Section {
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: ref.address).font(.body.monospaced()).textSelection(.enabled)
                    if !role.isEmpty { Text(role).font(.caption).foregroundStyle(.secondary) }
                    if let reason = unreachableReason(reachability) {
                        Text(verbatim: reason).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Spacer()
                // A maintenance of this node cordons it until it ends; otherwise the state is not known here.
                if maintenanceRunning { StatusPill(label: String(localized: "Cordoned"), color: .orange) }
                if reachability.isDown { StatusPill(label: String(localized: "Unreachable"), color: .red) }
            }
            .accessibilityElement(children: .combine)
            .contextMenu {
                Button {
                    copyAddress()
                } label: {
                    Label("Copy IP", systemImage: "doc.on.doc")
                }
            }
            .swipeActions(edge: .trailing) {
                Button {
                    copyAddress()
                } label: {
                    Label("Copy IP", systemImage: "doc.on.doc")
                }
                .tint(.blue)
            }
            if let history, let uptime = history.node(ref.address),
                uptime.uptimePercent != nil || !uptime.intervals.isEmpty
            {
                NodeUptimeStrip(
                    uptime: uptime, from: history.from, to: history.to, period: $period,
                    unchecked: history.uncheckedTail(uptime), downNow: reachability.isDown)
            }
        }
    }

    private var role: String {
        ref.role == "controlplane" ? String(localized: "control plane") : ref.role
    }

    /// Why a node known not to answer does not, by its error kind (else the core's words), and
    /// when it last did; nil for a node that answers or is unknown.
    private func unreachableReason(_ reachability: NodeReachability?) -> String? {
        guard let reachability, !reachability.reachable else { return nil }
        let reason: String? =
            switch reachability.errorKind {
            case "network": String(localized: "No route to the node from this phone")
            case "tls", "auth": String(localized: "The node answered but refused this talosconfig")
            default: reachability.error
            }
        let seen = reachability.lastSeen.map { at in
            let ago = RelativeDateTimeFormatter().localizedString(
                for: Date(timeIntervalSince1970: Double(at) / 1000), relativeTo: .now)
            return String(localized: "Last seen \(ago)")
        }
        let parts = [reason, seen].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    // As in the node row's long press.
    private func copyAddress() {
        UIPasteboard.general.string = ref.address
    }

    /// With the app lock on, destructive actions need a fresh Face ID / passcode check.
    /// `talosctl reset`, after the app lock when it is on; the screen closes once requested.
    private func performReset(_ request: NodeResetSheet.Request) async {
        guard let client = model.client else { return }
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: String(localized: "Reset \(ref.hostname)"))
        {
            succeeded = false
            resultMessage = failure
            return
        }
        running = true
        defer { running = false }
        do {
            try await client.reset(
                node: ref.address, wipe: request.wipe, graceful: request.graceful, reboot: request.reboot)
            succeeded = true
            resultMessage = String(localized: "\(ref.hostname): reset requested")
        } catch {
            succeeded = false
            resultMessage = error.localizedDescription
        }
    }

    private func perform(_ request: PowerRequest) async {
        guard let client = model.client else { return }
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: "\(request.localizedTitle) \(ref.hostname)")
        {
            succeeded = false
            resultMessage = failure
            return
        }
        running = true
        defer { running = false }
        do {
            try await client.perform(request, node: ref.address)
            succeeded = true
            resultMessage = String(localized: "\(ref.hostname): \(request.localizedTitle) requested")
        } catch {
            succeeded = false
            resultMessage = error.localizedDescription
        }
    }
}

extension NodeDetailView {
    /// A maintenance of this node runs: it owns the node's cordon until it ends.
    private var maintenanceRunning: Bool {
        MaintenanceJob.shared.isActive && MaintenanceJob.shared.target?.node == ref.address
    }

    /// `kubectl cordon` / `uncordon`, after the app lock when it is on.
    private func cordon(_ on: Bool) async {
        guard let client = model.client else { return }
        let title = on ? String(localized: "Cordon \(ref.hostname)") : String(localized: "Uncordon \(ref.hostname)")
        if lockStore.lock.enabled, let failure = await Authenticator.authenticate(reason: title) {
            succeeded = false
            resultMessage = failure
            return
        }
        running = true
        defer { running = false }
        do {
            try await client.cordon(node: ref.address, on: on)
            resultMessage =
                on
                ? String(localized: "\(ref.hostname) is cordoned: no new pods are scheduled on it.")
                : String(localized: "\(ref.hostname) is uncordoned: pods can be scheduled on it again.")
        } catch {
            resultMessage = error.localizedDescription
        }
        // Not `succeeded`: that dismisses the screen (power actions); the node stays usable here.
        succeeded = false
    }
}
