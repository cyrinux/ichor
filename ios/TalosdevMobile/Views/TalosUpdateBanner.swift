import SwiftUI
import TalosdevMobileCore

/// "Talos v1.14.2 is available · 8 nodes on older versions" at the top of the overview.
/// Admins open the outdated nodes, each leading to its upgrade; other roles get the info only.
struct TalosUpdateSection: View {
    let info: TalosUpdateInfo
    let nodes: [NodeOverview]

    @Environment(AppModel.self) private var model

    /// Oldest first, then by hostname.
    private var outdated: [NodeOverview] {
        nodes.filter { $0.reachable && isOutdatedTalos($0.version, latest: info.latest) }
            .sorted { a, b in
                if a.version != b.version { return isTalosDowngrade(from: b.version, to: a.version) }
                return a.hostname < b.hostname
            }
    }

    private var canUpgrade: Bool { model.allows(.upgrade) && !outdated.isEmpty }

    var body: some View {
        if let count = talosUpdateBannerCount(info, localOutdated: outdated.count) {
            Section {
                if canUpgrade {
                    NavigationLink {
                        OutdatedNodesView(latest: info.latest, nodes: outdated)
                    } label: {
                        bannerLabel(count: count)
                    }
                } else {
                    bannerLabel(count: count)
                }
                if info.notes.hasPrefix("https://"), let url = URL(string: info.notes) {
                    Link(destination: url) {
                        Label("Release notes", systemImage: "doc.text")
                    }
                }
            }
        }
    }

    private func bannerLabel(count: Int) -> some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text("Talos \(info.latest) is available") + Text(verbatim: " · ") + Text("\(count) nodes on older versions")
                if canUpgrade {
                    Text("Tap to upgrade a node.").font(.caption).foregroundStyle(.secondary)
                }
            }
        } icon: {
            Image(systemName: "arrow.up.circle.fill")
        }
        .foregroundStyle(Color.blue)
    }
}

/// Nodes older than the latest release; each opens its upgrade with that version preselected.
private struct OutdatedNodesView: View {
    let latest: String
    let nodes: [NodeOverview]

    var body: some View {
        List {
            Section {
                ForEach(nodes) { node in
                    NavigationLink {
                        UpgradeView(node: node.node, hostname: node.hostname, initialVersion: latest)
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: node.hostname).font(.headline)
                            Text(verbatim: "\(node.version) · \(node.node)").font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                }
            } footer: {
                Text("Upgrade one node at a time, control-plane nodes first, and wait for each to be healthy again.")
            }
        }
        .themedBackground()
        .navigationTitle(String(localized: "Upgrade which node to \(latest)?"))
        .navigationBarTitleDisplayMode(.inline)
    }
}
