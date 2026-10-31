import SwiftUI
import IchorCore

extension KubeHomeAction: BarActionLook {
    var systemImage: String {
        switch self {
        case .workloads: "square.stack.3d.up"
        case .resources: "square.grid.3x3"
        case .gitOps: "arrow.triangle.2.circlepath"
        case .metrics: "chart.xyaxis.line"
        case .alerts: "bell.badge"
        case .helm: "shippingbox"
        case .dataServices: "externaldrive.connected.to.line.below"
        case .checkup: "stethoscope"
        case .capacity: "gauge.with.dots.needle.33percent"
        case .apiHealth: "heart.text.square"
        case .networkPolicies: "shield.lefthalf.filled"
        case .search: "magnifyingglass"
        case .settings: "gearshape"
        }
    }

    var title: Text {
        switch self {
        case .workloads: Text("Kubernetes workloads")
        case .resources: Text("Resources")
        case .gitOps: Text(verbatim: "GitOps")
        case .metrics: Text("Metrics")
        case .alerts: Text("Alerts")
        case .helm: Text("Helm releases")
        case .dataServices: Text("Data services")
        case .checkup: Text(CheckupText.checkupTitle)
        case .capacity: Text(verbatim: CapacityText.capacityTitle)
        case .apiHealth: Text("API server")
        case .networkPolicies: Text("Network policies")
        case .search: Text("Search clusters")
        case .settings: Text("Settings")
        }
    }
}

/// The home of a cluster added from a kubeconfig, in place of the Talos overview: the API
/// server and who the app is to it, its nodes as Kubernetes sees them (KubeNodes), the
/// Kubernetes screens, and Argo CD, Flux and the data services (Longhorn, CloudNativePG...)
/// when installed. No Talos section or action: the cluster has no Talos API; a node can
/// still be cordoned and drained through Kubernetes. The sections and the toolbar are arranged
/// like the overview's (Customize home, in the ⋯ menu), in a layout of their own.
struct KubeHomeView: View {
    /// The navigation path, shared with the Talos overview.
    @Binding var path: [Route]

    /// The Kubernetes screens pushed from the toolbar that have no route of their own.
    enum Screen: Hashable {
        case resources
        case helm
        case checkup
        case capacity
        case apiHealth
        case policies
    }

    @Environment(AppModel.self) private var model
    @Environment(SettingsStore.self) private var settings
    @Environment(AISettings.self) private var ai
    @State private var state: LoadState<KubeNodesOverview> = .loading
    /// `state` is only what the last background check saw (a seed), until the cluster answers.
    @State private var fromMonitor = false
    /// The node whose cordon to change, after a confirmation.
    @State private var cordoning: KubeNodeInfo?
    @State private var screen: Screen?
    /// Argo CD, Flux and the data services: asked without inventory hints (there is no Talos
    /// inventory to read), and shown once the answer says they are installed; nil hides the section.
    @State private var argo: LoadState<ArgoStatus>?
    @State private var flux: LoadState<FluxStatus>?
    @State private var dataServices: LoadState<DataServices>?
    /// The systems read with Data services, for those the tools file in another family.
    @State private var toolServices: DataServices?
    /// The CRD plugins the cluster runs: Data services lines for those of that category, a tool
    /// each for the others. Empty until they answer (a failure keeps what was shown).
    @State private var plugins = PluginDetect()
    /// Crossplane: nil until the cluster says it runs it (the read answers "not installed" fast).
    @State private var crossplane: LoadState<Crossplane>?
    /// Tailscale: nil until the cluster says it runs the operator (the read answers fast).
    @State private var tailscale: LoadState<Tailscale>?
    /// Cluster API or Sidero Metal: nil until the cluster says it runs one (the read answers fast).
    @State private var clusterLifecycle: LoadState<ClusterLifecycle>?
    /// Argo Rollouts: nil until the cluster says it serves Rollouts (the read answers fast).
    @State private var argoRollouts: LoadState<ArgoRolloutsStatus>?
    /// The Alertmanager's alerts: nil until one is found (none hides the section).
    @State private var alerts: LoadState<AMAlerts>?
    /// Who the API server takes the credentials for; nil until known (or when it cannot say).
    @State private var whoAmI: KubeWhoAmI?
    /// Node usage from metrics-server, by node name; empty without it (no bars then).
    @State private var top: [String: KubeTopNode] = [:]
    /// The apps from the pods, as the Talos overview lists them; nil when the credentials may not list them.
    @State private var inventory: LoadState<ClusterInventory>? = .loading
    /// Shared with the Talos overview: one choice of chips or full rows for every cluster.
    @AppStorage("overview.nodesExpanded") private var nodesExpanded = false
    /// The sections' order and those hidden, the toolbar's icons and menu: one arrangement for
    /// every cluster added from a kubeconfig, changed in HomeEditorSheet (same saved form as Android).
    @AppStorage(KubeHomeLayout.storageKey) private var layoutText = ""
    @AppStorage(KubeHomeBar.storageKey) private var barText = ""
    @State private var customizing = false
    /// The incident summary sheet, from the cluster menu.
    @State private var sharingIncident = false
    @State private var showingServiceAccountHelp = false
    /// "Since you last looked" (the history ring).
    @State private var look = HistoryLook()

    var body: some View {
        LoadStateView(state: state, retry: load, fromMonitor: fromMonitor) { overview in
            List {
                // Offered once, after the first real cluster: first, above everything else.
                AlertsOfferSection()
                if model.activeSummary?.demo == true {
                    Section {
                        Text(
                            "Demo cluster · Sample data. Cluster changes are unavailable. Remove the demo from Manage clusters when finished."
                        )
                        .font(.callout).foregroundStyle(.secondary)
                    }
                }
                HistorySinceSection(look: look)
                if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
                    Section { KubeExpiryBanner(notAfter: ctx.certNotAfter) }
                }
                if let target = model.activeSignInTarget { KubeSignInSection(target: target) }
                // The sections as arranged (Customize home, in the ⋯ menu).
                ForEach(layout.visible) { card in section(card, overview: overview) }
                if layout.visible.isEmpty {
                    Section {
                        Button {
                            customizing = true
                        } label: {
                            Text("Every card is hidden. Tap to choose the ones to show.").foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
            .accessibilityIdentifier(SmokeID.kubeHome)
        }
        .navigationTitle(model.activeLabel)
        .kubeCordonDialogs(cordoning: $cordoning, reload: load)
        // The node menus' cordon and drain are cluster-wide.
        .loadsKubeActionAccess(namespace: "")
        // Shown by the title once it is inline (scrolled); before that, under ⋯ → Cluster.
        .toolbarTitleMenu { ClusterTitleMenu(path: $path, shareIncident: { sharingIncident = true }) }
        .safeAreaInset(edge: .top, spacing: 0) {
            if (model.summary?.contexts.count ?? 0) > 1 {
                ClusterBar { path.append(.clusters) }
            }
        }
        .toolbar {
            if settings.privacyMask {
                ToolbarItem(placement: .topBarLeading) {
                    Image(systemName: "eye.slash")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(Text("Screenshot mode"))
                }
            }
            ToolbarItemGroup(placement: .primaryAction) {
                // Optional: only once turned on in the settings.
                if ai.enabled {
                    Button {
                        path.append(Route.diagnosis(note: ""))
                    } label: {
                        Label("AI diagnosis", systemImage: "sparkles")
                    }
                }
                // As arranged: the bar's icons, the rest behind ⋯ (with the arrangement itself).
                ForEach(bar.icons.filter(offered)) { action in
                    Button {
                        open(action)
                    } label: {
                        Label {
                            action.title
                        } icon: {
                            Image(systemName: action.systemImage)
                        }
                    }
                }
                Menu {
                    let menu = bar.menu.filter(offered)
                    ForEach(menu) { action in
                        Button {
                            open(action)
                        } label: {
                            Label {
                                action.title
                            } icon: {
                                Image(systemName: action.systemImage)
                            }
                        }
                    }
                    if !menu.isEmpty { Divider() }
                    ClusterSubmenu(path: $path, shareIncident: { sharingIncident = true })
                    Button {
                        customizing = true
                    } label: {
                        Label("Customize home", systemImage: "pencil")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .accessibilityLabel(Text("More"))
            }
        }
        .sheet(isPresented: $customizing) {
            HomeEditorSheet(KubeHomeCard.self, KubeHomeAction.self, title: "Customize home", absent: absentCards)
        }
        .sheet(isPresented: $showingServiceAccountHelp) { ServiceAccountHelpSheet() }
        .sheet(isPresented: $sharingIncident) {
            if let context = model.activeSummary {
                IncidentSummarySheet(context: context.name, cluster: monitorClusterKey(context))
            }
        }
        .navigationDestination(item: $screen) { screen in
            switch screen {
            case .resources: KubeBrowserView()
            case .helm: HelmReleasesView()
            case .checkup: CheckupView()
            case .capacity: KubeCapacityView()
            case .apiHealth: ApiHealthView()
            case .policies: NetPoliciesView()
            }
        }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        .watchesHistory(look, cluster: model.activeSummary.map(monitorClusterKey) ?? "", id: loadID)
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: loadID) {
            state = .loading
            fromMonitor = false
            argo = nil
            flux = nil
            dataServices = nil
            toolServices = nil
            plugins = PluginDetect()
            crossplane = nil
            tailscale = nil
            clusterLifecycle = nil
            argoRollouts = nil
            alerts = nil
            whoAmI = nil
            top = [:]
            inventory = .loading
        }
        // The screenshot mode switching: the Kubernetes screen pushed from here holds old names.
        .onChange(of: settings.maskTransition) { _, switching in
            if switching { screen = nil }
        }
        // A new network (VPN connected, back on Wi-Fi) is the likely fix: try again at once.
        .task {
            for await _ in NetworkChanges.stream() {
                if case .failed = state { await load() }
            }
        }
    }

    /// One of the home's arranged sections; those that report on something the cluster does
    /// not have stay out (Android's KubeHomeCard.whenDetected).
    @ViewBuilder
    private func section(_ card: KubeHomeCard, overview: KubeNodesOverview) -> some View {
        switch card {
        case .pinned:
            if !pins.isEmpty {
                // No Talos nodes here: a pinned node shows without its state.
                PinnedSection(
                    pins: pins.pins, nodes: [], argo: argoStatus, flux: fluxStatus,
                    masked: settings.privacyMask, open: open,
                    unpin: { PinStore.shared.remove($0.target, fingerprint: pinsFingerprint) })
            }
        case .summary:
            summarySection(overview)
        case .apps:
            if let inventory {
                AppsCard(state: inventory, hostnames: [:], argo: argoStatus, path: $path)
            }
        case .nodes:
            nodesSection(overview)
        case .tools:
            toolsSection
        case .dataServices:
            if let dataServices {
                DataServicesSection(
                    state: dataServices, hints: "", apps: inventoryApps, downNodes: notReadyNodes(overview),
                    plugins: plugins.dataServicePlugins, families: [.data])
            }
        case .argoCD:
            if let argo {
                ArgoSection(state: argo, app: inventoryApps[argoCDCatalogID], downNodes: notReadyNodes(overview))
            }
        case .flux:
            if let flux {
                FluxSection(state: flux, app: inventoryApps[fluxCatalogID], downNodes: notReadyNodes(overview))
            }
        case .alerts:
            if let alerts { AlertsSection(state: alerts) }
        }
    }

    /// The summary card, as on the Talos home, then its footer: the checkup and the capacity.
    private func summarySection(_ overview: KubeNodesOverview) -> some View {
        Section {
            if let ctx = model.activeSummary {
                KubeSummaryCard(
                    name: model.activeLabel, context: ctx, overview: overview,
                    usage: Array(top.values), whoAmI: whoAmI)
                if ctx.usesKubeToken {
                    Button("How to create a service account") { showingServiceAccountHelp = true }
                }
                NavigationLink {
                    CheckupView()
                } label: {
                    toolLabel(KubeHomeAction.checkup)
                }
                NavigationLink(value: Route.capacity) {
                    toolLabel(KubeHomeAction.capacity)
                }
            }
        } header: {
            Text("Cluster")
        }
    }

    /// The nodes grouped by status, the worst first, as the Talos overview: collapsed (the
    /// default) a chip per node, expanded a full row; past nodeDenseThreshold a dot per node
    /// (DenseKubeNodes) and the header opens the Kubernetes nodes screen: a section holding
    /// hundreds of rows defeats the home.
    private func nodesSection(_ overview: KubeNodesOverview) -> some View {
        let dense = isDenseCluster(overview.nodes.count)
        return Section {
            if overview.forbidden {
                Text("These credentials may not list the nodes. The other screens show what they may read.")
                    .font(.callout).foregroundStyle(.secondary)
            } else if dense {
                DenseKubeNodes(nodes: overview.nodes, path: $path, cordoning: $cordoning)
            } else if !nodesExpanded {
                ChipFlow(spacing: 8) {
                    ForEach(overview.nodes.byStatus.flatMap(\.nodes)) { node in
                        // No node screen without Talos: the chip offers the node's actions.
                        Menu {
                            KubeNodeMenu(node: node, path: $path, cordoning: $cordoning)
                        } label: {
                            NodeChipLabel(name: node.name, status: node.status)
                        }
                        .foregroundStyle(node.status == .ready ? Color.primary : node.status.color)
                        .accessibilityLabel(Text(verbatim: "\(node.name), \(node.status.label)"))
                    }
                }
                .buttonStyle(.borderless)
                .padding(.vertical, 4)
            } else {
                ForEach(overview.nodes.byStatus.flatMap(\.nodes)) { node in
                    KubeNodeActionRow(node: node, path: $path, cordoning: $cordoning, usage: top[node.name])
                }
            }
        } header: {
            HStack {
                Text("Nodes")
                Spacer()
                if !overview.forbidden { Text(verbatim: "\(overview.nodes.count)") }
                if dense {
                    Button {
                        path.append(.kubeNodes(filter: nil, nodes: overview.nodes))
                    } label: {
                        Image(systemName: "chevron.right")
                    }
                    .accessibilityLabel(Text("Show all nodes"))
                }
                if !dense && !overview.forbidden && !overview.nodes.isEmpty {
                    Button {
                        withAnimation { nodesExpanded.toggle() }
                    } label: {
                        Image(systemName: nodesExpanded ? "chevron.up" : "chevron.down")
                    }
                    .accessibilityLabel(nodesExpanded ? Text("Show less") : Text("Show all"))
                }
            }
        }
    }

    /// The Kubernetes screens, with those of the operators the cluster runs.
    private var toolsSection: some View {
        KubeToolsSections(
            services: toolServices, plugins: plugins.homeTiles, downNodes: loadedNotReadyNodes,
            crossplane: crossplane != nil, tailscale: tailscale != nil, clusterLifecycle: clusterLifecycle != nil,
            argoRollouts: rolloutsInstalled
        )
    }

    /// The cluster serves Argo Rollouts; a refused read (no access) stays quiet.
    private var rolloutsInstalled: Bool {
        if case .loaded(let rollouts, _, _) = argoRollouts { return rollouts.installed }
        return false
    }

    /// A screen row named and drawn as in the toolbar and its editor.
    private func toolLabel(_ action: KubeHomeAction) -> some View {
        toolLabel(action.title, systemImage: action.systemImage)
    }

    private func toolLabel(_ title: Text, systemImage: String) -> some View {
        KubeToolLabel(title: title, systemImage: systemImage)
    }

    private func open(_ action: KubeHomeAction) {
        switch action {
        case .workloads: path.append(.workloads)
        case .resources: screen = .resources
        case .gitOps: if let route = gitOpsRoute { path.append(route) }
        case .metrics: path.append(.metrics)
        case .alerts: path.append(.alerts)
        case .helm: screen = .helm
        case .dataServices: path.append(.dataServices(hints: "", downNodes: loadedNotReadyNodes))
        case .checkup: screen = .checkup
        case .capacity: screen = .capacity
        case .apiHealth: screen = .apiHealth
        case .networkPolicies: screen = .policies
        case .search: path.append(.search)
        case .settings: path.append(.settings)
        }
    }

    /// The names of the nodes that are not ready: the likely cause of an operator's problems.
    private func notReadyNodes(_ overview: KubeNodesOverview) -> Set<String> {
        Set(overview.nodes.filter { !$0.ready }.map(\.name))
    }

    private var loadedNotReadyNodes: Set<String> {
        guard case .loaded(let overview, _, _) = state else { return [] }
        return notReadyNodes(overview)
    }

    private var layout: KubeHomeLayout { .parse(layoutText) }
    private var bar: KubeHomeBar { .parse(barText) }

    /// GitOps only once the cluster answered that it runs Argo CD or Flux.
    private func offered(_ action: KubeHomeAction) -> Bool {
        action != .gitOps || gitOpsRoute != nil
    }

    /// Argo CD when installed, else Flux: the same screens as their sections; nil for neither.
    private var gitOpsRoute: Route? {
        if case .loaded(let status, _, _)? = argo, status.installed { return .argoCD(downNodes: loadedNotReadyNodes) }
        if case .loaded(let status, _, _)? = flux, status.installed { return .flux(downNodes: loadedNotReadyNodes) }
        return nil
    }

    /// Sections the cluster has nothing for, left out of the editor too; each offered until its answer came.
    /// Best effort, as on the Talos overview: a first failure (credentials that may not list every
    /// pod) hides the apps section; a failed refresh keeps the apps shown.
    private func loadInventory(with client: TalosClient, id: String) async {
        if case .loading? = inventory {
            inventory = model.seeded(LoadState<ClusterInventory>.loading, from: .inventory)
        }
        let loaded: LoadState<ClusterInventory> = await .from { try await model.fetch(.inventory, with: client) }
        guard id == loadID else { return }
        switch (loaded, inventory) {
        case (.loaded(let apps, _, _), _) where apps.apps.isEmpty: inventory = nil
        case (.failed, .loaded?): inventory = inventory?.refreshed(with: loaded)
        case (.failed, _): inventory = nil
        default: inventory = loaded
        }
    }

    /// The inventory's apps by catalog id, for the data services' and GitOps sections' icons.
    private var inventoryApps: [String: InventoryApp] {
        guard case .loaded(let apps, _, _)? = inventory else { return [:] }
        return Dictionary(apps.apps.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
    }

    private var argoStatus: ArgoStatus? {
        if case .loaded(let status, _, _)? = argo { return status }
        return nil
    }

    private var fluxStatus: FluxStatus? {
        if case .loaded(let status, _, _)? = flux { return status }
        return nil
    }

    /// The fingerprint the cluster on screen keeps its pins under.
    private var pinsFingerprint: String { model.activeSummary?.fingerprint ?? "" }

    /// The screens pinned on the cluster on screen.
    private var pins: PinList { PinStore.shared.pins(for: pinsFingerprint) }

    /// Opens a pin's screen as its share link would; no node screen without Talos.
    private func open(_ pin: Pin) {
        guard pin.target.target != .node else { return }
        let client = model.client
        Task {
            guard let route = await pin.target.route(client: client, kube: true) else { return }
            path.append(route)
        }
    }

    private var absentCards: Set<KubeHomeCard> {
        var absent: Set<KubeHomeCard> = []
        if pins.isEmpty { absent.insert(.pinned) }
        if inventory == nil { absent.insert(.apps) }
        if case .loaded(let status, _, _)? = argo, !status.installed { absent.insert(.argoCD) }
        if case .loaded(let status, _, _)? = flux, !status.installed { absent.insert(.flux) }
        if case .loaded(let services, _, _)? = dataServices, services.detected.isEmpty,
            plugins.dataServicePlugins.isEmpty
        {
            absent.insert(.dataServices)
        }
        if AlertsStore.shared.absent(for: model.alertsKey) { absent.insert(.alerts) }
        return absent
    }

    /// What the loaded nodes belong to: the context and the screenshot mode generation.
    private var loadID: String { "\(model.activeContext)#\(model.dataGeneration)" }

    private func load() async {
        guard let client = model.client else { return }
        // The call is not cancelled with its task: a slow load of the previous context can
        // end after the new one's, and must not replace it.
        let id = loadID
        // Nothing on screen: the nodes the last background check saw, said as such.
        if case .loading = state, let snapshot = model.monitorSeed(kube: true) {
            state = .loaded(snapshot.kubeNodesSeed(), at: snapshot.takenAt)
            fromMonitor = true
        }
        let fetched: LoadState<KubeNodesOverview> = await .from { try await client.kubeNodes() }
        guard id == loadID else { return }
        state = state.refreshed(with: fetched)
        if case .loaded = fetched { fromMonitor = false }
        await loadInventory(with: client, id: id)
        // Kept on a failed refresh; hidden when the API server cannot say (before 1.28).
        if let me = try? await client.kubeWhoAmI(), id == loadID { whoAmI = me }
        guard id == loadID else { return }
        // Usage is live: replaced on each load, dropped when metrics-server is gone.
        let usage = try? await client.topNodes()
        guard id == loadID else { return }
        top = usage?.available == true ? usage?.byName ?? [:] : [:]
        // Argo CD, Flux and the data services answer "not installed" quickly when absent; a
        // tool not installed stays out (nil), the rest keeps what was shown on a failed refresh.
        let argoNext = await loadArgo(with: client)
        guard id == loadID else { return }
        argo = argoNext.map { (argo ?? .loading).refreshed(with: $0) }
        let fluxNext = await loadFlux(with: client)
        guard id == loadID else { return }
        flux = fluxNext.map { (flux ?? .loading).refreshed(with: $0) }
        // The plugins with the data services: a cluster whose only data service is a plugin
        // (kube-green) still shows the card. A failed ask keeps the plugins shown.
        async let detected = LoadState<PluginDetect>.from { try await client.pluginDetect(hints: "") }
        let dataRead: LoadState<DataServices> = await .from { try await client.dataServices(hints: "") }
        let detectRead = await detected
        guard id == loadID else { return }
        if case .loaded(let detect, _, _) = detectRead { plugins = detect }
        if case .loaded(let services, _, _) = dataRead { toolServices = services }
        dataServices = shownDataServices(dataRead).map { (dataServices ?? .loading).refreshed(with: $0) }
        // Crossplane: nil when the cluster does not run it (the read answers that quickly).
        let crossplaneNext = await loadCrossplane(with: client)
        guard id == loadID else { return }
        crossplane = crossplaneNext.map { (crossplane ?? .loading).refreshed(with: $0) }
        // Tailscale: nil when the cluster does not run the operator.
        let tailscaleNext = await loadTailscale(with: client)
        guard id == loadID else { return }
        tailscale = tailscaleNext.map { (tailscale ?? .loading).refreshed(with: $0) }
        // Cluster lifecycle: nil when the cluster runs neither Cluster API nor Sidero Metal.
        let lifecycleNext = await loadClusterLifecycle(with: client)
        guard id == loadID else { return }
        clusterLifecycle = lifecycleNext.map { (clusterLifecycle ?? .loading).refreshed(with: $0) }
        // Argo Rollouts: nil when the cluster does not serve Rollouts.
        let rolloutsNext = await loadArgoRollouts(with: client)
        guard id == loadID else { return }
        argoRollouts = rolloutsNext.map { (argoRollouts ?? .loading).refreshed(with: $0) }
        let alertsNext = await loadAlerts(with: client)
        guard id == loadID else { return }
        alerts = alertsNext.map { (alerts ?? .loading).refreshed(with: $0) }
    }

    /// The Alertmanager's alerts, nil when the cluster has none (searched once per cluster, see
    /// AlertsStore). A failure shows on the card only once one was found.
    private func loadAlerts(with client: TalosClient) async -> LoadState<AMAlerts>? {
        let key = model.alertsKey
        let fingerprint = model.activeSummary?.fingerprint ?? ""
        do {
            guard let read = try await AlertsStore.shared.load(with: client, key: key, fingerprint: fingerprint) else {
                return nil
            }
            return .loaded(read, at: Date())
        } catch {
            return alerts == nil ? nil : .failed(error.localizedDescription)
        }
    }

    /// The Argo CD status, nil when Argo CD is not installed.
    private func loadArgo(with client: TalosClient) async -> LoadState<ArgoStatus>? {
        let key = model.argoKey
        let loaded: LoadState<ArgoStatus> = await .from {
            try await ArgoCDStore.shared.load(with: client, key: key, cluster: model.activeSummary)
        }
        if case .loaded(let status, _, _) = loaded, !status.installed { return nil }
        return loaded
    }

    /// The Flux status, nil when Flux is not installed.
    private func loadFlux(with client: TalosClient) async -> LoadState<FluxStatus>? {
        let key = model.fluxKey
        let loaded: LoadState<FluxStatus> = await .from { try await FluxStore.shared.load(with: client, key: key) }
        if case .loaded(let status, _, _) = loaded, !status.installed { return nil }
        return loaded
    }

    /// The data services to show, nil when none of the operators is installed and no data
    /// services plugin runs. The operators are found from the API groups; Garage, which has
    /// none, from a listing of the pods (no hint, no inventory).
    private func shownDataServices(_ loaded: LoadState<DataServices>) -> LoadState<DataServices>? {
        // The card lists storage, databases and backups; the other systems are tools in their family.
        if case .loaded(let services, _, _) = loaded, services.detected(.data).isEmpty,
            plugins.dataServicePlugins.isEmpty
        {
            return nil
        }
        return loaded
    }

    /// Crossplane, nil when the cluster does not run it: the read answers that quickly when the
    /// API groups are absent.
    private func loadCrossplane(with client: TalosClient) async -> LoadState<Crossplane>? {
        let loaded: LoadState<Crossplane> = await .from { try await client.crossplane() }
        if case .loaded(let crossplane, _, _) = loaded, !crossplane.installed { return nil }
        return loaded
    }

    /// Tailscale, nil when the cluster does not run the operator: the read answers that quickly
    /// when tailscale.com is not served.
    private func loadTailscale(with client: TalosClient) async -> LoadState<Tailscale>? {
        let loaded: LoadState<Tailscale> = await .from { try await client.tailscale() }
        if case .loaded(let tailscale, _, _) = loaded, !tailscale.installed { return nil }
        return loaded
    }

    /// Argo Rollouts, nil when the cluster does not serve Rollouts: the read answers that quickly
    /// when the API group or the kind is absent.
    private func loadArgoRollouts(with client: TalosClient) async -> LoadState<ArgoRolloutsStatus>? {
        let loaded: LoadState<ArgoRolloutsStatus> = await .from { try await client.argoRollouts() }
        if case .loaded(let status, _, _) = loaded, !status.installed { return nil }
        return loaded
    }

    /// Cluster API and Sidero Metal, nil when the cluster serves neither: the read answers that
    /// quickly.
    private func loadClusterLifecycle(with client: TalosClient) async -> LoadState<ClusterLifecycle>? {
        let loaded: LoadState<ClusterLifecycle> = await .from { try await client.clusterLifecycle() }
        if case .loaded(let lifecycle, _, _) = loaded, !lifecycle.installed { return nil }
        return loaded
    }
}

/// The kubeconfig's credentials (client certificate or token) expire within certWarnDays, or
/// have: the app cannot renew them, a new kubeconfig has to be imported.
private struct KubeExpiryBanner: View {
    let notAfter: Int64

    var body: some View {
        let days = daysUntil(notAfter)
        Label {
            Text(
                days < 0
                    ? String(localized: "The kubeconfig credentials have expired. Import a new kubeconfig.")
                    : String(
                        localized:
                            "The kubeconfig credentials expire \(localizedCertExpiry(notAfter)). Import a new kubeconfig before then."
                    ))
        } icon: {
            Image(systemName: "exclamationmark.triangle.fill")
        }
        .foregroundStyle(days < 0 ? Color.red : Color.orange)
    }
}
