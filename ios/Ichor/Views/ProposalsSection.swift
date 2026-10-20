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
        model.proposals(tool: app.tool, kind: app.kind, namespace: app.namespace, name: app.name)
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

    @ViewBuilder
    private func row(_ record: GitProposalRecord, view: ProposalView?) -> some View {
        let status = proposalStatus(view?.state, view?.runs)
        VStack(alignment: .leading, spacing: 4) {
            Button {
                if let url = URL(string: record.url), !record.url.isEmpty { openURL(url) }
            } label: {
                HStack {
                    HealthDot(health: status.health)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: view?.state?.title.isEmpty == false ? view!.state!.title : (record.title.isEmpty ? record.path : record.title))
                            .lineLimit(1)
                        Text(verbatim: "#\(record.number)").font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Tag(status.label)
                }
            }
            .buttonStyle(.plain)
            if let error = view?.error { Text(verbatim: error).font(.caption).foregroundStyle(.secondary) }
            if let reconcile, status == .merged || status == .otherRevision {
                Text(status == .merged ? "Merged, not deployed yet." : "Merged; the app runs another commit (a newer one, or this one is not applied yet).")
                    .font(.caption).foregroundStyle(.secondary)
                Button("Reconcile now", action: reconcile).buttonStyle(.bordered)
            }
        }
        .swipeActions {
            Button("Forget this proposal", role: .destructive) { model.forgetProposal(record) }
        }
        .contextMenu {
            Button("Forget this proposal", role: .destructive) { model.forgetProposal(record) }
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
