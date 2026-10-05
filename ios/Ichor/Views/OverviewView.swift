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
    /// Flux: likewise, only when the inventory shows it.
    @State private var flux: LoadState<FluxStatus>?
    /// Finding the public IPs Talos does not know (AppModel.detectPublicIPs).
    @State private var confirmDetectIPs = false
    @State private var detectIPsError: String?
    /// The nodes section with a full row per node; collapsed (a chip each) by default.
    @AppStorage("overview.nodesExpanded") private var nodesExpanded = false
    /// Live CPU and memory on the summary (Settings), sampled only while this screen is on
    /// screen and the app active.
    @AppStorage(LiveStatsSettings.key) private var liveStats = true
    /// The sections' order and those hidden, the toolbar's icons and menu: one arrangement for
    /// every cluster, changed in OverviewEditorSheet (same saved form as Android).
    @AppStorage(OverviewLayout.storageKey) private var layoutText = ""
    @AppStorage(OverviewBar.storageKey) private var barText = ""
    @State private var customizing = false
    @Environment(\.scenePhase) private var scenePhase
    @State private var visible = false

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
                    // The sections as arranged (Customize overview, in the ⋯ menu).
                    ForEach(layout.visible) { card in section(card, overview: overview) }
                    if layout.visible.isEmpty {
                        Section {
                            Button { customizing = true } label: {
                                Text("Every card is hidden. Tap to choose the ones to show.").foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                .refreshable {
                    await load()
                    // Sites rarely change: the map is only asked again on a pull.
                    if let client = model.client, outage == nil {
                        await TopologyStore.shared.load(with: client, key: model.topologyKey, force: true)
                    }
                }
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
                // Buttons with labels, not NavigationLinks with images: on a phone the ones that
                // don't fit fold into the system's "…" menu, where a link does nothing and an image
                // shows no title.
                // Optional: only once turned on in the settings.
                if ai.enabled {
                    Button { path.append(Route.diagnosis(note: "")) } label: { Label("AI diagnosis", systemImage: "sparkles") }
                }
                // As arranged: the bar's icons, the rest behind ⋯ (with the arrangement itself).
                ForEach(bar.icons.filter(offered)) { action in
                    Button { path.append(route(action)) } label: {
                        Label { action.title } icon: { Image(systemName: action.systemImage) }
                    }
                    .disabled(!enabled(action))
                }
                Menu {
                    let menu = bar.menu.filter(offered)
                    ForEach(menu) { action in
                        Button { path.append(route(action)) } label: {
                            Label { action.title } icon: { Image(systemName: action.systemImage) }
                        }
                        .disabled(!enabled(action))
                    }
                    if !menu.isEmpty { Divider() }
                    Button { customizing = true } label: { Label("Customize overview", systemImage: "pencil") }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .accessibilityLabel(Text("More"))
            }
        }
        .sheet(isPresented: $customizing) { OverviewEditorSheet() }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        // Live CPU and memory while the overview is on screen, the app active, the setting on
        // and a node answers; fewer samples on a large cluster (each one asks every node).
        .task(id: liveID) {
            guard liveStats else {
                ClusterLiveStore.shared.clear()
                return
            }
            guard visible, scenePhase == .active, case .loaded(let overview, _, _) = state, overview.outage == nil,
                  let client = model.client else { return }
            await ClusterLiveStore.shared.poll(with: client, key: loadID, nodes: overview.nodes.count)
        }
        .onAppear { visible = true }
        .onDisappear { visible = false }
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
            flux = nil
        }
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: model.activeContext) {
            state = .loading
            discovered = []
            inventory = .loading
            dataServices = nil
            argo = nil
            flux = nil
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

    /// One of the overview's arranged sections; those that report on something the cluster does
    /// not have stay out (Android's OverviewCard.whenDetected).
    @ViewBuilder
    private func section(_ card: OverviewCard, overview: ClusterOverview) -> some View {
        switch card {
        case .summary:
            Section {
                ClusterSummaryCard(
                    name: model.activeLabel,
                    summary: ClusterSummary(nodes: overview.nodes),
                    live: liveStats ? ClusterLiveStore.shared.live(for: loadID) : nil
                )
                NavigationLink(value: Route.insights) { Label("Cluster insights", systemImage: "magnifyingglass") }
            } header: {
                if let access = model.activeSummary?.localizedAccessLabel { Text(access) }
            }
        case .apps:
            AppsCard(state: inventory, hostnames: hostnames, argo: argoStatus)
        case .dataServices:
            if let dataServices {
                DataServicesSection(state: dataServices, hints: dataHints, apps: inventoryApps, downNodes: overview.downHostnames)
            }
        case .argoCD:
            if let argo {
                ArgoSection(state: argo, app: inventoryApps[argoCDCatalogID], downNodes: overview.downHostnames)
            }
        case .flux:
            if let flux {
                FluxSection(state: flux, app: inventoryApps[fluxCatalogID], downNodes: overview.downHostnames)
            }
        case .nodes:
            nodesSection(overview)
        case .timeDrift:
            // Re-checked with every overview refresh (the load time is the task id).
            TimeDriftSection(hostnames: hostnames, refreshID: loadedAt)
        }
    }

    private func nodesSection(_ overview: ClusterOverview) -> some View {
        Section {
            // In the map's order, site by site, once the cluster map is loaded.
            let groups = groupNodes(overview.nodes, by: TopologyStore.shared.topology(for: model.topologyKey))
            let version = overview.nodes.sharedVersion
            if isDenseCluster(overview.nodes.count) {
                // Too many for a chip each: counts, dots, and the problems capped.
                DenseNodes(groups: groups, path: $path)
            } else {
                ForEach(groups) { group in
                    // Site headers only when there is more than one: a single site says nothing.
                    if groups.count > 1 { SiteHeader(title: group.title) }
                    NodeGroupRows(nodes: group.nodes, expanded: nodesExpanded, sharedVersion: version, path: $path)
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
                if isDenseCluster(overview.nodes.count) {
                    // A section holding hundreds of rows defeats the overview: a screen instead.
                    Button { path.append(.nodes(filter: nil, nodes: overview.nodes.overviewOrder)) } label: {
                        Image(systemName: "chevron.right")
                    }
                    .accessibilityLabel(Text("Show all nodes"))
                } else {
                    Button { withAnimation { nodesExpanded.toggle() } } label: {
                        Image(systemName: nodesExpanded ? "chevron.up" : "chevron.down")
                    }
                    .accessibilityLabel(nodesExpanded ? Text("Show less") : Text("Show all"))
                }
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

    /// Restarts (or stops) the live sampling when one of its conditions changes.
    private var liveID: String {
        var nodes = -1
        if case .loaded(let overview, _, _) = state, overview.outage == nil { nodes = overview.nodes.count }
        return "\(loadID)|\(liveStats)|\(visible)|\(scenePhase == .active)|\(nodes)"
    }

    /// A cluster-wide screen is only disabled when no reachable node's Talos has it (unknown
    /// features count as supported). Same as Android's OverviewActions.
    private func clusterWide(_ feature: NodeFeature) -> Bool {
        guard case .loaded(let overview, _, _) = state else { return true }
        let known = overview.nodes.filter(\.reachable).compactMap { model.nodeFeatures[$0.node] }
        return clusterSupport(known, feature).supported
    }

    private var layout: OverviewLayout { .parse(layoutText) }
    private var bar: OverviewBar { .parse(barText) }

    /// Only offered when the config's role can run it. Workloads and the PromQL panels reach the
    /// Kubernetes API with the admin kubeconfig Talos issues.
    private func offered(_ action: OverviewAction) -> Bool {
        switch action {
        case .health: model.allows(.health)
        case .workloads, .metrics: model.allows(.workloads)
        default: true
        }
    }

    /// Cluster-wide screens: only disabled when no reachable node's Talos has them.
    private func enabled(_ action: OverviewAction) -> Bool {
        switch action {
        case .events: clusterWide(.events)
        case .kubespan: clusterWide(.kubespan)
        case .etcd: clusterWide(.etcd)
        default: true
        }
    }

    private func route(_ action: OverviewAction) -> Route {
        switch action {
        case .health: .health
        case .events: .events(node: nil, hostnames: hostnames)
        case .workloads: .workloads
        case .metrics: .metrics
        case .kubespan: .kubespan
        case .etcd: .etcd
        case .settings: .settings
        }
    }

    /// The Argo CD status the Argo CD section loaded, for the Apps card's badges.
    private var argoStatus: ArgoStatus? {
        if case .loaded(let status, _, _)? = argo { return status }
        return nil
    }

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
            // The map that groups the nodes by site: once per cluster (see TopologyStore).
            if overview.outage == nil {
                let key = model.topologyKey
                Task { await TopologyStore.shared.load(with: client, key: key) }
            }
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
            Task { await loadFlux(with: client, id: id, inventory: apps) }
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

    /// Only for clusters whose inventory shows Flux, and roles that may use the Kubernetes API.
    /// The answer is shared (FluxStore) with the Flux screens and the Flux tile's sheet.
    private func loadFlux(with client: TalosClient, id: String, inventory apps: ClusterInventory) async {
        guard model.allows(.workloads), fluxHinted(apps) else {
            flux = nil
            return
        }
        if flux == nil { flux = .loading }
        let key = model.fluxKey
        let loaded: LoadState<FluxStatus> = await .from { try await FluxStore.shared.load(with: client, key: key) }
        guard id == loadID else { return }
        flux = (flux ?? .loading).refreshed(with: loaded)
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

/// One site's nodes in the overview's nodes section. Collapsed (the default): a chip per calm
/// node, a full row only for those needing attention, so a problem never hides behind the fold.
private struct NodeGroupRows: View {
    let nodes: [NodeOverview]
    let expanded: Bool
    let sharedVersion: String?
    @Binding var path: [Route]

    var body: some View {
        let calm = expanded ? [] : nodes.filter { !$0.needsAttention }
        if !calm.isEmpty {
            ChipFlow(spacing: 8) {
                ForEach(calm) { node in
                    NodeChip(node: node) { path.append(.node(node.ref)) }
                        .contextMenu { NodeMenu(node: node, path: $path) }
                }
            }
            .buttonStyle(.borderless)
            .padding(.vertical, 4)
        }
        ForEach(expanded ? nodes : nodes.filter(\.needsAttention)) { node in
            NodeListRow(node: node, sharedVersion: sharedVersion, path: $path)
        }
    }
}

/// A calm node in the collapsed nodes section: its status dot and hostname; tap opens it.
private struct NodeChip: View {
    let node: NodeOverview
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            HStack(spacing: 6) {
                Circle().fill(node.health.color).frame(width: 8, height: 8)
                Text(verbatim: node.hostname).font(.subheadline).lineLimit(1)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .background(Color.secondary.opacity(0.12), in: Capsule())
        }
        .foregroundStyle(.primary)
        .accessibilityLabel(Text(verbatim: "\(node.hostname), \(node.health.label)"))
    }
}
