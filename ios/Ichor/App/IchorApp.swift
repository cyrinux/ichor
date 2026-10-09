import SwiftUI
import IchorCore
import UserNotifications

@main
struct IchorApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var model = AppModel()
    @State private var support = SupportPrompt()
    @State private var ai = AISettings()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        // Before any Talos call, including the background refresh task registered below.
        TalosClient.applyStoredPrivacyMask()
        TalosClient.setDataDirectory()
        // Where Go keeps the sign-ins of kubeconfig clusters, before any Kubernetes call.
        KubeAuthStore.register()
        SupportBundleStore.removeStaleParts()
        BackgroundMonitor.register()
        BackgroundMonitor.registerCategories()
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .environment(support)
                .environment(ai)
                .task { support.onLaunch() }
                // A share link or a config file while the app runs (the scene delegate takes them at launch).
                .onOpenURL { url in
                    if url.isFileURL { IncomingConfig.receive(url) }
                    if url.scheme == "ichor", url.host == "open" { NotificationRouter.shared.pendingShareLink = url }
                }
                .onChange(of: scenePhase) { _, phase in
                    if phase == .background, BackgroundMonitor.alertsEnabled { BackgroundMonitor.schedule() }
                    if phase == .background { UpgradeJob.shared.didEnterBackground() }
                    if phase == .background { MaintenanceJob.shared.didEnterBackground() }
                }
                .preferredColorScheme(model.theme.colorScheme)
                // The accent color follows the cluster on screen.
                .tint(model.accent)
        }
    }
}

extension ThemeMode {
    var colorScheme: ColorScheme? {
        switch self {
        case .auto: nil
        case .light: .light
        case .dark, .black: .dark
        }
    }
}

struct RootView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var everUnlocked = false

    var body: some View {
        ZStack {
            if everUnlocked || !model.lock.locked {
                content
            }
            if model.lock.locked {
                LockView()
            } else if model.lock.enabled && scenePhase != .active {
                // Keeps cluster data out of the app-switcher snapshot.
                PrivacyCover()
            }
            // The "tap your security key" prompt, over the lock screen or whatever asked for a check.
            SecurityKeyPromptHost()
        }
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .background: model.didEnterBackground()
            case .active: model.willEnterForeground()
            default: break
            }
        }
        .onChange(of: model.lock.locked) { _, locked in
            if !locked { everUnlocked = true }
        }
        // Lock off at launch: keep the content (and its navigation) mounted if it relocks later.
        .onAppear { if !model.lock.locked { everUnlocked = true } }
    }

    @ViewBuilder
    private var content: some View {
        if !model.loaded {
            ProgressView().task { await model.load() }
        } else if !model.unreadable.isEmpty {
            // Fail closed: neither store is shown while one cannot be read.
            ConfigUnreadableView()
        } else if !model.hasConfig {
            NavigationStack { ImportView() }
        } else if !model.lock.enabled && AppLockState.required(for: model.summary?.contexts ?? []) {
            // Right after the first import, or on updating from an optional lock.
            LockOnboardingView()
        } else {
            MainNavigation()
        }
    }
}

private struct PrivacyCover: View {
    var body: some View {
        ZStack {
            Color(.systemBackground).ignoresSafeArea()
            Image(systemName: "hexagon").font(.system(size: 56)).foregroundStyle(.orange)
        }
    }
}

enum Route: Hashable {
    case node(NodeRef)
    /// Node screen opened on a tab, or with a reboot/shutdown confirmation (from row swipes).
    case nodeLive(NodeRef)
    case nodePower(NodeRef, PowerAction)
    /// Node screen opened on a tab (a share link).
    case nodeTab(NodeRef, tab: NodeDetailView.Tab)
    /// Every node of a large cluster (see isDenseCluster), as the overview loaded them; filter preselects one.
    case nodes(filter: NodeFilter?, nodes: [NodeOverview])
    /// Every node of a large cluster added from a kubeconfig, as its home loaded them; filter preselects one.
    case kubeNodes(filter: NodeFilter?, nodes: [KubeNodeInfo])
    case logs(node: String, hostname: String, service: String?)
    /// Log of one Kubernetes container (from the Pods tab).
    case containerLogs(node: String, hostname: String, container: LogContainer)
    case insights
    /// PromQL panels from the cluster's Prometheus, Mimir, Thanos or VictoriaMetrics.
    case metrics
    case supportBundle
    /// The operators the cluster runs that Ichor does not show yet, to request (os:admin).
    case integrations
    /// The bundled release history.
    case changelog
    /// The bundled third-party licenses.
    case licenses
    /// The projects the app integrates with, and which ones the cluster runs.
    case supportedIntegrations
    case etcd
    case kubespan
    /// Kubernetes Deployments, StatefulSets and DaemonSets (os:admin).
    case workloads
    /// The Kubernetes screen on a tab, showing one workload, pod or CronJob (a share link).
    case kubernetes(KubeFocus)
    /// Longhorn, Garage and CloudNativePG health (os:admin); hints: catalog ids from the
    /// inventory, downNodes: hostnames Talos reports not ready, for the likely cause; kind: the
    /// system to open on (nil: the first).
    case dataServices(hints: String, downNodes: Set<String>, kind: DataServiceKind? = nil)
    /// Argo CD Applications (os:admin); downNodes as for dataServices.
    case argoCD(downNodes: Set<String>)
    /// Every Argo CD sync window, freezes first (from a freeze reminder).
    case argoWindows
    /// One Argo CD Application (a share link; the list pushes its own ArgoAppRoute).
    case argoApp(namespace: String, name: String)
    /// One Flux object (a share link; the list pushes its own FluxAppRoute).
    case fluxApp(kind: String, namespace: String, name: String)
    /// Flux Kustomizations, HelmReleases and sources (os:admin); downNodes as for dataServices.
    case flux(downNodes: Set<String>)
    /// The cluster checkup (a share link; the Kubernetes screens push their own).
    case checkup
    case health
    case settings
    case importConfig
    /// The imported clusters: switch, color, remove, add.
    case clusters
    case debugShell(node: String, hostname: String)
    /// The drain of a node (the maintenance screen, drain only); on a cluster without Talos,
    /// node and hostname are its Kubernetes name.
    case drain(node: String, hostname: String)
    /// Events timeline for one node, or all of them (node nil); hostnames by address.
    case events(node: String?, hostnames: [String: String])
    /// The apps running in the cluster; hostnames by address, for the nodes pods run on;
    /// filter: the chip it opens on.
    case apps(hostnames: [String: String], filter: AppFilter = .all)
    /// Issue a talosconfig (os:admin): renew this device's certificate, or one for another device.
    case issueConfig(renew: Bool)
    /// AI diagnosis (optional, see AISettings); note: what the opening screen already knows.
    case diagnosis(note: String)
    /// The action audit log; cluster: a context name, "" for every cluster.
    case activity(cluster: String)
}

/// The home screen: the Talos overview, or the Kubernetes home of a cluster added from a kubeconfig.
private struct HomeView: View {
    @Binding var path: [Route]
    @Environment(AppModel.self) private var model

    var body: some View {
        if model.activeIsKube {
            KubeHomeView(path: $path)
        } else {
            OverviewView(path: $path)
        }
    }
}

struct NodeRef: Hashable {
    let address: String
    let hostname: String
    let role: String
}

struct MainNavigation: View {
    @Environment(AppModel.self) private var model
    @State private var path: [Route] = []
    /// The outcome of "+1 h" from a freeze reminder.
    @State private var freezeMessage: String?
    /// Why a share link opened nothing.
    @State private var linkMessage: String?

    var body: some View {
        NavigationStack(path: $path) {
            HomeView(path: $path)
                .navigationDestination(for: Route.self) { route in
                    switch route {
                    case .node(let ref): NodeDetailView(ref: ref)
                    case .nodeLive(let ref): NodeDetailView(ref: ref, initialTab: .live)
                    case .nodePower(let ref, let action): NodeDetailView(ref: ref, initialAction: action)
                    case .nodeTab(let ref, let tab): NodeDetailView(ref: ref, initialTab: tab)
                    case .nodes(let filter, let nodes): NodesView(nodes: nodes, filter: filter, path: $path)
                    case .kubeNodes(let filter, let nodes): KubeNodesView(nodes: nodes, filter: filter, path: $path)
                    case .logs(let node, let hostname, let service):
                        LogsView(node: node, hostname: hostname, service: service)
                    case .containerLogs(let node, let hostname, let container):
                        LogsView(node: node, hostname: hostname, service: nil, container: container)
                    case .insights: InsightsView()
                    case .metrics: MetricsView()
                    case .supportBundle: SupportBundleView()
                    case .integrations: IntegrationRequestView()
                    case .changelog: ChangelogView()
                    case .licenses: LicensesView()
                    case .supportedIntegrations: SupportedIntegrationsView()
                    case .etcd: EtcdView()
                    case .kubespan: KubeSpanView()
                    case .workloads: KubernetesView()
                    case .kubernetes(let focus): KubernetesView(focus: focus)
                    case .dataServices(let hints, let downNodes, let kind): DataServicesView(hints: hints, downNodes: downNodes, selected: kind)
                    case .argoCD(let downNodes): ArgoCDView(downNodes: downNodes)
                    case .argoWindows: ArgoWindowsView()
                    case .argoApp(let namespace, let name): ArgoAppView(namespace: namespace, name: name, downNodes: [])
                    case .fluxApp(let kind, let namespace, let name): FluxAppView(kind: kind, namespace: namespace, name: name, downNodes: [])
                    case .flux(let downNodes): FluxView(downNodes: downNodes)
                    case .checkup: CheckupView()
                    case .health: HealthView()
                    case .settings: SettingsView()
                    case .diagnosis(let note): DiagnosisView(initialNote: note)
                    case .importConfig: ImportView { path.removeAll() }
                    case .clusters: ClustersView()
                    case .debugShell(let node, let hostname): DebugShellView(node: node, hostname: hostname)
                    case .drain(let node, let hostname): MaintenanceView(node: node, hostname: hostname, drainOnly: true)
                    case .events(let node, let hostnames): EventsView(node: node, hostnames: hostnames)
                    case .apps(let hostnames, let filter): AppsView(hostnames: hostnames, filter: filter)
                    case .issueConfig(let renew): IssueConfigView(initialMode: renew ? .renew : .otherDevice)
                    case .activity(let cluster): ActivityLogView(cluster: cluster)
                    }
                }
        }
        // Wake-on-LAN settings and results, for the node menus of every screen.
        .wakeOnLanPresenter()
        .onChange(of: NotificationRouter.shared.pendingRenewal) { _, pending in
            if pending { openRenewal() }
        }
        .onChange(of: NotificationRouter.shared.pendingCluster) { _, pending in
            if pending != nil { openCluster() }
        }
        .onChange(of: NotificationRouter.shared.pendingArgoWindows) { _, pending in
            if pending { openArgoWindows() }
        }
        .onChange(of: NotificationRouter.shared.pendingShareLink) { _, pending in
            if pending != nil { openShareLink() }
        }
        // Mounted under the lock screen once unlocked: a link waits for the next unlock.
        .onChange(of: model.lock.locked) { _, locked in
            if !locked, NotificationRouter.shared.pendingShareLink != nil { openShareLink() }
            if !locked { openImport() }
        }
        .onChange(of: NotificationRouter.shared.pendingImportText) { _, pending in
            if pending != nil { openImport() }
        }
        .onChange(of: NotificationRouter.shared.pendingGitOps) { _, pending in
            if pending != nil { openGitOps() }
        }
        .onAppear {
            if NotificationRouter.shared.pendingRenewal { openRenewal() }
            if NotificationRouter.shared.pendingCluster != nil { openCluster() }
            if NotificationRouter.shared.pendingArgoWindows { openArgoWindows() }
            if NotificationRouter.shared.pendingShareLink != nil { openShareLink() }
            if NotificationRouter.shared.pendingGitOps != nil { openGitOps() }
            openImport()
        }
        .messageAlert($freezeMessage)
        .messageAlert($linkMessage)
    }

    /// From a freeze reminder: the sync windows, after "+1 h" on the freeze when it was chosen.
    private func openArgoWindows() {
        NotificationRouter.shared.pendingArgoWindows = false
        let extend = NotificationRouter.shared.pendingFreezeExtend
        NotificationRouter.shared.pendingFreezeExtend = nil
        if path.last != .argoWindows { path.append(.argoWindows) }
        guard let extend else { return }
        // Only on the freeze's own cluster: another one is on screen, say so.
        guard model.activeSummary?.fingerprint == extend.cluster else {
            freezeMessage = String(localized: "Switch Ichor to the cluster of this freeze to extend it.")
            return
        }
        guard let client = model.client else {
            freezeMessage = String(localized: "Could not extend the freeze: \(String(localized: "the cluster cannot be reached now."))")
            return
        }
        Task {
            let project = extend.project, namespace = extend.namespace
            let options = ArgoFreezeOptions(minutes: freezeExtendMinutes, window: extend.window)
            do {
                try await client.argoFreeze(namespace: namespace, project: project, action: .extend, options: options)
                let until = Date(epochMillis: extend.end).addingTimeInterval(TimeInterval(freezeExtendMinutes * 60))
                freezeMessage = String(localized: "Freeze extended until \(until.formatted(date: .omitted, time: .shortened)).")
                // The windows screen and the next reminder (the extended window has a new id).
                let key = model.argoKey, cluster = model.activeSummary
                _ = try? await ArgoCDStore.shared.load(with: client, key: key, cluster: cluster)
            } catch {
                freezeMessage = String(localized: "Could not extend the freeze: \(error.localizedDescription)")
            }
        }
    }

    /// From a quick action: that cluster's overview, no screen of the previous one over it.
    private func openCluster() {
        guard let fingerprint = NotificationRouter.shared.pendingCluster else { return }
        NotificationRouter.shared.pendingCluster = nil
        if model.selectCluster(fingerprint: fingerprint) { path = [] }
    }

    /// From a share link: like a quick action, the overview of the cluster it names (the
    /// context on screen when it is one of that cluster), then the screen it names over it.
    private func openShareLink() {
        guard !model.lock.locked, let url = NotificationRouter.shared.pendingShareLink else { return }
        NotificationRouter.shared.pendingShareLink = nil
        Task {
            guard let target = try? await TalosClient.parseShareLink(url) else {
                linkMessage = String(localized: "Not a valid Ichor link")
                return
            }
            guard let context = model.summary?.contexts.context(forCluster: target.cluster, active: model.activeContext),
                  model.selectCluster(fingerprint: context.fingerprint) else {
                linkMessage = String(localized: "This link is for a cluster that isn’t on this phone")
                return
            }
            path = []
            if let route = await target.route(client: model.client, kube: model.activeIsKube) { path = [route] }
        }
    }

    /// From a config file opened with Ichor: the import screen, which previews it (and takes
    /// the text from NotificationRouter itself, also when it is already on screen).
    private func openImport() {
        guard !model.lock.locked, NotificationRouter.shared.pendingImportText != nil, path.last != .importConfig else { return }
        path.append(.importConfig)
    }

    /// From a GitOps app alert: the Argo CD or Flux screen of the cluster on screen, when this
    /// config may use the Kubernetes API.
    private func openGitOps() {
        guard let destination = NotificationRouter.shared.pendingGitOps else { return }
        NotificationRouter.shared.pendingGitOps = nil
        guard model.allows(.workloads) else { return }
        let route: Route = switch destination {
        case .argoCD: .argoCD(downNodes: [])
        case .flux: .flux(downNodes: [])
        }
        if path.last != route { path.append(route) }
    }

    /// From the certificate-expiry alert: the renewal screen, or the settings (which show the
    /// expiry) when this config may not issue certificates.
    private func openRenewal() {
        NotificationRouter.shared.pendingRenewal = false
        path = [model.allows(.issueConfig) ? .issueConfig(renew: true) : .settings]
    }
}
