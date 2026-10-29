import SwiftUI
import IchorCore

/// What an app waits for, indented by depth with each link's state (a missing one says so),
/// then the apps that wait for it; a listed app opens its own detail.
struct FluxDependsOnTree: View {
    let tree: FluxDependencyTree
    let downNodes: Set<String>

    var body: some View {
        if !tree.upstream.isEmpty {
            Section("Depends on") {
                ForEach(tree.upstream) { node in
                    row(key: node.key, app: node.app, indent: node.depth - 1)
                }
            }
        }
        if !tree.dependents.isEmpty {
            Section(String(localized: "Needed by (\(String(tree.dependents.count)))")) {
                ForEach(tree.dependents) { app in
                    row(key: "\(app.namespace)/\(app.name)", app: app, indent: 0)
                }
            }
        }
    }

    @ViewBuilder
    private func row(key: String, app: FluxApp?, indent: Int) -> some View {
        if let app {
            NavigationLink {
                FluxAppView(kind: app.kind, namespace: app.namespace, name: app.name, downNodes: downNodes)
            } label: {
                label(key: key, app: app, indent: indent)
            }
        } else {
            label(key: key, app: nil, indent: indent)
        }
    }

    private func label(key: String, app: FluxApp?, indent: Int) -> some View {
        HStack(spacing: 10) {
            if let app {
                FluxGlyph(state: app.state, font: .caption)
            } else {
                Image(systemName: "questionmark.circle").font(.caption).foregroundStyle(.secondary)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: key).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                if app == nil {
                    Text("Not found in this cluster").font(.caption).foregroundStyle(.secondary)
                } else if let reason = app?.reason, !reason.isEmpty {
                    Text(verbatim: reason).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        }
        .padding(.leading, CGFloat(indent) * 16)
    }
}

/// A watched HelmRelease's drift: none, or when it was found and what changed. In warn mode
/// Flux only reports it.
struct FluxDriftRow: View {
    let app: FluxApp

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if app.drifted {
                Label(
                    String(localized: "Drift detected · \(relativeTime(app.driftAt))"),
                    systemImage: "arrow.left.arrow.right"
                )
                .foregroundStyle(.statusWarn)
                if !app.driftMessage.isEmpty {
                    Text(verbatim: app.driftMessage).font(.caption.monospaced()).textSelection(.enabled)
                }
            } else {
                Label("Drift: none", systemImage: "arrow.left.arrow.right").foregroundStyle(.secondary)
            }
            if app.driftMode == fluxDriftWarn {
                Text("Flux only reports drift for this release; correcting it runs a forced upgrade.").font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }
}
