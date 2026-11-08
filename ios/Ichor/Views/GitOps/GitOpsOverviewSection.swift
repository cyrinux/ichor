import IchorCore
import SwiftUI

/// The overview row of a GitOps tool (Argo CD, Flux): a placeholder while loading, why it could
/// not be read, or `content` when the tool is installed; the row opens `route`.
struct GitOpsOverviewSection<Status, Placeholder: View, Content: View>: View {
    let title: String
    let route: Route
    /// The tool's own inventory app, for its icon.
    let app: InventoryApp?
    let state: LoadState<Status>
    /// Whether the status is worth a row at all.
    let installed: (Status) -> Bool
    @ViewBuilder let placeholder: () -> Placeholder
    @ViewBuilder let content: (Status) -> Content

    var body: some View {
        switch state {
        case .loading:
            Section {
                VStack(alignment: .leading, spacing: 10) {
                    GitOpsSectionHeader(app: app, title: title, count: 0)
                    placeholder()
                }
                .redacted(reason: .placeholder)
            }
        case .failed(let message):
            Section {
                NavigationLink(value: route) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: title).font(.headline)
                        Text("Could not read: \(message)").font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            }
        case .loaded(let status, _, _):
            if installed(status) {
                Section {
                    NavigationLink(value: route) {
                        VStack(alignment: .leading, spacing: 6) {
                            content(status)
                            UpdatesCountLine()
                        }
                    }
                }
            }
        }
    }
}

/// "Updates available: N", once the Updates screen looked the cluster on screen up in this session.
private struct UpdatesCountLine: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let outdated = UpdatesSession.shared.outdated(for: UpdatesSession.key(model)), outdated > 0 {
            Text("Updates available: \(outdated)").font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// The row's first line: the tool's icon, then the shared header: its name, how many apps it
/// manages and, when some need a look, the one pill saying how many.
struct GitOpsSectionHeader: View {
    let app: InventoryApp?
    let title: String
    let count: Int
    var attention = 0

    var body: some View {
        HStack(spacing: 10) {
            if let app {
                AppIconView(app: app, size: 28)
            } else {
                Image(systemName: "arrow.triangle.branch")
                    .frame(width: 28, height: 28)
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 8))
            }
            SectionHeader(title: title, subtitle: String(localized: "\(count) apps"), pill: pill)
                .font(.headline)
        }
    }

    @ViewBuilder private var pill: some View {
        if attention > 0 {
            StatusPill(label: String(localized: "\(attention) need a look"), color: attentionColor)
        }
    }
}
