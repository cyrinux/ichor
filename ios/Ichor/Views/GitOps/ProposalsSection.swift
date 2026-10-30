import SwiftUI
import IchorCore

/// The pull requests opened from the phone for `app` (the repository's for `.repository`): each
/// with its state on the forge and, once merged, whether the app runs it; `reconcile` (nil:
/// none) applies a merged one now. Read again when `refresh` changes. Nothing when there is none.
struct ProposalsSection: View {
    let app: GitOpsAppRef
    var refresh: String = ""
    var reconcile: (() -> Void)?

    @Environment(AppModel.self) private var model
    @Environment(ClusterGitStore.self) private var git
    @Environment(GitHostStore.self) private var hosts
    @Environment(\.openURL) private var openURL
    @State private var views: [String: ProposalView] = [:]

    /// What one row knows: the forge's answer and what the app runs, or why not.
    struct ProposalView: Equatable {
        var state: GitRequestState?
        var runs: RevisionRuns?
        var error: String?
    }

    private var records: [GitProposalRecord] {
        git.proposals(
            for: model.activeSummary, tool: app.tool, kind: app.kind, namespace: app.namespace, name: app.name)
    }

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        if !records.isEmpty {
            Section("Proposals") {
                ForEach(records) { record in
                    row(record, view: views[record.id])
                }
            }
            .task(id: "\(records.map(\.id))|\(refresh)") { await load() }
        }
    }

    /// The state the background monitor last read, when the forge cannot be read now: merged or
    /// closed only (an open one says nothing more than "Unknown").
    private func storedState(_ record: GitProposalRecord) -> GitRequestState? {
        record.state == "merged" || record.state == "closed" ? GitRequestState(state: record.state) : nil
    }

    /// "merged 2 hours ago" from the stored state.
    private func storedLine(_ record: GitProposalRecord) -> String {
        let at = Date(timeIntervalSince1970: TimeInterval(record.stateAt) / 1000)
        let ago = RelativeDateTimeFormatter().localizedString(for: at, relativeTo: .now)
        return record.state == "merged" ? String(localized: "merged \(ago)") : String(localized: "closed \(ago)")
    }

    @ViewBuilder
    private func row(_ record: GitProposalRecord, view: ProposalView?) -> some View {
        let stored = view?.state == nil ? storedState(record) : nil
        let status = proposalStatus(view?.state ?? stored, view?.runs)
        VStack(alignment: .leading, spacing: 4) {
            Button {
                if let url = URL(string: record.url), !record.url.isEmpty { openURL(url) }
            } label: {
                HStack {
                    HealthDot(health: status.health)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(
                            verbatim: view?.state?.title.isEmpty == false
                                ? view!.state!.title : (record.title.isEmpty ? record.path : record.title)
                        )
                        .lineLimit(1)
                        Text(verbatim: "#\(record.number)").font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Tag(status.label)
                }
            }
            .buttonStyle(.plain)
            if let error = view?.error { Text(verbatim: error).font(.caption).foregroundStyle(.secondary) }
            if stored != nil, record.stateAt > 0 {
                Text(verbatim: storedLine(record)).font(.caption).foregroundStyle(.secondary)
            }
            if let reconcile, status == .merged || status == .otherRevision {
                Text(
                    status == .merged
                        ? "Merged, not deployed yet."
                        : "Merged; the app runs another commit (a newer one, or this one is not applied yet)."
                )
                .font(.caption).foregroundStyle(.secondary)
                Button("Reconcile now", action: reconcile).buttonStyle(.bordered)
            }
        }
        .swipeActions {
            Button("Forget this proposal", role: .destructive) { git.forget(record, for: model.activeSummary) }
        }
        .contextMenu {
            Button("Forget this proposal", role: .destructive) { git.forget(record, for: model.activeSummary) }
        }
    }

    private func load() async {
        for record in records {
            do {
                let state = try await TalosClient.requestState(record, hosts: hosts, demo: demo)
                var runs: RevisionRuns?
                if state.merged, let client = model.client {
                    runs = try? await client.revisionRuns(record, state: state)
                }
                views[record.id] = ProposalView(state: state, runs: runs)
            } catch {
                views[record.id] = ProposalView(error: error.localizedDescription)
            }
        }
    }
}

private extension ProposalStatus {
    var label: String {
        switch self {
        case .open: String(localized: "Open")
        case .draft: String(localized: "Draft")
        case .closed: String(localized: "Closed")
        case .merged, .otherRevision: String(localized: "Merged")
        case .deployed: String(localized: "Deployed")
        case .unknown: String(localized: "Unknown")
        }
    }

    var health: ServiceHealth {
        switch self {
        case .deployed: .ok
        case .merged, .otherRevision: .warning
        case .open, .draft, .closed: .idle
        case .unknown: .unknown
        }
    }
}
