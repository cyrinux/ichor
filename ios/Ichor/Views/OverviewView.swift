import SwiftUI
import IchorCore

struct OverviewView: View {
    /// The navigation path, so row swipes can open screens directly.
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @Environment(SupportPrompt.self) private var support
    @Environment(AISettings.self) private var ai
    @State private var state: LoadState<ClusterOverview> = .loading
    @State private var update: TalosUpdateInfo?
    /// Release notes to present after an app update.
    @State private var whatsNew: WhatsNewContent?
    @State private var openChangelog = false

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            List {
                if model.activeSummary?.demo == true {
                    Section {
                        Text("Demo cluster · Sample data. Cluster changes are unavailable. Remove the demo from Manage clusters when finished.")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                }
                if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
                    Section { CertExpiryBanner(notAfter: ctx.certNotAfter) }
                }
                if let update { TalosUpdateSection(info: update, nodes: overview.nodes) }
                if support.visible { Section { SupportCard(prompt: support) } }
                Section {
                    Summary(nodes: overview.nodes)
                } header: {
                    if let access = model.activeSummary?.localizedAccessLabel { Text(access) }
                }
                Section {
                    ForEach(sorted(overview.nodes)) { node in
                        let ref = NodeRef(address: node.node, hostname: node.hostname, role: node.role)
                        Group {
                            if node.reachable {
                                NavigationLink(value: Route.node(ref)) { NodeRow(node: node) }
                            } else {
                                NodeRow(node: node)
                            }
                        }
                        // Swipe right: live graphs. Swipe left: logs, shell, reboot (which only opens
                        // its confirmation). Long press: everything, plus Copy IP.
                        .swipeActions(edge: .leading) {
                            if node.reachable {
                                Button { path.append(.nodeLive(ref)) } label: { Label("Live", systemImage: "chart.xyaxis.line") }
                                    .tint(.blue)
                            }
                        }
                        .swipeActions(edge: .trailing) {
                            if node.reachable {
                                if model.allows(.power) {
                                    Button { path.append(.nodePower(ref, .reboot)) } label: { Label("Reboot", systemImage: "power") }
                                        .tint(.red)
                                }
                                if model.allows(.debugShell) {
                                    Button { path.append(.debugShell(node: node.node, hostname: node.hostname)) } label: {
                                        Label("Shell", systemImage: "apple.terminal")
                                    }
                                    .tint(.indigo)
                                }
                                Button { path.append(.logs(node: node.node, hostname: node.hostname, service: nil)) } label: {
                                    Label("Logs", systemImage: "text.alignleft")
                                }
                            }
                        }
                        .contextMenu {
                            if node.reachable {
                                Button { path.append(.nodeLive(ref)) } label: { Label("Live graphs", systemImage: "chart.xyaxis.line") }
                                Button { path.append(.node(ref)) } label: { Label("Services and logs", systemImage: "list.bullet") }
                                Button { path.append(.logs(node: node.node, hostname: node.hostname, service: nil)) } label: {
                                    Label("Kernel log", systemImage: "text.alignleft")
                                }
                                if model.allows(.debugShell) {
                                    Button { path.append(.debugShell(node: node.node, hostname: node.hostname)) } label: {
                                        Label("Debug shell", systemImage: "apple.terminal")
                                    }
                                }
                                if model.allows(.power) {
                                    Button(role: .destructive) { path.append(.nodePower(ref, .reboot)) } label: { Label("Reboot…", systemImage: "power") }
                                    Button(role: .destructive) { path.append(.nodePower(ref, .shutdown)) } label: { Label("Shut down…", systemImage: "power") }
                                }
                            }
                            Button { UIPasteboard.general.string = node.node } label: { Label("Copy IP", systemImage: "doc.on.doc") }
                        }
                    }
                }
                // Re-checked with every overview refresh (the load time is the task id).
                TimeDriftSection(hostnames: hostnames, refreshID: loadedAt)
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(model.activeLabel)
        // Shown by the title once it is inline (scrolled); the bar below is always there.
        .toolbarTitleMenu {
            ForEach(model.summary?.contexts ?? []) { context in
                Button { model.activeContext = context.name } label: {
                    if context.name == model.activeContext {
                        Label(model.labels.of(context), systemImage: "checkmark")
                    } else {
                        Text(model.labels.of(context))
                    }
                }
            }
            Divider()
            Button { path.append(.clusters) } label: { Label("Manage clusters…", systemImage: "square.stack.3d.up") }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if (model.summary?.contexts.count ?? 0) > 1 {
                ClusterBar { path.append(.clusters) }
            }
        }
        .toolbar {
            if model.privacyMask {
                ToolbarItem(placement: .topBarLeading) {
                    // A small icon rather than a label, so it stays out of the way in screenshots.
                    Image(systemName: "eye.slash")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(Text("Screenshot mode"))
                }
            }
            ToolbarItemGroup(placement: .primaryAction) {
                // Optional: only once turned on in the settings.
                if ai.enabled {
                    NavigationLink(value: Route.diagnosis(note: "")) { Image(systemName: "sparkles") }
                        .accessibilityLabel(Text("AI diagnosis"))
                }
                if model.allows(.health) {
                    NavigationLink(value: Route.health) { Image(systemName: "heart.text.square") }
                }
                NavigationLink(value: Route.events(node: nil, hostnames: hostnames)) {
                    Image(systemName: "list.bullet.rectangle")
                }
                .accessibilityLabel(Text("Events"))
                NavigationLink(value: Route.kubespan) { Image(systemName: "point.3.connected.trianglepath.dotted") }
                NavigationLink(value: Route.etcd) { Image(systemName: "cylinder.split.1x2") }
                NavigationLink(value: Route.settings) { Image(systemName: "gearshape") }
            }
        }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        .onChange(of: model.dataGeneration) { state = .loading }
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: model.activeContext) { state = .loading }
        // After an update (and the unlock: the overview is not shown before): what changed
        // since the build launched last time. The build is remembered once the notes are closed.
        .task {
            let releases = ChangelogStore.pendingWhatsNew()
            if !releases.isEmpty { whatsNew = WhatsNewContent(releases: releases) }
        }
        .sheet(item: $whatsNew, onDismiss: {
            ChangelogStore.storeCurrentBuild()
            if openChangelog {
                openChangelog = false
                path.append(.changelog)
            }
        }) { content in
            WhatsNewSheet(content: content) {
                openChangelog = true
                whatsNew = nil
            }
        }
    }

    /// Address → hostname of the loaded nodes, for the events timeline.
    private var hostnames: [String: String] {
        guard case .loaded(let overview, _) = state else { return [:] }
        return Dictionary(overview.nodes.map { ($0.node, $0.hostname) }, uniquingKeysWith: { first, _ in first })
    }

    private var loadedAt: Date? {
        if case .loaded(_, let at) = state { return at }
        return nil
    }

    /// What the loaded overview belongs to: the context and the screenshot mode generation.
    private var loadID: String { "\(model.activeContext)#\(model.dataGeneration)" }

    private func load() async {
        guard let client = model.client else { return }
        if case .loaded = state {} else { state = .loading }
        // The call is not cancelled with its task: a slow load of the previous context can
        // end after the new one's, and must not replace it.
        let id = loadID
        let loaded: LoadState<ClusterOverview> = await .from { try await client.overview() }
        guard id == loadID else { return }
        state = loaded
        if case .loaded(let overview, _) = loaded {
            let info = model.activeSummary?.demo == true
                ? nil : await TalosUpdateChecker.refresh(nodeVersions: overview.nodes.filter(\.reachable).map(\.version))
            guard id == loadID else { return }
            update = info
            // What each node's Talos version can do, cached per version: gates menus and screens.
            await model.loadFeatures(of: overview.nodes)
        }
    }

    private func sorted(_ nodes: [NodeOverview]) -> [NodeOverview] {
        // Control-plane nodes first, then by hostname.
        nodes.sorted { (rank($0), $0.hostname) < (rank($1), $1.hostname) }
    }

    private func rank(_ node: NodeOverview) -> Int {
        node.role == "controlplane" ? 0 : 1
    }
}

/// The client certificate expires within certWarnDays (or has expired): renew it (os:admin;
/// other roles get the role notice there).
private struct CertExpiryBanner: View {
    let notAfter: Int64

    var body: some View {
        let days = daysUntil(notAfter)
        NavigationLink(value: Route.issueConfig(renew: true)) {
            Label {
                Text(days < 0
                     ? String(localized: "The client certificate expired \(-days) days ago.")
                     : String(localized: "The client certificate expires in \(days) days. Generate a new talosconfig."))
            } icon: {
                Image(systemName: "exclamationmark.triangle.fill")
            }
            .foregroundStyle(days < 0 ? Color.red : Color.orange)
        }
    }
}

private struct Summary: View {
    let nodes: [NodeOverview]

    var body: some View {
        HStack(spacing: 24) {
            ForEach(NodeHealth.allCases, id: \.self) { health in
                let count = nodes.filter { $0.health == health }.count
                VStack(alignment: .leading) {
                    Text(verbatim: "\(count)").font(.title.bold()).foregroundStyle(count > 0 ? health.color : .secondary)
                    Text(health.label.lowercased()).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
}

private struct NodeRow: View {
    let node: NodeOverview

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                VStack(alignment: .leading) {
                    Text(node.hostname).font(.headline)
                    Text(node.node).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Spacer()
                StatusPill(label: node.health.label, color: node.health.color)
            }
            if node.reachable {
                Text([node.role == "controlplane" ? String(localized: "control plane") : node.role, node.version, node.stage, node.arch]
                    .filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.caption)
            }
            ForEach(node.unmetConditions, id: \.self) {
                Text(verbatim: "\($0.name): \($0.reason)").font(.caption).foregroundStyle(.orange)
            }
            if let error = node.error, !error.isEmpty {
                Text(error).font(.caption).foregroundStyle(.red)
            }
        }
        .padding(.vertical, 2)
    }
}
