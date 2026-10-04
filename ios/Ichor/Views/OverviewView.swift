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
    /// Cluster members the talosconfig context misses (cluster discovery), offered to add.
    @State private var discovered: [DiscoveredNode] = []
    @State private var showDiscovered = false
    /// Show the nodes even though none answered (the outage notice replaces them otherwise).
    @State private var showNodesAnyway = false
    /// The apps card's data: loaded with the overview, not with every refresh of live data.
    @State private var inventory: LoadState<ClusterInventory> = .loading
    /// Longhorn, Garage, CloudNativePG: only asked when the inventory shows one of them and the
    /// role may use the Kubernetes API (nil hides the section); hints are their catalog ids.
    @State private var dataServices: LoadState<DataServices>?
    @State private var dataHints = ""
    /// Argo CD: only asked when the inventory shows it and the role may use the Kubernetes API.
    @State private var argo: LoadState<ArgoStatus>?
    /// Finding the public IPs Talos does not know (AppModel.detectPublicIPs).
    @State private var confirmDetectIPs = false
    @State private var detectIPsError: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            // No node answered (VPN off, another network): one notice instead of a list of red
            // nodes, unless they are known from before (then the list, under a banner).
            if let outage = overview.outage, !showNodesAnyway, !overview.showsLastKnown {
                ClusterUnreachableView(
                    outage: outage,
                    endpoints: model.activeSummary?.endpoints ?? [],
                    retry: load,
                    showNodes: { showNodesAnyway = true }
                )
                .themedBackground()
            } else {
                List {
                    if let outage = overview.outage, overview.showsLastKnown {
                        Section { LastKnownBanner(outage: outage, retry: load) }
                    }
                    if model.activeSummary?.demo == true {
                        Section {
                            Text("Demo cluster · Sample data. Cluster changes are unavailable. Remove the demo from Manage clusters when finished.")
                                .font(.callout).foregroundStyle(.secondary)
                        }
                    }
                    if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
                        Section { CertExpiryBanner(notAfter: ctx.certNotAfter) }
                    }
                    if !discovered.isEmpty {
                        Section { DiscoveredNodesBanner(count: discovered.count) { showDiscovered = true } }
                    }
                    if let update { TalosUpdateSection(info: update, nodes: overview.nodes) }
                    if support.visible { Section { SupportCard(prompt: support) } }
                    Section {
                        Summary(nodes: overview.nodes)
                        NavigationLink(value: Route.insights) { Label("Cluster insights", systemImage: "magnifyingglass") }
                    } header: {
                        if let access = model.activeSummary?.localizedAccessLabel { Text(access) }
                    }
                    AppsCard(state: inventory, hostnames: hostnames)
                    if let dataServices {
                        DataServicesSection(state: dataServices, hints: dataHints, apps: inventoryApps, downNodes: overview.downHostnames)
                    }
                    if let argo {
                        ArgoSection(state: argo, app: inventoryApps[argoCDCatalogID], downNodes: overview.downHostnames)
                    }
                    Section {
                        ForEach(sorted(overview.nodes)) { node in
                            let ref = NodeRef(address: node.node, hostname: node.hostname, role: node.role)
                            Group {
                                if node.reachable {
                                    NavigationLink(value: Route.node(ref)) { NodeRow(node: node, publicIPs: node.shownPublicIPs(probed: model.activePublicIPs)) }
                                } else {
                                    NodeRow(node: node, publicIPs: node.shownPublicIPs(probed: model.activePublicIPs))
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
                    } header: {
                        HStack {
                            Text("Nodes")
                            Spacer()
                            if model.isDetectingPublicIPs || canDetectIPs(overview) {
                                DetectPublicIPsButton(running: model.isDetectingPublicIPs) { confirmDetectIPs = true }
                            }
                            Text(verbatim: "\(overview.nodes.count)")
                        }
                    }
                    // Re-checked with every overview refresh (the load time is the task id).
                    TimeDriftSection(hostnames: hostnames, refreshID: loadedAt)
                }
                .refreshable { await load() }
                .themedBackground()
            }
        }
        .navigationTitle(model.activeLabel)
        // Shown by the title once it is inline (scrolled); the bar below is always there.
        .toolbarTitleMenu {
            // A submenu: with many clusters, the actions below stay in reach without a scroll.
            if (model.summary?.contexts.count ?? 0) > 1 {
                Menu {
                    ForEach(model.summary?.contexts ?? []) { context in
                        Button { model.activeContext = context.name } label: {
                            if context.name == model.activeContext {
                                Label(model.labels.of(context), systemImage: "checkmark")
                            } else {
                                Text(model.labels.of(context))
                            }
                        }
                    }
                } label: {
                    Label("Switch cluster", systemImage: "arrow.left.arrow.right")
                }
            }
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
                        .accessibilityLabel(Text("Cluster health"))
                }
                NavigationLink(value: Route.events(node: nil, hostnames: hostnames)) {
                    Image(systemName: "list.bullet.rectangle")
                }
                .accessibilityLabel(Text("Events"))
                // The Kubernetes API is reached with the admin kubeconfig Talos issues.
                if model.allows(.workloads) {
                    NavigationLink(value: Route.workloads) { Image(systemName: "square.stack.3d.up") }
                        .accessibilityLabel(Text("Kubernetes workloads"))
                    // PromQL panels, through the same kubeconfig (or a URL set on the screen).
                    NavigationLink(value: Route.metrics) { Image(systemName: "chart.xyaxis.line") }
                        .accessibilityLabel(Text("Metrics"))
                }
                NavigationLink(value: Route.kubespan) { Image(systemName: "point.3.connected.trianglepath.dotted") }
                    .accessibilityLabel(Text("KubeSpan"))
                NavigationLink(value: Route.etcd) { Image(systemName: "cylinder.split.1x2") }
                    .accessibilityLabel(Text(verbatim: "etcd"))
                NavigationLink(value: Route.settings) { Image(systemName: "gearshape") }
                    .accessibilityLabel(Text("Settings"))
            }
        }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        .alert("Find the public IPs?", isPresented: $confirmDetectIPs) {
            Button("Find") { detectPublicIPs() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(publicIPDetectionNotice)
        }
        .alert("Public IPs", isPresented: Binding(get: { detectIPsError != nil }, set: { if !$0 { detectIPsError = nil } })) {
            Button("OK") {}
        } message: {
            Text(verbatim: detectIPsError ?? "")
        }
        // Nothing answered: try again on a timer, not only on a pull to refresh.
        // Keyed on the cluster only: a retry that succeeds must not cancel its own load().
        .task(id: loadID) {
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(unreachableRetrySeconds))
                if Task.isCancelled { return }
                if outage != nil { await load() }
            }
        }
        // A new outage shows the notice again, even if the nodes were shown for the last one.
        .onChange(of: outage != nil) { _, down in
            if down { showNodesAnyway = false }
        }
        // A new network (VPN connected, back on Wi-Fi) is the likely fix: try again at once.
        .task {
            for await _ in NetworkChanges.stream() {
                if case .failed = state { await load() } else if outage != nil { await load() }
            }
        }
        .onChange(of: model.dataGeneration) {
            state = .loading
            discovered = []
            inventory = .loading
            dataServices = nil
            argo = nil
        }
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: model.activeContext) {
            state = .loading
            discovered = []
            inventory = .loading
            dataServices = nil
            argo = nil
        }
        .sheet(isPresented: $showDiscovered) {
            let context = model.activeContext
            DiscoveredNodesSheet(nodes: discovered) { nodes in
                try await model.addNodes(nodes.map(\.address))
            } notNow: {
                model.dismissNodes(discovered, of: context)
                discovered = discovered.filter { !model.dismissedNodes(of: context).contains($0.address) }
            }
        }
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
        guard case .loaded(let overview, _, _) = state else { return [:] }
        return Dictionary(overview.nodes.map { ($0.node, $0.hostname) }, uniquingKeysWith: { first, _ in first })
    }

    /// No node answered in the loaded overview.
    private var outage: ClusterOutage? {
        if case .loaded(let overview, _, _) = state { return overview.outage }
        return nil
    }

    private var loadedAt: Date? {
        if case .loaded(_, let at, _) = state { return at }
        return nil
    }

    /// Whether the public IPs Talos does not know can be asked from the internet now.
    private func canDetectIPs(_ overview: ClusterOverview) -> Bool {
        model.allows(.kubeconfig) && !model.privacyMask && overview.lacksPublicIPs
    }

    private func detectPublicIPs() {
        Task {
            do {
                if let error = try await model.detectPublicIPs()?.firstError, !error.isEmpty { detectIPsError = error }
            } catch {
                detectIPsError = error.localizedDescription
            }
        }
    }

    /// What the loaded overview belongs to: the context and the screenshot mode generation.
    private var loadID: String { "\(model.activeContext)#\(model.dataGeneration)" }

    private func load() async {
        guard let client = model.client else { return }
        // Nothing on screen: the last known overview (when kept) while this one loads.
        if case .loaded = state {} else { state = model.seeded(LoadState<ClusterOverview>.loading, from: .overview) }
        // The call is not cancelled with its task: a slow load of the previous context can
        // end after the new one's, and must not replace it.
        let id = loadID
        let target = model.lastKnownTarget
        let fetched: LoadState<ClusterOverview> = await .from { try await client.overview() }
        guard id == loadID else { return }
        let loaded = merged(fetched, for: target)
        state = state.refreshed(with: loaded)
        if case .loaded(let overview, _, _) = loaded {
            // Alongside the rest: listing every node's containers takes a while.
            Task { await loadInventory(with: client, id: id) }
            let info = model.activeSummary?.demo == true
                ? nil : await TalosUpdateChecker.refresh(nodeVersions: overview.nodes.filter(\.reachable).map(\.version))
            guard id == loadID else { return }
            update = info
            // What each node's Talos version can do, cached per version: gates menus and screens.
            await model.loadFeatures(of: overview.nodes)
            // Discovery asks the nodes too: pointless while none answers.
            if overview.outage == nil { await discover(with: client, id: id) }
        }
    }

    /// `fetched` with the nodes that stopped answering filled in from the overview on screen
    /// (see mergeLastKnown), kept as last known while a node answers: an outage leaves the
    /// stored one, with its time, as it is.
    private func merged(_ fetched: LoadState<ClusterOverview>, for target: LastKnownTarget?) -> LoadState<ClusterOverview> {
        guard case .loaded(let overview, let at, _) = fetched else { return fetched }
        var previous: ClusterOverview?
        var previousAt = at
        if case .loaded(let shown, let shownAt, _) = state {
            previous = shown
            previousAt = shownAt
        }
        let result = mergeLastKnown(current: overview, previous: previous, previousAt: previousAt)
        if let target, result.outage == nil, let json = try? JSONEncoder().encode(result) {
            model.remember(.overview, json: String(decoding: json, as: UTF8.self), at: at, for: target)
        }
        return .loaded(result, at: at)
    }

    /// Best effort: a first failure hides the apps card; a failed refresh keeps the apps shown.
    private func loadInventory(with client: TalosClient, id: String) async {
        inventory = model.seeded(inventory, from: .inventory)
        let loaded: LoadState<ClusterInventory> = await .from { try await model.fetch(.inventory, with: client) }
        guard id == loadID else { return }
        inventory = inventory.refreshed(with: loaded)
        if case .loaded(let apps, _, _) = loaded {
            // Alongside: each one only calls the Kubernetes API when the inventory shows it.
            Task { await loadArgo(with: client, id: id, inventory: apps) }
            await loadDataServices(with: client, id: id, inventory: apps)
        }
    }

    /// Only for clusters whose inventory shows Argo CD, and roles that may use the Kubernetes API.
    /// The answer is shared (ArgoCDStore) with the Argo CD screens and the Apps grid's badges.
    private func loadArgo(with client: TalosClient, id: String, inventory apps: ClusterInventory) async {
        guard model.allows(.workloads), argoCDHinted(apps) else {
            argo = nil
            return
        }
        if argo == nil { argo = .loading }
        let key = model.argoKey
        let loaded: LoadState<ArgoStatus> = await .from { try await ArgoCDStore.shared.load(with: client, key: key) }
        guard id == loadID else { return }
        argo = (argo ?? .loading).refreshed(with: loaded)
    }

    /// Only for clusters whose inventory shows Longhorn, Garage, CloudNativePG or Dragonfly, and roles
    /// that may use the Kubernetes API: others make no Kubernetes call. A failed refresh keeps what was
    /// shown (with its error noted).
    private func loadDataServices(with client: TalosClient, id: String, inventory apps: ClusterInventory) async {
        let hints = dataServiceHints(apps)
        guard model.allows(.workloads), !hints.isEmpty else {
            dataServices = nil
            return
        }
        dataHints = hints
        if dataServices == nil { dataServices = .loading }
        let loaded: LoadState<DataServices> = await .from { try await client.dataServices(hints: hints) }
        guard id == loadID else { return }
        dataServices = (dataServices ?? .loading).refreshed(with: loaded)
    }

    /// The inventory's apps by catalog id, for the data services' icons.
    private var inventoryApps: [String: InventoryApp] {
        guard case .loaded(let apps, _, _) = inventory else { return [:] }
        return Dictionary(apps.apps.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
    }

    /// Best effort: discovery off, or no node answering, offers nothing.
    private func discover(with client: TalosClient, id: String) async {
        let discovery = try? await client.discoverNodes()
        guard id == loadID else { return }
        discovered = discovery?.offer(dismissed: model.dismissedNodes(of: model.activeContext)) ?? []
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
    /// What Talos knows, else what a probe found (see shownPublicIPs).
    var publicIPs: [String] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(node.hostname).font(.headline)
                Spacer()
                StatusPill(label: node.health.label, color: node.health.color)
            }
            // Below the pill rather than beside it: the full width keeps an IPv6 on one line.
            VStack(alignment: .leading, spacing: 2) {
                AddressLine(symbol: "network", address: node.node)
                if node.reachable && !publicIPs.isEmpty {
                    VStack(alignment: .leading, spacing: 2) {
                        ForEach(publicIPs, id: \.self) { AddressLine(symbol: "globe", address: $0) }
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(Text("Public IP: \(publicIPs.joined(separator: ", "))"))
                }
            }
            if node.reachable {
                Text([role, node.version, node.stage, node.arch].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.caption)
            } else if let lastSeen = node.lastSeenDate {
                // Not answering, known from before: what it was, dimmed, and since when.
                Text([role, node.version, node.arch].filter { !$0.isEmpty }.joined(separator: "  ·  "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text("Last seen \(FreshnessFooter.ago(Date().timeIntervalSince(lastSeen)))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach(node.unmetConditions, id: \.self) {
                Text(verbatim: "\($0.name): \($0.reason)").font(.caption).foregroundStyle(.statusWarn)
            }
            if let error = node.error, !error.isEmpty {
                Text(error).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .padding(.vertical, 2)
    }

    private var role: String {
        node.role == "controlplane" ? String(localized: "control plane") : node.role
    }
}

/// An address behind a fixed-width symbol, so private and public ones line up.
private struct AddressLine: View {
    let symbol: String
    let address: String

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: symbol).frame(width: 14)
            Text(verbatim: address)
        }
        .font(.caption2.monospaced())
        .foregroundStyle(.secondary)
    }
}
