import SwiftUI
import IchorCore

extension KubeHomeCard: CardLook {
    var title: Text {
        switch self {
        case .summary: Text("Cluster summary")
        case .apps: Text("Apps")
        case .nodes: Text("Nodes")
        case .tools: Text("Kubernetes screens")
        case .dataServices: Text("Data services")
        case .argoCD: Text(verbatim: "Argo CD")
        case .flux: Text(verbatim: "Flux")
        }
    }

    /// One line on what the section shows, so the editor's names need no guessing.
    var detail: Text {
        switch self {
        case .summary: Text("API server, Kubernetes version, nodes ready, capacity and who the credentials are")
        case .apps: Text("The apps running on the cluster, from its pods, with their health")
        case .nodes: Text("Every node as Kubernetes lists it, with cordons and pressure")
        case .tools: Text("Buttons to the Kubernetes screens these credentials can use")
        case .dataServices: Text("Health of Longhorn, Garage and CloudNativePG")
        case .argoCD: Text("Sync and health of the Argo CD apps, syncs in progress")
        case .flux: Text("Ready, reconciling and failing Kustomizations and HelmReleases")
        }
    }
}

/// The home of a cluster added from a kubeconfig, in place of the Talos overview: the API
/// server and who the app is to it, the apps (from the pod list), its nodes as Kubernetes sees
/// them (KubeNodes), the Kubernetes screens, and Argo CD, Flux and the data services (Longhorn,
/// CloudNativePG...) when installed. No Talos section or action: the cluster has no Talos API;
/// a node can still be cordoned and drained through Kubernetes, and the AI diagnosis reads the
/// Kubernetes API alone. The sections and the toolbar are arranged like the overview's
/// (Customize home, in the ⋯ menu), in a layout of their own.
struct KubeHomeView: View {
    /// The navigation path, shared with the Talos overview.
    @Binding var path: [Route]

    /// The Kubernetes screens pushed from the toolbar that have no route of their own.
    enum Screen: Hashable {
        case resources
        case helm
        case checkup
        case apiHealth
        case policies
        case events
    }

    @Environment(AppModel.self) private var model
    @Environment(SupportPrompt.self) private var support
    @State private var state: LoadState<KubeNodesOverview> = .loading
    /// The apps card's data (KubeInventory, from the pod list), loaded with the nodes.
    @State private var inventory: LoadState<ClusterInventory> = .loading
    /// Release notes to present after an app update.
    @State private var whatsNew: WhatsNewContent?
    @State private var openChangelog = false
    /// The node whose cordon to change, after a confirmation.
    @State private var cordoning: KubeNodeInfo?
    @State private var screen: Screen?
    /// Argo CD, Flux and the data services: asked without inventory hints (there is no Talos
    /// inventory to read), and shown once the answer says they are installed; nil hides the section.
    @State private var argo: LoadState<ArgoStatus>?
    @State private var flux: LoadState<FluxStatus>?
    @State private var dataServices: LoadState<DataServices>?
    /// The sections' order and those hidden, the toolbar's icons and menu: one arrangement for
    /// every cluster added from a kubeconfig, changed in HomeEditorSheet (same saved form as Android).
    @AppStorage(KubeHomeLayout.storageKey) private var layoutText = ""
    @AppStorage(KubeHomeBar.storageKey) private var barText = ""
    @State private var customizing = false

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            List {
                if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
                    Section { KubeExpiryBanner(notAfter: ctx.certNotAfter) }
                }
                if let target = model.activeSignInTarget { KubeSignInSection(target: target) }
                if support.visible { Section { SupportCard(prompt: support) } }
                // The sections as arranged (Customize home, in the ⋯ menu).
                ForEach(layout.visible) { card in section(card, overview: overview) }
                if layout.visible.isEmpty { AllHiddenRow { customizing = true } }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .kubeCordonDialogs(cordoning: $cordoning, reload: load)
        // The cluster's title and menu, the cluster bar and the toolbar as arranged (HomeChrome).
        .homeChrome(path: $path, icons: bar.icons, menu: bar.menu, customizeTitle: "Customize home", customizing: $customizing, open: open)
        .sheet(isPresented: $customizing) {
            HomeEditorSheet(KubeHomeCard.self, KubeHomeAction.self, title: "Customize home", absent: absentCards)
        }
        .navigationDestination(item: $screen) { screen in
            switch screen {
            case .resources: KubeBrowserView()
            case .helm: HelmReleasesView()
            case .checkup: CheckupView()
            case .apiHealth: ApiHealthView()
            case .policies: NetPoliciesView()
            case .events: KubeEventsView()
            }
        }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: loadID) {
            state = .loading
            inventory = .loading
            argo = nil
            flux = nil
            dataServices = nil
        }
        // After an update (and the unlock): what changed since the build launched last time, as
        // on the Talos overview. The build is remembered once the notes are closed.
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
        case .summary:
            summarySection(overview)
        case .apps:
            AppsCard(state: inventory, hostnames: hostnames(overview), argo: argoStatus, path: $path)
        case .nodes:
            nodesSection(overview)
        case .tools:
            toolsSection
        case .dataServices:
            if let dataServices {
                DataServicesSection(state: dataServices, hints: "", apps: [:], downNodes: notReadyNodes(overview))
            }
        case .argoCD:
            if let argo {
                ArgoSection(state: argo, app: nil, downNodes: notReadyNodes(overview))
            }
        case .flux:
            if let flux {
                FluxSection(state: flux, app: nil, downNodes: notReadyNodes(overview))
            }
        }
    }

    private func summarySection(_ overview: KubeNodesOverview) -> some View {
        Section {
            if let ctx = model.activeSummary {
                LabeledContent("API server", value: ctx.endpoints.first ?? "")
                LabeledContent("Version", value: overview.serverVersion.isEmpty ? String(localized: "unknown") : overview.serverVersion)
                if !overview.forbidden {
                    LabeledContent("Nodes ready", value: "\(overview.readyCount)/\(overview.nodes.count)")
                    // The nodes' allocatable CPU and memory, as the Talos summary shows the machines'.
                    let cpu = overview.nodes.reduce(0.0) { $0 + $1.cpu }
                    let memory = overview.nodes.reduce(0.0) { $0 + $1.memory }
                    if cpu > 0 { LabeledContent("CPU cores", value: formatCores(cpu)) }
                    if memory > 0 { LabeledContent("Memory", value: formatBytes(Int64(memory))) }
                }
                LabeledContent("Sign-in", value: ctx.localizedAuthLabel)
                if let user = ctx.user, !user.isEmpty { LabeledContent("Signed in as", value: user) }
                if let namespace = ctx.namespace, !namespace.isEmpty { LabeledContent("Namespace", value: namespace) }
                if ctx.certNotAfter > 0 { LabeledContent("Expires", value: localizedCertExpiry(ctx.certNotAfter)) }
            }
        } header: {
            Text("Cluster")
        }
    }

    /// The nodes grouped by status, the worst first; past nodeDenseThreshold a dot per node
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
            } else {
                ForEach(overview.nodes.byStatus.flatMap(\.nodes)) { node in
                    KubeNodeActionRow(node: node, path: $path, cordoning: $cordoning)
                }
            }
        } header: {
            HStack {
                Text("Nodes")
                Spacer()
                if !overview.forbidden { Text(verbatim: "\(overview.nodes.count)") }
                if dense {
                    Button { path.append(.kubeNodes(filter: nil, nodes: overview.nodes)) } label: {
                        Image(systemName: "chevron.right")
                    }
                    .accessibilityLabel(Text("Show all nodes"))
                }
            }
        }
    }

    /// The Kubernetes screens that work with these credentials alone (the toolbar's, with their
    /// icons and names, and the apps).
    private var toolsSection: some View {
        Section {
            NavigationLink(value: Route.workloads) { toolLabel(.workloads) }
            NavigationLink(value: Route.apps(hostnames: loadedHostnames)) {
                Label("Apps", systemImage: "square.grid.2x2")
            }
            NavigationLink { KubeBrowserView() } label: { toolLabel(.resources) }
            NavigationLink { HelmReleasesView() } label: { toolLabel(.helm) }
            NavigationLink(value: Route.metrics) { toolLabel(.metrics) }
            NavigationLink(value: Route.dataServices(hints: "", downNodes: loadedNotReadyNodes)) { toolLabel(.dataServices) }
            NavigationLink { CheckupView() } label: { toolLabel(.checkup) }
            NavigationLink { ApiHealthView() } label: { toolLabel(.apiHealth) }
            NavigationLink { NetPoliciesView() } label: { toolLabel(.networkPolicies) }
            NavigationLink { KubeEventsView() } label: { toolLabel(.events) }
        } header: {
            Text("Kubernetes")
        }
    }

    private func toolLabel(_ action: KubeHomeAction) -> some View {
        Label { action.title } icon: { Image(systemName: action.systemImage) }
    }

    private func open(_ action: KubeHomeAction) {
        switch action {
        case .workloads: path.append(.workloads)
        case .resources: screen = .resources
        case .metrics: path.append(.metrics)
        case .helm: screen = .helm
        case .dataServices: path.append(.dataServices(hints: "", downNodes: loadedNotReadyNodes))
        case .checkup: screen = .checkup
        case .apiHealth: screen = .apiHealth
        case .networkPolicies: screen = .policies
        case .events: screen = .events
        case .settings: path.append(.settings)
        }
    }

    /// The node each pod runs on, by the name the inventory uses (the Kubernetes node name)
    /// and by address: what the apps screens show, and the node they open.
    private func hostnames(_ overview: KubeNodesOverview) -> [String: String] {
        var names: [String: String] = [:]
        for node in overview.nodes {
            names[node.name] = node.name
            if let ip = node.internalIP, !ip.isEmpty { names[ip] = node.name }
        }
        return names
    }

    private var loadedHostnames: [String: String] {
        guard case .loaded(let overview, _, _) = state else { return [:] }
        return hostnames(overview)
    }

    /// The Argo CD status the Argo CD section loaded, for the Apps card's badges.
    private var argoStatus: ArgoStatus? {
        if case .loaded(let status, _, _)? = argo { return status }
        return nil
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

    /// Sections the cluster has nothing for, left out of the editor too; each offered until its answer came.
    private var absentCards: Set<KubeHomeCard> {
        var absent: Set<KubeHomeCard> = []
        if case .loaded(let apps, _, _) = inventory, apps.apps.isEmpty { absent.insert(.apps) }
        if case .loaded(let status, _, _)? = argo, !status.installed { absent.insert(.argoCD) }
        if case .loaded(let status, _, _)? = flux, !status.installed { absent.insert(.flux) }
        if case .loaded(let services, _, _)? = dataServices, services.detected.isEmpty { absent.insert(.dataServices) }
        return absent
    }

    /// What the loaded nodes belong to: the context and the screenshot mode generation.
    private var loadID: String { "\(model.activeContext)#\(model.dataGeneration)" }

    private func load() async {
        guard let client = model.client else { return }
        // The call is not cancelled with its task: a slow load of the previous context can
        // end after the new one's, and must not replace it.
        let id = loadID
        let fetched: LoadState<KubeNodesOverview> = await .from { try await client.kubeNodes() }
        guard id == loadID else { return }
        state = state.refreshed(with: fetched)
        // Alongside the rest: the pod list of a large cluster takes a while.
        Task { await loadInventory(with: client, id: id) }
        // Argo CD, Flux and the data services answer "not installed" quickly when absent; a
        // tool not installed stays out (nil), the rest keeps what was shown on a failed refresh.
        let argoNext = await loadArgo(with: client)
        guard id == loadID else { return }
        argo = argoNext.map { (argo ?? .loading).refreshed(with: $0) }
        let fluxNext = await loadFlux(with: client)
        guard id == loadID else { return }
        flux = fluxNext.map { (flux ?? .loading).refreshed(with: $0) }
        let dataNext = await loadDataServices(with: client)
        guard id == loadID else { return }
        dataServices = dataNext.map { (dataServices ?? .loading).refreshed(with: $0) }
    }

    /// The apps, from the pods (KubeInventory). Best effort: a first failure hides the apps
    /// card; a failed refresh keeps the apps shown.
    private func loadInventory(with client: TalosClient, id: String) async {
        inventory = model.seeded(inventory, from: .inventory)
        let loaded: LoadState<ClusterInventory> = await .from { try await model.fetch(.inventory, with: client) }
        guard id == loadID else { return }
        inventory = inventory.refreshed(with: loaded)
    }

    /// The Argo CD status, nil when Argo CD is not installed.
    private func loadArgo(with client: TalosClient) async -> LoadState<ArgoStatus>? {
        let key = model.argoKey
        let loaded: LoadState<ArgoStatus> = await .from { try await ArgoCDStore.shared.load(with: client, key: key, cluster: model.activeSummary) }
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

    /// The data services, nil when none of the operators is installed. The operators are found
    /// from the API groups; Garage, which has none, from a listing of the pods (no hint, no inventory).
    private func loadDataServices(with client: TalosClient) async -> LoadState<DataServices>? {
        let loaded: LoadState<DataServices> = await .from { try await client.dataServices(hints: "") }
        if case .loaded(let services, _, _) = loaded, services.detected.isEmpty { return nil }
        return loaded
    }
}

/// The kubeconfig's credentials (client certificate or token) expire within certWarnDays, or
/// have: the app cannot renew them, a new kubeconfig has to be imported.
private struct KubeExpiryBanner: View {
    let notAfter: Int64

    var body: some View {
        ExpiryBanner(notAfter: notAfter) { days in
            days < 0
                ? String(localized: "The kubeconfig credentials have expired. Import a new kubeconfig.")
                : String(localized: "The kubeconfig credentials expire \(localizedCertExpiry(notAfter)). Import a new kubeconfig before then.")
        }
    }
}
