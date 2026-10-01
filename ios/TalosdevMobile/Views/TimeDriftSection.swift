import SwiftUI
import TalosdevMobileCore

/// Clock offset of every node against its NTP server (os:reader), on the overview: orange
/// from 500 ms, red from 5 s or when a node does not answer (same thresholds as Android).
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
            case .loaded(let time, _):
                ForEach(time.nodes) { info in
                    TimeOffsetRow(info: info, title: hostnames[info.node] ?? info.node)
                }
            }
        } header: {
            HStack {
                Text("Clock drift")
                Spacer()
                if case .loaded(let time, _) = state {
                    StatusPill(label: label(time.worst), color: time.worst.color)
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
    }

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
        state = await .from { try await client.clusterTime() }
    }
}
