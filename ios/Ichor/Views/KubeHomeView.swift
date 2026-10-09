import SwiftUI
import IchorCore

extension KubeHomeAction: BarActionLook {
    var systemImage: String {
        switch self {
        case .workloads: "square.stack.3d.up"
        case .resources: "square.grid.3x3"
        case .gitOps: "arrow.triangle.2.circlepath"
        case .metrics: "chart.xyaxis.line"
        case .helm: "shippingbox"
        case .dataServices: "externaldrive.connected.to.line.below"
        case .checkup: "stethoscope"
        case .apiHealth: "heart.text.square"
        case .networkPolicies: "shield.lefthalf.filled"
        case .settings: "gearshape"
        }
    }

    var title: Text {
        switch self {
        case .workloads: Text("Kubernetes workloads")
        case .resources: Text("Resources")
        case .gitOps: Text(verbatim: "GitOps")
        case .metrics: Text("Metrics")
        case .helm: Text("Helm releases")
        case .dataServices: Text("Data services")
        case .checkup: Text(CheckupText.checkupTitle)
        case .apiHealth: Text("API server")
        case .networkPolicies: Text("Network policies")
        case .settings: Text("Settings")
        }
    }
}

extension KubeHomeCard: CardLook {
    var title: Text {
        switch self {
        case .summary: Text("Cluster summary")
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
        case .summary: Text("API server, Kubernetes version, nodes ready and who the credentials are")
        case .nodes: Text("Every node as Kubernetes lists it, with cordons and pressure")
        case .tools: Text("Buttons to the Kubernetes screens these credentials can use")
        case .dataServices: Text("Health of Longhorn, Garage and CloudNativePG")
        case .argoCD: Text("Sync and health of the Argo CD apps, syncs in progress")
        case .flux: Text("Ready, reconciling and failing Kustomizations and HelmReleases")
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
        case apiHealth
        case policies
    }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeNodesOverview> = .loading
    /// The node whose cordon to change, after a confirmation.
    @State private var cordoning: KubeNodeInfo?
    @State private var screen: Screen?
    /// Argo CD, Flux and the data services: asked without inventory hints (there is no Talos
    /// inventory to read), and shown once the answer says they are installed; nil hides the section.
    @State private var argo: LoadState<ArgoStatus>?
    @State private var flux: LoadState<FluxStatus>?
    @State private var dataServices: LoadState<DataServices>?
    /// Who the API server takes the credentials for; nil until known (or when it cannot say).
    @State private var whoAmI: KubeWhoAmI?
    /// The sections' order and those hidden, the toolbar's icons and menu: one arrangement for
    /// every cluster added from a kubeconfig, changed in HomeEditorSheet (same saved form as Android).
    @AppStorage(KubeHomeLayout.storageKey) private var layoutText = ""
    @AppStorage(KubeHomeBar.storageKey) private var barText = ""
    @State private var customizing = false

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
                    Section { KubeExpiryBanner(notAfter: ctx.certNotAfter) }
                }
                if let target = model.activeSignInTarget { KubeSignInSection(target: target) }
                // The sections as arranged (Customize home, in the ⋯ menu).
                ForEach(layout.visible) { card in section(card, overview: overview) }
                if layout.visible.isEmpty {
                    Section {
                        Button { customizing = true } label: {
                            Text("Every card is hidden. Tap to choose the ones to show.").foregroundStyle(.secondary)
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(model.activeLabel)
        .kubeCordonDialogs(cordoning: $cordoning, reload: load)
        // The node menus' cordon and drain are cluster-wide.
        .loadsKubeActionAccess(namespace: "")
        .toolbarTitleMenu {
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
            ShareLinkButton(target: .screen(.cluster))
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if (model.summary?.contexts.count ?? 0) > 1 {
                ClusterBar { path.append(.clusters) }
            }
        }
        .toolbar {
            if model.privacyMask {
                ToolbarItem(placement: .topBarLeading) {
                    Image(systemName: "eye.slash")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(Text("Screenshot mode"))
                }
            }
            ToolbarItemGroup(placement: .primaryAction) {
                // As arranged: the bar's icons, the rest behind ⋯ (with the arrangement itself).
                ForEach(bar.icons.filter(offered)) { action in
                    Button { open(action) } label: {
                        Label { action.title } icon: { Image(systemName: action.systemImage) }
                    }
                }
                Menu {
                    let menu = bar.menu.filter(offered)
                    ForEach(menu) { action in
                        Button { open(action) } label: {
                            Label { action.title } icon: { Image(systemName: action.systemImage) }
                        }
                    }
                    if !menu.isEmpty { Divider() }
                    Button { customizing = true } label: { Label("Customize home", systemImage: "pencil") }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .accessibilityLabel(Text("More"))
            }
        }
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
            }
        }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: loadID) {
            state = .loading
            argo = nil
            flux = nil
            dataServices = nil
            whoAmI = nil
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
                }
                LabeledContent("Sign-in", value: ctx.localizedAuthLabel)
                if let user = ctx.user, !user.isEmpty { LabeledContent("Signed in as", value: user) }
                if let whoAmI, whoAmI.isKnown { whoAmIRow(whoAmI) }
                if ctx.certNotAfter > 0 { LabeledContent("Expires", value: localizedCertExpiry(ctx.certNotAfter)) }
            }
        } header: {
            Text("Cluster")
        }
    }

    /// The user and groups the API server takes the credentials for (what RBAC decides on).
    private func whoAmIRow(_ me: KubeWhoAmI) -> some View {
        LabeledContent {
            VStack(alignment: .trailing, spacing: 2) {
                Text(verbatim: me.user).textSelection(.enabled)
                if !me.groupsLine.isEmpty {
                    Text(verbatim: me.groupsLine).font(.caption).foregroundStyle(.secondary)
                }
            }
            .multilineTextAlignment(.trailing)
        } label: {
            Text("Kubernetes user")
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

    /// The Kubernetes screens that work with these credentials alone.
    private var toolsSection: some View {
        Section {
            NavigationLink(value: Route.workloads) {
                Label("Kubernetes workloads", systemImage: "square.stack.3d.up")
            }
            NavigationLink { KubeBrowserView() } label: {
                Label("Resources", systemImage: "square.grid.3x3")
            }
            NavigationLink { HelmReleasesView() } label: {
                Label("Helm releases", systemImage: "shippingbox")
            }
            NavigationLink(value: Route.metrics) {
                Label("Metrics", systemImage: "chart.xyaxis.line")
            }
            NavigationLink(value: Route.dataServices(hints: "", downNodes: loadedNotReadyNodes)) {
                Label("Data services", systemImage: "externaldrive.connected.to.line.below")
            }
        } header: {
            Text("Kubernetes")
        }
    }

    private func open(_ action: KubeHomeAction) {
        switch action {
        case .workloads: path.append(.workloads)
        case .resources: screen = .resources
        case .gitOps: if let route = gitOpsRoute { path.append(route) }
        case .metrics: path.append(.metrics)
        case .helm: screen = .helm
        case .dataServices: path.append(.dataServices(hints: "", downNodes: loadedNotReadyNodes))
        case .checkup: screen = .checkup
        case .apiHealth: screen = .apiHealth
        case .networkPolicies: screen = .policies
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
    private var absentCards: Set<KubeHomeCard> {
        var absent: Set<KubeHomeCard> = []
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
        // Kept on a failed refresh; hidden when the API server cannot say (before 1.28).
        if let me = try? await client.kubeWhoAmI(), id == loadID { whoAmI = me }
        guard id == loadID else { return }
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
        let days = daysUntil(notAfter)
        Label {
            Text(days < 0
                 ? String(localized: "The kubeconfig credentials have expired. Import a new kubeconfig.")
                 : String(localized: "The kubeconfig credentials expire \(localizedCertExpiry(notAfter)). Import a new kubeconfig before then."))
        } icon: {
            Image(systemName: "exclamationmark.triangle.fill")
        }
        .foregroundStyle(days < 0 ? Color.red : Color.orange)
    }
}
