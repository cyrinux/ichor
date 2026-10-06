import SwiftUI
import IchorCore

/// "Talos v1.14.2 is available · 8 nodes on older versions" at the top of the overview.
/// Admins open the outdated nodes, each leading to its upgrade; other roles get the info only.
struct TalosUpdateSection: View {
    let info: TalosUpdateInfo
    let nodes: [NodeOverview]

    @Environment(AppModel.self) private var model

    private var choices: TalosUpgradeChoices { talosUpgradeChoices(nodes, latest: info.latest) }

    private var canUpgrade: Bool { model.allows(.upgrade) && choices.count > 0 }

    var body: some View {
        if let count = talosUpdateBannerCount(info, localOutdated: choices.count) {
            Section {
                if canUpgrade {
                    NavigationLink {
                        OutdatedNodesView(latest: info.latest, choices: choices)
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

/// Nodes older than the latest release under their role, control plane first; each opens its
/// upgrade with that version preselected. The workers stay tappable while a control-plane node is
/// outdated (the order is advice, and a new release is often tried on a worker first), only dimmed.
private struct OutdatedNodesView: View {
    let latest: String
    let choices: TalosUpgradeChoices

    var body: some View {
        List {
            if !choices.controlPlane.isEmpty {
                Section {
                    ForEach(choices.controlPlane) { row($0, dimmed: false) }
                } header: {
                    Text("Control plane")
                } footer: {
                    if choices.workers.isEmpty { hint }
                }
            }
            if !choices.workers.isEmpty {
                Section {
                    ForEach(choices.workers) { row($0, dimmed: choices.workersWait) }
                } header: {
                    if choices.workersWait { Text("Workers · after the control plane") } else { Text("Workers") }
                } footer: {
                    hint
                }
            }
        }
        .themedBackground()
        .navigationTitle(String(localized: "Upgrade which node to \(latest)?"))
        .navigationBarTitleDisplayMode(.inline)
    }

    private var hint: some View {
        Text("Upgrade one node at a time, control-plane nodes first, and wait for each to be healthy again.")
    }

    private func row(_ node: NodeOverview, dimmed: Bool) -> some View {
        NavigationLink {
            UpgradeView(node: node.node, hostname: node.hostname, initialVersion: latest)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: node.hostname).font(.headline).foregroundStyle(dimmed ? .secondary : .primary)
                Text(verbatim: "\(node.version) · \(node.node)").font(.caption.monospaced()).foregroundStyle(.secondary)
            }
        }
    }
}
