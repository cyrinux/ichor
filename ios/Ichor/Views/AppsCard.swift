import SwiftUI
import IchorCore

/// The overview's Apps row: how many run, what needs a look and a few icons; opens the Apps
/// screen. A skeleton while loading; nothing when the inventory failed or is empty, so it
/// never gets in the way of the overview. With `argo` loaded, an app whose Argo CD
/// Application is critical or drifting gets the Apps grid's badge. The "need a look" pill
/// opens the Apps screen on its Attention chip.
struct AppsCard: View {
    let state: LoadState<ClusterInventory>
    let hostnames: [String: String]
    var argo: ArgoStatus? = nil
    @Binding var path: [Route]

    @AppStorage(AppIconSettings.remoteKey) private var remoteIcons = false

    var body: some View {
        switch state {
        case .loading:
            Section { content(apps: Self.placeholder, containers: 0).redacted(reason: .placeholder) }
        case .failed:
            EmptyView()
        case .loaded(let inventory, _, _):
            if !inventory.apps.isEmpty {
                Section {
                    NavigationLink(value: Route.apps(hostnames: hostnames)) {
                        content(apps: inventory.apps, containers: inventory.containers)
                    }
                }
            }
        }
    }

    private func content(apps: [InventoryApp], containers: Int) -> some View {
        let running = apps.filter { !$0.system }.count
        let attention = apps.filter(\.needsAttention).count
        let tiles = overviewTiles(apps, limit: 6, remoteIcons: remoteIcons)
        return VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Apps").font(.headline)
                    Text(verbatim: [String(localized: "\(running) running"), String(localized: "\(containers) containers")]
                        .joined(separator: " · "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
                Spacer()
                if attention > 0 {
                    // Borderless: its own tap target inside the row's link.
                    Button { path.append(.apps(hostnames: hostnames, filter: .attention)) } label: {
                        StatusPill(label: String(localized: "\(attention) need a look"), color: attentionColor)
                    }
                    .buttonStyle(.borderless)
                }
            }
            HStack(spacing: 8) {
                ForEach(tiles.shown) { app in
                    AppIconView(app: app, size: 36)
                        .overlay(alignment: .bottomTrailing) {
                            if let argo, argoNeedsBadge(argoApps(for: app, in: argo)) { ArgoTileBadge().offset(x: 4, y: 4) }
                        }
                }
                if tiles.rest > 0 {
                    Text(verbatim: "+\(tiles.rest)")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                        .padding(.leading, 2)
                }
            }
        }
        .padding(.vertical, 4)
    }

    /// Shapes for the skeleton row.
    private static let placeholder = (0..<5).map { InventoryApp(id: "placeholder-\($0)", name: "App") }
}
