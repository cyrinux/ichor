import SwiftUI
import IchorCore

/// Clock offset of every node against its NTP server (os:reader), on the overview: orange
/// from 500 ms, red from 5 s (same thresholds as Android). Nodes that do not answer are
/// counted apart, not as out of sync. One summary line while every reachable node is in sync.
struct TimeDriftSection: View {
    /// Address → hostname, from the overview.
    let hostnames: [String: String]
    /// Changes when the overview reloads, to re-check the clocks with it.
    let refreshID: Date?

    // Explicit: the private @State properties make the memberwise init private.
    init(hostnames: [String: String], refreshID: Date?) {
        self.hostnames = hostnames
        self.refreshID = refreshID
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ClusterTimeInfo> = .loading
    @State private var refreshing = false
    @State private var expanded = false
    @State private var shownError: NodeTimeInfo?

    var body: some View {
        Section {
            switch state {
            case .loading:
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Checking clocks…").foregroundStyle(.secondary)
                }
            case .failed(let message):
                SectionError(message: message)
                Button("Retry") { Task { await load() } }
            case .loaded(let time, _, _):
                let summary = time.summary
                DisclosureGroup(isExpanded: $expanded) {
                    ForEach(time.nodes) { info in
                        if info.error == nil {
                            TimeOffsetRow(info: info, title: title(info))
                        } else {
                            Button { shownError = info } label: { UnreachableRow(title: title(info)) }
                                .foregroundStyle(.primary)
                        }
                    }
                } label: {
                    SummaryLine(summary: summary)
                }
            }
        } header: {
            HStack {
                Text("Clock drift")
                Spacer()
                if case .loaded(let time, _, _) = state, let status = time.summary.status {
                    StatusPill(label: label(status), color: status.color)
                        .textCase(nil)
                }
                Button { Task { await load() } } label: { Image(systemName: "arrow.clockwise") }
                    .disabled(refreshing)
                    .accessibilityLabel(Text("Refresh"))
            }
        } footer: {
            Text("Offset of each node's clock from its NTP server. Drift breaks etcd and certificate checks: warning from 500 ms, critical from 5 s.")
        }
        .task(id: refreshID) { await load() }
        .alert(shownError.map { title($0) } ?? "", isPresented: Binding(get: { shownError != nil }, set: { if !$0 { shownError = nil } })) {
            Button("OK") { shownError = nil }
        } message: {
            Text(shownError?.error ?? "")
        }
    }

    private func title(_ info: NodeTimeInfo) -> String { hostnames[info.node] ?? info.node }

    private func label(_ drift: TimeDrift) -> String {
        switch drift {
        case .ok: String(localized: "In sync")
        case .warning: String(localized: "Drifting")
        case .bad: String(localized: "Out of sync")
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        refreshing = true
        defer { refreshing = false }
        let before = loadedSummary?.expandedByDefault
        state = await .from { try await client.clusterTime() }
        // Follows the default when it changes (e.g. a node starts drifting), not on every
        // refresh, so a list the user opened stays open.
        if let after = loadedSummary?.expandedByDefault, after != before { expanded = after }
    }

    private var loadedSummary: TimeDriftSummary? {
        if case .loaded(let time, _, _) = state { return time.summary }
        return nil
    }
}

/// "7 nodes in sync · max ±9 ms", or how many drift, then "1 unreachable" in neutral grey.
private struct SummaryLine: View {
    let summary: TimeDriftSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            if summary.reachable > 0 {
                let offset = formatOffsetMagnitude(summary.maxOffsetMs)
                Text(summary.drifting == 0
                     ? String(localized: "\(summary.reachable) nodes in sync · max ±\(offset)")
                     : String(localized: "\(summary.drifting) nodes drifting · max ±\(offset)"))
            }
            if summary.unreachable > 0 {
                Text(String(localized: "\(summary.unreachable) unreachable"))
                    .font(summary.reachable > 0 ? Font.caption : Font.body)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

/// A node that did not answer: one short line, the full error on tap.
private struct UnreachableRow: View {
    let title: String

    var body: some View {
        HStack {
            Text(verbatim: title)
            Spacer()
            Text("unreachable").foregroundStyle(.secondary)
            Image(systemName: "info.circle").foregroundStyle(.secondary)
        }
    }
}
