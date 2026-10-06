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
                    NavigationLink(value: route) { content(status) }
                }
            }
        }
    }
}

/// The row's first line: the tool's icon, its name and how many apps it manages.
struct GitOpsSectionHeader: View {
    let app: InventoryApp?
    let title: String
    let count: Int

    var body: some View {
        HStack(spacing: 10) {
            if let app {
                AppIconView(app: app, size: 28)
            } else {
                Image(systemName: "arrow.triangle.branch")
                    .frame(width: 28, height: 28)
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 8))
            }
            Text(verbatim: title).font(.headline)
            Spacer()
            Text("\(count) apps").font(.subheadline).foregroundStyle(.secondary).monospacedDigit()
        }
    }
}
