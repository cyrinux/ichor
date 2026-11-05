import SwiftUI
import IchorCore

/// Nothing searched or opened yet: what typing finds, that Refresh is the only cluster call, and
/// a few of the active cluster's screens (Android's FirstUseState). Opening one is a navigation,
/// not a cluster call. The buttons wrap, so a large Dynamic Type size never clips them.
struct SearchFirstUseSection: View {
    /// The active cluster's catalog, already filtered by the role.
    let catalog: [ScreenSpec]
    let canRefresh: Bool
    let openRoute: (Route) -> Void

    var body: some View {
        let specs = Dictionary(catalog.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let shortcuts = firstUseShortcuts(catalog.map(\.entry)).compactMap { specs[$0.id] }
        Section {
            VStack(alignment: .leading, spacing: 10) {
                Label {
                    Text(
                        "Type to find the nodes, pods, workloads, CronJobs and GitOps apps your clusters last showed. Typing never calls a cluster."
                    )
                } icon: {
                    Image(systemName: "magnifyingglass")
                }
                if canRefresh {
                    Text("Refresh, top right, reads the active cluster again.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if !shortcuts.isEmpty {
                    ChipFlow(spacing: 8) {
                        ForEach(shortcuts) { spec in
                            Button {
                                openRoute(spec.route)
                            } label: {
                                Label(spec.title, systemImage: spec.systemImage).lineLimit(1)
                            }
                            .buttonStyle(.bordered)
                        }
                    }
                }
            }
            .padding(.vertical, 4)
        }
    }
}
