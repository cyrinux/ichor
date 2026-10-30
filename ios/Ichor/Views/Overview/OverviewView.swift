import SwiftUI
import IchorCore

struct OverviewView: View {
    /// The navigation path, so row swipes can open screens directly.
    @Binding var path: [Route]

    @Environment(AppModel.self) var model
    @Environment(LockStore.self) private var lockStore
    @Environment(SettingsStore.self) var settings
    @Environment(AISettings.self) private var ai
    @State private var state: LoadState<ClusterOverview> = .loading
    /// `state` is only what the last background check saw (a seed), until the cluster answers.
    @State private var fromMonitor = false
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
    @State var dataServices: LoadState<DataServices>?
    @State var dataHints = ""
    /// The active CRD plugins of Data services (kube-green...), asked with the same hints; their
    /// lines join the card. Empty until they answer, or when they cannot be asked.
    @State var dataPlugins: [PluginDetectItem] = []
    /// The other active CRD plugins, tools of the Kubernetes screens section.
    @State var toolPlugins: [PluginDetectItem] = []
    /// The installed plugins (no cluster call, see PluginCatalogCache): those of Data services
    /// (Rook, MariaDB...) hint the card too.
    @State var pluginCatalog = PluginCatalog()
    /// Argo CD: only asked when the inventory shows it and the role may use the Kubernetes API.
    @State var argo: LoadState<ArgoStatus>?
    /// Flux: likewise, only when the inventory shows it.
    @State var flux: LoadState<FluxStatus>?
    /// The Alertmanager's alerts: only asked when the role may use the Kubernetes API; nil until
    /// one is found (none hides the section).
    @State var alerts: LoadState<AMAlerts>?
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
    /// The incident summary sheet (CYR-77), from the title menu.
    @State private var sharingIncident = false
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var visible = false
    /// "Since you last looked" (the history ring).
    @State private var look = HistoryLook()
    /// The demo tour's first coach mark (inline, see DemoTourSwitcherTip) shows now.
    @State private var tourShowsSwitcher = false

    var body: some View {
        content
            .navigationTitle(model.activeLabel)
            // Shown by the title once it is inline (scrolled); before that, under ⋯ → Cluster.
            .toolbarTitleMenu { ClusterTitleMenu(path: $path, shareIncident: { sharingIncident = true }) }
            .safeAreaInset(edge: .top, spacing: 0) {
                if (model.summary?.contexts.count ?? 0) > 1 {
                    ClusterBar { path.append(.clusters) }
                }
            }
            .toolbar { overviewToolbar }
            .sheet(isPresented: $customizing) {
                OverviewEditorSheet(
                    OverviewCard.self, OverviewAction.self, title: "Customize overview", absent: absentCards)
            }
            .sheet(isPresented: $sharingIncident) {
                if let context = model.activeSummary {
                    IncidentSummarySheet(context: context.name, cluster: monitorClusterKey(context))
                }
            }
            // Reloads with the screenshot mode too, dropping what was loaded with the old names.
            .task(id: loadID) { await load() }
            .watchesHistory(look, cluster: model.activeSummary.map(monitorClusterKey) ?? "", id: loadID)
            // Live CPU and memory while the overview is on screen, the app active, the setting on
            // and a node answers; fewer samples on a large cluster, most of them of the focus nodes only.
            .task(id: liveID) {
                guard liveStats else {
                    ClusterLiveStore.shared.clear()
                    return
                }
                guard visible, scenePhase == .active, !fromMonitor, case .loaded(let overview, _, _) = state,
                    !overview.partial,
                    overview.outage == nil, let client = model.client
                else { return }
                // Above the dense threshold most samples ask only the problem nodes and the rows on screen.
                await ClusterLiveStore.shared.poll(
                    with: client, key: loadID, nodes: overview.nodes.count, focus: liveFocus(overview))
            }
            .onAppear { visible = true }
            .onDisappear { visible = false }
            // The demo tour: on to the next coach mark as each one is closed (DemoTour.follow).
            .task(id: tourID) {
                guard tourActive, let available = tourAvailable else { return }
                await DemoTour.follow(available: available)
            }
            .task(id: tourID) {
                guard tourActive else { return }
                await DemoTour.watchSwitcher { tourShowsSwitcher = $0 }
            }
            .alert("Find the public IPs?", isPresented: $confirmDetectIPs) {
                Button("Find") { detectPublicIPs() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(publicIPDetectionNotice)
            }
            .alert("Public IPs", isPresented: $detectIPsError.isPresent()) {
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
                fromMonitor = false
                discovered = []
                inventory = .loading
                dataServices = nil
                dataPlugins = []
                toolPlugins = []
                argo = nil
                flux = nil
                alerts = nil
            }
            // The Talos update section shown again: its check was not made while hidden.
            .onChange(of: layout.hidden.contains(.talosUpdate)) { _, hidden in
                guard !hidden, case .loaded(let overview, _, _) = state else { return }
                Task { await checkTalosUpdate(overview, id: loadID) }
            }
            // Another cluster: never its name over the previous one's nodes.
            .onChange(of: model.activeContext) {
                state = .loading
                fromMonitor = false
                discovered = []
                inventory = .loading
                dataServices = nil
                dataPlugins = []
                toolPlugins = []
                argo = nil
                flux = nil
                alerts = nil
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
            .sheet(
                item: $whatsNew,
                onDismiss: {
                    ChangelogStore.storeCurrentBuild()
                    if openChangelog {
                        openChangelog = false
                        path.append(.changelog)
                    }
                }
            ) { content in
                WhatsNewSheet(content: content) {
                    openChangelog = true
                    whatsNew = nil
                }
            }
    }

    /// The nodes and cards, or why there are none: no endpoint yet, or no node answering.
    private var content: some View {
        Group {
            // A talosconfig generated before the cluster had addresses: nothing to load yet.
            if let context = model.activeSummary, context.needsEndpoint {
                NoEndpointsView(context: context).themedBackground()
            } else {
                LoadStateView(state: state, retry: load, fromMonitor: fromMonitor) { overview in
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
                        // Two columns on a wide iPad (OverviewColumns); a narrow Split View keeps one.
                        GeometryReader { geometry in
                            let columns = OverviewColumns.count(
                                width: geometry.size.width, regularPad: ListDetailLayout.isSplit(sizeClass))
                            Group {
                                if columns == 2 {
                                    let split = OverviewColumns.split(layout.visible)
                                    HStack(alignment: .top, spacing: 0) {
                                        overviewList(split.leading, overview: overview, leading: true)
                                        overviewList(split.trailing, overview: overview, leading: false)
                                    }
                                } else {
                                    overviewList(layout.visible, overview: overview, leading: true)
                                }
                            }
                            .environment(\.availableWidth, geometry.size.width)
                        }
                    }
                }
            }
        }
    }

    /// One column of the overview: `cards` as arranged; the `leading` (or only) one also holds
    /// the banners and the notice when every card is hidden.
    private func overviewList(_ cards: [OverviewCard], overview: ClusterOverview, leading: Bool) -> some View {
        List {
            if leading {
                OverviewBannerSections(
                    lastKnownOutage: overview.showsLastKnown ? overview.outage : nil,
                    retry: load,
                    showsSwitcherTip: tourActive && tourShowsSwitcher,
                    look: look,
                    discovered: discovered.count,
                    showDiscovered: { showDiscovered = true }
                )
            }
            // The sections as arranged (Customize overview, in the ⋯ menu).
            ForEach(cards) { card in section(card, overview: overview) }
            // The Talos update section alone, with no release to offer, leaves the list as empty
            // (the Pinned one too, without pins).
            if leading,
                layout.visible.allSatisfy({ $0 == .talosUpdate && !offersTalosUpdate || $0 == .pinned && pins.isEmpty })
            {
                Section {
                    Button {
                        customizing = true
                    } label: {
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

    /// The screenshot-mode sign, AI diagnosis, the bar's icons as arranged and the ⋯ menu.
    @ToolbarContentBuilder
    private var overviewToolbar: some ToolbarContent {
        if settings.privacyMask {
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
                Button {
                    path.append(Route.diagnosis(note: ""))
                } label: {
                    Label("AI diagnosis", systemImage: "sparkles")
                }
            }
            // As arranged: the bar's icons, the rest behind ⋯ (with the arrangement itself).
            ForEach(barIcons) { action in
                Button {
                    path.append(route(action))
                } label: {
                    Label {
                        action.title
                    } icon: {
                        Image(systemName: action.systemImage)
                    }
                }
                .disabled(!enabled(action))
                .smokeID(action == .settings ? SmokeID.settings : nil)
                .demoTourTip(
                    action == .settings ? .settings : .actions,
                    active: tourActive && (action == .settings || action == tourActionsAnchor))
            }
            Menu {
                let menu = bar.menu.filter(offered)
                ForEach(menu) { action in
                    Button {
                        path.append(route(action))
                    } label: {
                        Label {
                            action.title
                        } icon: {
                            Image(systemName: action.systemImage)
                        }
                    }
                    .disabled(!enabled(action))
                    .smokeID(action == .settings ? SmokeID.settings : nil)
                }
                if !menu.isEmpty { Divider() }
                ClusterSubmenu(path: $path, shareIncident: { sharingIncident = true })
                Button {
                    customizing = true
                } label: {
                    Label("Customize overview", systemImage: "pencil")
                }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .accessibilityLabel(Text("More"))
            // Settings sits in this menu unless it was made an icon.
            .demoTourTip(.settings, active: tourActive && !barIcons.contains(.settings))
        }
    }

    /// One of the overview's arranged sections; those that report on something the cluster does
    /// not have stay out (Android's OverviewCard.whenDetected).
    @ViewBuilder
    private func section(_ card: OverviewCard, overview: ClusterOverview) -> some View {
        switch card {
        case .pinned:
            if !pins.isEmpty {
                PinnedSection(
                    pins: pins.pins, nodes: overview.nodes, argo: argoStatus, flux: fluxStatus,
                    masked: settings.privacyMask, open: { open($0, overview: overview) },
                    unpin: { PinStore.shared.remove($0.target, fingerprint: pinsFingerprint) })
            }
        case .talosUpdate:
            if let update { TalosUpdateSection(info: update, nodes: overview.nodes) }
        case .summary:
            Section {
                ClusterSummaryCard(
                    name: model.activeLabel,
                    context: model.activeSummary,
                    summary: ClusterSummary(nodes: overview.nodes),
                    answered: overview.answered(of: overviewNodeTargets),
                    live: liveStats ? ClusterLiveStore.shared.live(for: loadID) : nil
                )
                // The card's two actions as named rows; the card itself opens nothing.
                NavigationLink(value: Route.insights) {
                    Label {
                        Text("Insights")
                    } icon: {
                        Image(systemName: "chart.xyaxis.line")
                    }
                }
                // Capacity reads pods and nodes through the admin kubeconfig, as the workloads do.
                if model.allows(.workloads) {
                    NavigationLink(value: Route.capacity) {
                        Label {
                            Text(verbatim: CapacityText.capacityTitle)
                        } icon: {
                            Image(systemName: "gauge.with.dots.needle.33percent")
                        }
                    }
                }
                // The cluster's own Git repository, once one is saved (the demo has one).
                ClusterRepoToolLink {
                    Label {
                        Text("Repository")
                    } icon: {
                        Image(systemName: "folder.badge.gearshape")
                    }
                }
            } header: {
                if let access = model.activeSummary?.localizedAccessLabel { Text(access) }
            }
        case .apps:
            AppsCard(state: inventory, hostnames: hostnames, argo: argoStatus, path: $path)
        case .dataServices:
            if let dataServices {
                DataServicesSection(
                    state: dataServices, hints: dataHints, apps: inventoryApps, downNodes: overview.downHostnames,
                    plugins: dataPlugins)
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
            OverviewNodesSection(
                overview: overview, expanded: $nodesExpanded, tourActive: tourActive,
                tourNode: tourActive ? tourDegradedNode(overview)?.node : nil,
                canDetectIPs: canDetectIPs(overview), detectIPs: { confirmDetectIPs = true }, path: $path)
        case .timeDrift:
            // Re-checked with every overview refresh (the load time is the task id).
            TimeDriftSection(hostnames: hostnames, refreshID: loadedAt)
        case .alerts:
            if let alerts { AlertsSection(state: alerts) }
        case .tools:
            OverviewKubeTools(
                dataServices: dataServices, plugins: toolPlugins, downNodes: overview.downHostnames, refreshID: loadedAt
            )
        }
    }

    /// The demo tour runs here: the Talos demo's overview on screen, with neither the lock nor
    /// the screenshot mode's switching cover over it.
    private var tourActive: Bool {
        DemoTourSteps.runs(demo: model.activeSummary?.demo == true, kube: model.activeIsKube)
            && visible && scenePhase == .active && !settings.maskTransition && !lockStore.lock.locked
    }

    /// The coach marks whose anchor is on screen; nil until the overview is loaded.
    private var tourAvailable: Set<DemoTourStep>? {
        guard case .loaded(let overview, _, _) = state, overview.outage == nil, !fromMonitor else { return nil }
        var available: Set<DemoTourStep> = [.switcher, .settings]
        if layout.visible.contains(.nodes) {
            available.insert(.nodes)
            if tourDegradedNode(overview) != nil { available.insert(.degradedNode) }
        }
        if tourActionsAnchor != nil { available.insert(.actions) }
        return available
    }

    /// Restarts the tour's tasks when what it shows on changes.
    private var tourID: String {
        let available = tourAvailable.map { $0.map(\.rawValue).sorted().joined(separator: ",") } ?? "-"
        return "\(loadID)|\(tourActive)|\(available)|\(DemoTour.generation)"
    }

    /// The node the tour's third coach mark points at: the first one not ready, with a row or
    /// chip of its own (not on a large cluster's counts).
    private func tourDegradedNode(_ overview: ClusterOverview) -> NodeOverview? {
        guard !isDenseCluster(overview.nodes.count) else { return nil }
        return overview.nodes.overviewOrder.first { $0.status != .ready }
    }

    /// The toolbar's icons as offered.
    private var barIcons: [OverviewAction] { bar.icons.filter(offered) }

    /// The icon the tour's action bar coach mark points at: the first one, Settings aside.
    private var tourActionsAnchor: OverviewAction? { barIcons.first { $0 != .settings } }

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
        model.allows(.kubeconfig) && !settings.privacyMask && overview.lacksPublicIPs
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
    var loadID: String { "\(model.activeContext)#\(model.dataGeneration)" }

    /// Restarts (or stops) the live sampling when one of its conditions changes.
    private var liveID: String {
        var nodes = -1
        // Another set of problem nodes is another focus set for a large cluster's samples.
        var problems = 0
        if !fromMonitor, case .loaded(let overview, _, _) = state, !overview.partial, overview.outage == nil {
            nodes = overview.nodes.count
            problems = liveFocus(overview).hashValue
        }
        return "\(loadID)|\(liveStats)|\(visible)|\(scenePhase == .active)|\(nodes)|\(problems)"
    }

    /// The nodes a large cluster's live samples always ask: those needing attention (down, not
    /// ready or reporting a problem), by address.
    private func liveFocus(_ overview: ClusterOverview) -> Set<String> {
        Set(overview.nodes.filter(\.needsAttention).map(\.node))
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
    /// Kubernetes API with the admin kubeconfig Talos issues; GitOps only when the cluster runs
    /// Argo CD or Flux (each asked only for such a cluster and role). Alerts like the metrics: its
    /// screen sets a source up by hand when none is found.
    private func offered(_ action: OverviewAction) -> Bool {
        switch action {
        case .health: model.allows(.health)
        case .workloads, .metrics, .alerts: model.allows(.workloads)
        case .gitOps: argo != nil || flux != nil
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
        // Argo CD when the cluster has it, else Flux: the same screens as their sections.
        case .gitOps: argo != nil ? .argoCD(downNodes: loadedDownHostnames) : .flux(downNodes: loadedDownHostnames)
        case .metrics: .metrics
        case .alerts: .alerts
        case .kubespan: .kubespan
        case .etcd: .etcd
        case .search: .search
        case .settings: .settings
        }
    }

    /// The hostnames of the nodes not answering in the overview on screen.
    private var loadedDownHostnames: Set<String> {
        guard case .loaded(let overview, _, _) = state else { return [] }
        return overview.downHostnames
    }

    /// The fingerprint the cluster on screen keeps its pins under.
    private var pinsFingerprint: String { model.activeSummary?.fingerprint ?? "" }

    /// The screens pinned on the cluster on screen.
    private var pins: PinList { PinStore.shared.pins(for: pinsFingerprint) }

    /// Opens a pin's screen as its share link would: a node straight from the overview's nodes,
    /// the rest through the link's route.
    private func open(_ pin: Pin, overview: ClusterOverview) {
        if pin.target.target == .node {
            if let node = pinnedNode(pin.target, in: overview.nodes) { path.append(.node(node.ref)) }
            return
        }
        let client = model.client
        let kube = model.activeIsKube
        Task {
            guard let route = await pin.target.route(client: client, kube: kube) else { return }
            path.append(route)
        }
    }

    /// The Flux status the Flux section loaded, for the Pinned card's chips.
    private var fluxStatus: FluxStatus? {
        if case .loaded(let status, _, _)? = flux { return status }
        return nil
    }

    /// The Argo CD status the Argo CD section loaded, for the Apps card's badges.
    private var argoStatus: ArgoStatus? {
        if case .loaded(let status, _, _)? = argo { return status }
        return nil
    }

    private func load() async {
        guard let client = model.client else { return }
        StartupTrace.markOnce("overview load start")
        // Nothing on screen: the last known overview (when kept) while this one loads.
        if case .loaded = state {
        } else {
            state = model.seeded(LoadState<ClusterOverview>.loading, from: .overview)
            if case .loaded = state { StartupTrace.markOnce("overview first Loaded", detail: "(last known)") }
        }
        // Else the nodes the last background check saw, said as such.
        if case .loading = state, let snapshot = model.monitorSeed(kube: false) {
            state = .loaded(snapshot.overviewSeed(), at: snapshot.takenAt)
            fromMonitor = true
            StartupTrace.markOnce("overview first Loaded", detail: "(background check)")
        }
        // The call is not cancelled with its task: a slow load of the previous context can
        // end after the new one's, and must not replace it.
        let id = loadID
        let target = model.lastKnownTarget
        let fetched = await streamOverview(with: client, id: id)
        guard id == loadID else { return }
        let loaded = merged(fetched, for: target)
        // Only the complete overview reaches merged (and so remember) and the rest below.
        state = state.finished(with: loaded)
        if case .loaded = loaded { fromMonitor = false }
        if case .loaded = state { StartupTrace.markOnce("overview first Loaded", detail: "(fresh)") }
        if case .loaded(let overview, _, _) = loaded {
            // Alongside the rest: listing every node's containers takes a while.
            Task { await loadInventory(with: client, id: id) }
            Task { await loadAlerts(with: client, id: id) }
            // The map that groups the nodes by site: once per cluster (see TopologyStore).
            if overview.outage == nil {
                let key = model.topologyKey
                Task { await TopologyStore.shared.load(with: client, key: key) }
            }
            await checkTalosUpdate(overview, id: id)
            guard id == loadID else { return }
            // What each node's Talos version can do, cached per version: gates menus and screens.
            await model.loadFeatures(of: overview.nodes)
            // Discovery asks the nodes too: pointless while none answers.
            if overview.outage == nil { await discover(with: client, id: id) }
        }
    }

    /// The overview node by node: each node on screen as it answers while nothing else is
    /// (a refresh or a cluster switch keeps the rows shown, see showingPartial), the complete
    /// one returned. Not cancelled with the calling task (like the one-shot call before it);
    /// it stops once the screen moved to another cluster.
    private func streamOverview(with client: TalosClient, id: String) async -> LoadState<ClusterOverview> {
        let context = model.activeSummary?.name ?? model.activeContext
        let targets = overviewNodeTargets
        return await Task { @MainActor in
            await LoadState<ClusterOverview>.from {
                var assembly = OverviewAssembly(context: context, targets: targets)
                for try await part in client.overviewStream() {
                    switch part {
                    case .node(let node):
                        guard id == loadID else { throw CancellationError() }
                        assembly = assembly.adding(node)
                        if let shown = state.showingPartial(assembly.overview) {
                            state = shown
                            StartupTrace.markOnce("overview first Loaded", detail: "(first node)")
                        }
                    case .done(let overview):
                        return overview
                    }
                }
                throw CancellationError()
            }
        }.value
    }

    /// The nodes the overview asks (as Go picks them): the total of "7 of 8 nodes answered".
    private var overviewNodeTargets: [String] {
        guard let context = model.activeSummary else { return [] }
        return overviewTargets(nodes: context.nodes, endpoints: context.endpoints, omni: context.omni)
    }

    /// Whether the Talos update section has something to show: a newer release, not skipped.
    private var offersTalosUpdate: Bool {
        update.flatMap { talosUpdateBannerCount($0, localOutdated: 0, skipped: model.activeSkippedTalosUpdate) } != nil
    }

    /// Not asked (an internet call) for the demo cluster, or while its section is hidden.
    private func checkTalosUpdate(_ overview: ClusterOverview, id: String) async {
        let unasked = model.activeSummary?.demo == true || layout.hidden.contains(.talosUpdate)
        let info =
            unasked
            ? nil : await TalosUpdateChecker.refresh(nodeVersions: overview.nodes.filter(\.reachable).map(\.version))
        guard id == loadID else { return }
        update = info
    }

    /// `fetched` with the nodes that stopped answering filled in from the overview on screen
    /// (see mergeLastKnown), kept as last known while a node answers: an outage leaves the
    /// stored one, with its time, as it is.
    private func merged(_ fetched: LoadState<ClusterOverview>, for target: LastKnownTarget?) -> LoadState<
        ClusterOverview
    > {
        guard case .loaded(let overview, let at, _) = fetched else { return fetched }
        var previous: ClusterOverview?
        var previousAt = at
        // The background check's seed is not the cluster's data: nothing to fill nodes in from
        // (nor are this load's own partial nodes).
        if !fromMonitor, let shown = state.completeOverview {
            previous = shown.overview
            previousAt = shown.at
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

    /// Sections the cluster has nothing for (the same checks as their loading), left out of the
    /// editor too; all offered until the inventory is known.
    private var absentCards: Set<OverviewCard> {
        // Known before the inventory: no pins, no Alertmanager.
        var known: Set<OverviewCard> = pins.isEmpty ? [.pinned] : []
        if !model.allows(.workloads) { known.insert(.tools) }
        if !model.allows(.workloads) || AlertsStore.shared.absent(for: model.alertsKey) { known.insert(.alerts) }
        guard case .loaded(let apps, _, _) = inventory else { return known }
        let kubernetes = model.allows(.workloads)
        var absent: Set<OverviewCard> = known
        if !kubernetes || dataServiceHints(apps, plugins: pluginCatalog).isEmpty { absent.insert(.dataServices) }
        if !kubernetes || !argoCDHinted(apps) { absent.insert(.argoCD) }
        if !kubernetes || !fluxHinted(apps) { absent.insert(.flux) }
        return absent
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
