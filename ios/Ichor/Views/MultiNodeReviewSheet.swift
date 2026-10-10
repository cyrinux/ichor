import SwiftUI
import IchorCore

/// The same change on several nodes: pick them (this node already picked), see each node's
/// own diff, then apply to the nodes it would change, one after the other (os:admin).
struct MultiNodeReviewSheet: View {
    let node: String
    let edits: [ConfigEdit]
    /// The run is over and changed nodes: this node's config is to be read again.
    let onFinished: () -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var candidates: LoadState<[NodeOverview]> = .loading
    @State private var cluster = ""
    @State private var selected: Set<String> = []
    @State private var preview: LoadState<MultiConfigPreview>?
    @State private var confirming: ConfigApplyMode?
    @State private var applying: ConfigApplyMode?
    @State private var failed = false
    @State private var message: String?

    var body: some View {
        NavigationStack {
            Group {
                if let preview {
                    LoadStateView(state: preview, retry: loadPreview) { review($0) }
                } else {
                    LoadStateView(state: candidates, retry: loadNodes) { picker($0) }
                }
            }
            .navigationTitle("Other nodes")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        if preview == nil { dismiss() } else { preview = nil }
                    } label: {
                        preview == nil ? Text("Cancel") : Text("Back")
                    }
                }
                if preview == nil {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Preview") { Task { await loadPreview() } }
                            .disabled(selected.isEmpty || candidates.value == nil)
                    }
                }
            }
        }
        .task {
            selected = [node]
            await loadNodes()
        }
        .sheet(item: $confirming) { mode in
            let names = changing.map(\.name)
            // One node: its hostname is typed; several: the cluster's name, once for them all.
            HostnameConfirmationSheet(
                title: String(localized: "Apply the change to these nodes?"),
                message: confirmText(mode) + "\n\n" + names.joined(separator: ", "),
                hostname: names.count == 1 ? names[0] : (cluster.isEmpty ? node : cluster),
                actionTitle: modeTitle(mode)
            ) {
                confirming = nil
                Task { await start(mode) }
            }
        }
        .fullScreenCover(item: $applying, onDismiss: runClosed) { mode in
            MultiApplyView(nodes: changing.map(\.node), edits: edits, mode: mode) { didFail in
                if didFail { failed = true }
            }
        }
    }

    private var changing: [MultiConfigNodePreview] { preview?.value?.changing ?? [] }

    private func picker(_ nodes: [NodeOverview]) -> some View {
        List {
            let planes = nodes.filter { $0.role == "controlplane" }
            let workers = nodes.filter { $0.role != "controlplane" }
            if !planes.isEmpty { pickSection("Control planes", planes) }
            if !workers.isEmpty { pickSection("Workers", workers) }
        }
        .themedBackground()
    }

    private func pickSection(_ title: LocalizedStringKey, _ nodes: [NodeOverview]) -> some View {
        Section(title) {
            ForEach(nodes) { n in
                Button {
                    if selected.contains(n.node) { selected.remove(n.node) } else { selected.insert(n.node) }
                } label: {
                    HStack {
                        Image(systemName: selected.contains(n.node) ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(selected.contains(n.node) ? Color.accentColor : .secondary)
                        VStack(alignment: .leading) {
                            Text(verbatim: n.hostname.isEmpty ? n.node : n.hostname)
                            Text(verbatim: n.node).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                .tint(.primary)
                .accessibilityAddTraits(selected.contains(n.node) ? .isSelected : [])
            }
        }
    }

    private func review(_ preview: MultiConfigPreview) -> some View {
        List {
            ForEach(preview.nodes) { n in
                Section {
                    if let error = n.error {
                        Text("These edits do not fit this node's config, so it is skipped: \(error)").note()
                    } else if !n.changed {
                        Text("No changes").note()
                    } else {
                        DisclosureGroup("Changes") {
                            ConfigDiffView(lines: n.lines).listRowInsets(EdgeInsets())
                        }
                    }
                } header: {
                    HStack {
                        Text(verbatim: n.name)
                        Spacer()
                        if n.error != nil {
                            StatusPill(label: String(localized: "Skipped"), color: .statusBad)
                        } else if !n.changed {
                            StatusPill(label: String(localized: "No change"), color: .secondary)
                        } else if n.needsReboot {
                            StatusPill(label: String(localized: "Needs a reboot"), color: .statusWarn)
                        }
                    }
                }
            }
            if preview.changing.isEmpty {
                Section { Text("None of these nodes would change: the edits do not fit them, or they already have the change.").note() }
            } else {
                Section {
                    ForEach(preview.applyModes) { mode in
                        Button(modeTitle(mode)) { confirming = mode }
                    }
                    if let message { Text(verbatim: message).font(.footnote).foregroundStyle(.statusBad) }
                } footer: {
                    Text("Nodes changed: \(preview.changing.count) of \(preview.nodes.count). Applied one after the other, workers first, control planes last.")
                }
            }
        }
        .themedBackground()
    }

    private func confirmText(_ mode: ConfigApplyMode) -> String {
        switch mode {
        case .auto: String(localized: "The change is applied now and stays: nothing reverts it.")
        case .staged: String(localized: "The change is saved on the node and takes effect at its next reboot.")
        case .reboot: String(localized: "Each node applies the change and reboots, one after the other: the next one starts once the previous one is back.")
        }
    }

    private func loadNodes() async {
        guard let client = model.client else { return }
        candidates = await .from {
            let overview = try await client.overview()
            cluster = overview.context
            return overview.nodes.filter { $0.reachable || $0.node == node }
        }
    }

    private func loadPreview() async {
        guard let client = model.client, let nodes = candidates.value?.map(\.node).filter(selected.contains), !nodes.isEmpty else { return }
        preview = .loading
        preview = await .from { try await client.previewMachineConfigMulti(nodes: nodes, edits: edits) }
    }

    /// Like a single apply: a fresh Face ID / passcode first when the app lock is on.
    private func start(_ mode: ConfigApplyMode) async {
        message = nil
        if model.lock.enabled,
           let failure = await Authenticator.authenticate(reason: String(localized: "Apply the machine config of \(cluster)")) {
            message = failure
            return
        }
        applying = mode
    }

    /// A failed run leaves the draft as it is; one that went through ends the edit.
    private func runClosed() {
        if failed {
            failed = false
            dismiss()
        } else {
            onFinished()
        }
    }
}

/// A multi-node apply: every node's state as it goes, then how it ended. The rollout is
/// ConfigMultiJob's: it borrows background time, and a notification asks to come back.
struct MultiApplyView: View {
    let nodes: [String]
    let edits: [ConfigEdit]
    let mode: ConfigApplyMode
    let onOutcome: (_ failed: Bool) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var started = false
    /// Refused before starting (another rollout runs, or no client).
    @State private var refused: String?

    private var job: ConfigMultiJob { ConfigMultiJob.shared }
    private var progress: MultiConfigProgress? { job.progress }
    private var outcome: String?? { refused.map { .some($0) } ?? job.outcome }

    var body: some View {
        NavigationStack {
            List {
                if let progress, progress.total > 0 {
                    Section {
                        Text("Node \(min(progress.index + 1, progress.total)) of \(progress.total)").font(.headline)
                    }
                }
                Section {
                    ForEach(progress?.nodes ?? []) { n in row(n) }
                }
                if let outcome {
                    Section {
                        Label {
                            Text(verbatim: outcome.map { $0.isEmpty ? String(localized: "The config could not be applied.") : $0 }
                                 ?? String(localized: "Every node has the change."))
                        } icon: {
                            Image(systemName: outcome == nil ? "checkmark.seal.fill" : "xmark.octagon.fill")
                                .foregroundStyle(outcome == nil ? Color.statusOK : Color.statusBad)
                        }
                        Button("Done") {
                            job.clear()
                            dismiss()
                        }
                    }
                }
            }
            .themedBackground()
            .navigationTitle(modeTitle(mode))
            .navigationBarTitleDisplayMode(.inline)
        }
        .interactiveDismissDisabled(outcome == nil)
        .task { start() }
        .onChange(of: job.isActive) { _, active in
            if !active, case .some(let error) = job.outcome { onOutcome(error != nil) }
        }
    }

    private func row(_ n: MultiConfigNodeState) -> some View {
        let running = outcome == nil && n.nodeState == .applying
        let detail = running ? (progress?.message ?? "") : (n.error ?? "")
        return HStack(spacing: 12) {
            if running {
                ProgressView()
            } else {
                Image(systemName: icon(n.nodeState)).foregroundStyle(tint(n.nodeState))
            }
            VStack(alignment: .leading) {
                Text(verbatim: n.name)
                Text(verbatim: [stateText(n.nodeState), detail].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func icon(_ state: MultiConfigNodeState.State) -> String {
        switch state {
        case .done: "checkmark.circle.fill"
        case .failed: "xmark.octagon.fill"
        case .skipped: "nosign"
        case .unchanged: "minus.circle"
        case .pending, .applying: "circle"
        }
    }

    private func tint(_ state: MultiConfigNodeState.State) -> Color {
        switch state {
        case .done: .statusOK
        case .failed: .statusBad
        case .skipped: .statusWarn
        case .unchanged, .pending, .applying: .secondary
        }
    }

    private func stateText(_ state: MultiConfigNodeState.State) -> String {
        switch state {
        case .pending: String(localized: "Waiting")
        case .applying: String(localized: "Applying")
        case .done: String(localized: "Done")
        case .failed: String(localized: "Failed")
        case .skipped: String(localized: "Skipped")
        case .unchanged: String(localized: "No change")
        }
    }

    /// Starts the rollout in ConfigMultiJob, unless one runs already.
    private func start() {
        guard !started else { return }
        started = true
        guard !job.isActive else {
            refused = String(localized: "A config change is already being applied on several nodes: wait for it to end.")
            onOutcome(true)
            return
        }
        guard let client = model.client else {
            refused = String(localized: "The config could not be applied.")
            onOutcome(true)
            return
        }
        job.clear()
        job.start(client: client, nodes: nodes, edits: edits, mode: mode)
    }
}

private extension LoadState {
    /// The data once loaded, else nil.
    var value: T? {
        if case .loaded(let value, _, _) = self { return value }
        return nil
    }
}
