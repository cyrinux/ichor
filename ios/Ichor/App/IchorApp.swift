import CoreSpotlight
import SwiftUI
import Ichorgo
import IchorCore
import UserNotifications

@main
struct IchorApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var model = AppModel()
    @State private var support = SupportPrompt()
    @State private var ai = AISettings()
    @State private var gitHosts = GitHostStore()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        // First, so the startup trace (when on) sees the whole launch.
        TalosClient.applyStoredStartupTrace()
        StartupTrace.markOnce("app.init start")
        // Before any Talos call, including the background refresh task registered below.
        TalosClient.applyStoredPrivacyMask()
        TalosClient.applyStoredAuditEvents()
        TalosClient.setDataDirectory()
        // Where Go keeps the sign-ins of kubeconfig clusters, before any Kubernetes call.
        KubeAuthStore.register()
        // GKE's Sign in with Google, when the build carries Ichor's iOS client
        // (Config/GoogleSignIn.xcconfig): the option appears on the GKE sign-in sheet.
        if let clientID = Bundle.main.object(forInfoDictionaryKey: "GoogleSignInClientID") as? String, !clientID.isEmpty {
            IchorgoSetGoogleSignInClient("ios", clientID)
        }
        SupportBundleStore.removeStaleParts()
        BackgroundMonitor.register()
        BackgroundMonitor.registerCategories()
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
        // The runs' Live Activities, and their buttons' handler.
        OperationActivities.start()
        StartupTrace.markOnce("app.init end")
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .environment(support)
                .environment(ai)
                .environment(gitHosts)
                .task { support.onLaunch() }
                // A share link or a config file while the app runs (the scene delegate takes them at launch).
                .onOpenURL { url in
                    if url.isFileURL { IncomingConfig.receive(url) }
                    if let kind = OperationKind(link: url) {
                        // A run's Live Activity: its screen.
                        NotificationRouter.shared.pendingOperation = kind
                    } else if url.scheme == "ichor", url.host == "open" {
                        NotificationRouter.shared.pendingShareLink = url
                    }
                }
                // A Spotlight result (SpotlightIndex): its share link, routed like any other.
                .onContinueUserActivity(CSSearchableItemActionType) { activity in
                    if let url = SpotlightIndex.link(of: activity) { NotificationRouter.shared.pendingShareLink = url }
                }
                .onChange(of: scenePhase) { _, phase in
                    if phase == .background, BackgroundMonitor.alertsEnabled { BackgroundMonitor.schedule() }
                    if phase == .background { UpgradeJob.shared.didEnterBackground() }
                    if phase == .background { MaintenanceJob.shared.didEnterBackground() }
                    if phase == .background { JoinJob.shared.didEnterBackground() }
                    if phase == .background { ClusterUpgradeJob.shared.didEnterBackground() }
                    if phase == .background { K8sUpgradeJob.shared.didEnterBackground() }
                    if phase == .background { ConfigTryJob.shared.didEnterBackground() }
                    if phase == .active { ConfigTryJob.shared.willEnterForeground() }
                    if phase == .background { ConfigMultiJob.shared.didEnterBackground() }
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
    /// Node tabs opened on Live; node hub with a reboot/shutdown confirmation (from row swipes).
    case nodeLive(NodeRef)
    case nodePower(NodeRef, PowerAction)
    /// Every node to a Talos version, one after the other ("": the newest stable release).
    case clusterUpgrade(version: String)
    /// The Kubernetes upgrade (talosctl upgrade-k8s).
    case k8sUpgrade
    /// "Add a node…": a node in maintenance mode, booted from the Talos ISO.
    case joinNode
    /// Node hub opened on its cordon / uncordon confirmation (from a row's menu).
    case nodeCordon(NodeRef)
    /// Node tabs opened on a tab (a hub row, a share link that names a tab).
    case nodeTab(NodeRef, tab: NodeTabsView.Tab)
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
    /// The guided replacement of a failed etcd member; node: its address as last seen.
    case replaceControlPlane(memberId: String, node: String, hostname: String)
    case kubespan
    /// Kubernetes Deployments, StatefulSets and DaemonSets (os:admin).
    case workloads
    /// The Kubernetes screen on a tab, showing one workload, pod or CronJob (a share link).
    case kubernetes(KubeFocus)
    /// The Kubernetes screen on its network test (a followed test's Live Activity).
    case networkTest
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
    /// The directory dir (from the repository root) of an app's source, at the commit the cluster runs.
    case gitOpsSource(GitSource, dir: String)
    /// One file of an app's source, read only.
    case gitOpsFile(GitSource, path: String)
    /// Flux Kustomizations, HelmReleases and sources (os:admin); downNodes as for dataServices.
    case flux(downNodes: Set<String>)
    /// The cluster checkup (a share link; the Kubernetes screens push their own).
    case checkup
    /// The Alertmanager's alerts and silences (os:admin).
    case alerts
    case health
    case settings
    /// One group of Settings on its own page.
    case settingsPage(SettingsPage)
    case importConfig
    /// The imported clusters: switch, color, remove, add.
    case clusters
    case debugShell(node: String, hostname: String)
    /// A node's Storage screen: volumes, mounts and disks' health (a share link, a storage alert).
    case storage(node: String, hostname: String)
    /// A node's Hardware screen: sensors, CPUs and PCI devices (a share link, a temperature alert).
    case hardware(node: String, hostname: String)
    /// The drain of a node (the maintenance screen, drain only); on a cluster without Talos,
    /// node and hostname are its Kubernetes name.
    case drain(node: String, hostname: String)
    /// A node's upgrade, maintenance or machine config screen, where its followed run shows
    /// (a run's Live Activity).
    case upgrade(node: String, hostname: String)
    case maintenance(node: String, hostname: String)
    case machineConfig(node: String, hostname: String)
    /// A root shell on a node of a cluster without Talos, by its Kubernetes name.
    case kubeNodeDebug(node: String)
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
    /// Every cluster's nodes, pods, workloads and GitOps apps, as last seen.
    case search
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

extension Route {
    /// The node a share link opens, on its hub or on one of its tabs; nil for any other screen.
    var nodeRef: NodeRef? {
        switch self {
        case .node(let ref), .nodeTab(let ref, _): ref
        default: nil
        }
    }
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
                    case .nodeLive(let ref): NodeTabsView(ref: ref, initialTab: .live)
                    case .nodePower(let ref, let action): NodeDetailView(ref: ref, initialAction: action)
                    case .clusterUpgrade(let version): ClusterUpgradePlanView(initialVersion: version)
                    case .k8sUpgrade: K8sUpgradePlanView()
                    case .joinNode: JoinNodeView(path: $path)
                    case .nodeCordon(let ref): NodeDetailView(ref: ref, initialCordon: true)
                    case .nodeTab(let ref, let tab): NodeTabsView(ref: ref, initialTab: tab)
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
                    case .replaceControlPlane(let memberId, let node, let hostname):
                        ReplaceControlPlaneView(memberId: memberId, node: node, hostname: hostname)
                    case .kubespan: KubeSpanView()
                    case .workloads: KubernetesView()
                    case .kubernetes(let focus): KubernetesView(focus: focus)
                    case .networkTest: KubernetesView(networkTest: true)
                    case .dataServices(let hints, let downNodes, let kind): DataServicesView(hints: hints, downNodes: downNodes, selected: kind)
                    case .argoCD(let downNodes): ArgoCDView(downNodes: downNodes)
                    case .argoWindows: ArgoWindowsView()
                    case .argoApp(let namespace, let name): ArgoAppView(namespace: namespace, name: name, downNodes: [])
                    case .fluxApp(let kind, let namespace, let name): FluxAppView(kind: kind, namespace: namespace, name: name, downNodes: [])
                    case .gitOpsSource(let source, let dir): SourceBrowserView(source: source, dir: dir)
                    case .gitOpsFile(let source, let path): SourceFileView(source: source, path: path)
                    case .flux(let downNodes): FluxView(downNodes: downNodes)
                    case .checkup: CheckupView()
                    case .alerts: AlertsView(path: $path)
                    case .health: HealthView()
                    case .settings: SettingsView()
                    case .settingsPage(let page): SettingsPageView(page: page)
                    case .diagnosis(let note): DiagnosisView(initialNote: note)
                    case .importConfig: ImportView { path.removeAll() }
                    case .clusters: ClustersView()
                    case .debugShell(let node, let hostname): DebugShellView(node: node, hostname: hostname)
                    case .storage(let node, let hostname): StorageView(node: node, hostname: hostname)
                    case .hardware(let node, let hostname): HardwareView(node: node, hostname: hostname)
                    case .drain(let node, let hostname): MaintenanceView(node: node, hostname: hostname, drainOnly: true)
                    case .upgrade(let node, let hostname): UpgradeView(node: node, hostname: hostname)
                    case .maintenance(let node, let hostname): MaintenanceView(node: node, hostname: hostname)
                    case .machineConfig(let node, let hostname): MachineConfigView(node: node, hostname: hostname)
                    case .kubeNodeDebug(let node): KubeNodeDebugView(node: node)
                    case .events(let node, let hostnames): EventsView(node: node, hostnames: hostnames)
                    case .apps(let hostnames, let filter): AppsView(hostnames: hostnames, filter: filter)
                    case .issueConfig(let renew): IssueConfigView(initialMode: renew ? .renew : .otherDevice)
                    case .activity(let cluster): ActivityLogView(cluster: cluster)
                    case .search: SearchView { target in Task { await open(target) } }
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
        .onChange(of: NotificationRouter.shared.pendingSearch) { _, pending in
            if pending { openSearch() }
        }
        .onChange(of: NotificationRouter.shared.pendingOperation) { _, pending in
            if pending != nil { openOperation() }
        }
        // Mounted under the lock screen once unlocked: a link waits for the next unlock.
        .onChange(of: model.lock.locked) { _, locked in
            if !locked, NotificationRouter.shared.pendingShareLink != nil { openShareLink() }
            if !locked, NotificationRouter.shared.pendingSearch { openSearch() }
            if !locked, NotificationRouter.shared.pendingOperation != nil { openOperation() }
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
            if NotificationRouter.shared.pendingSearch { openSearch() }
            if NotificationRouter.shared.pendingGitOps != nil { openGitOps() }
            if NotificationRouter.shared.pendingOperation != nil { openOperation() }
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

    /// From a run's Live Activity: the screen that shows the run, once unlocked; the app as it
    /// was for a run with no screen of its own (an image pull or scan) or one already cleared.
    private func openOperation() {
        guard !model.lock.locked, let kind = NotificationRouter.shared.pendingOperation else { return }
        NotificationRouter.shared.pendingOperation = nil
        guard let route = operationRoute(kind), path.last != route else { return }
        path.append(route)
    }

    private func operationRoute(_ kind: OperationKind) -> Route? {
        switch kind {
        case .upgrade:
            guard let target = UpgradeJob.shared.target else { return nil }
            return Route.upgrade(node: target.node, hostname: target.hostname)
        case .maintenance:
            guard let target = MaintenanceJob.shared.target else { return nil }
            return target.kube ? Route.drain(node: target.node, hostname: target.hostname)
                : Route.maintenance(node: target.node, hostname: target.hostname)
        case .configTry:
            guard let target = ConfigTryJob.shared.target else { return nil }
            return Route.machineConfig(node: target.node, hostname: target.hostname)
        case .clusterUpgrade:
            guard let version = ClusterUpgradeJob.shared.version else { return nil }
            return Route.clusterUpgrade(version: version)
        case .k8sUpgrade:
            return K8sUpgradeJob.shared.version == nil ? nil : Route.k8sUpgrade
        case .join:
            return JoinJob.shared.request == nil ? nil : Route.joinNode
        case .argoSync:
            guard let run = GitOpsOperationJob.shared.argo else { return nil }
            return Route.argoApp(namespace: run.namespace, name: run.name)
        case .fluxReconcile:
            guard let target = GitOpsOperationJob.shared.flux?.target else { return nil }
            return Route.fluxApp(kind: target.kind, namespace: target.namespace, name: target.name)
        case .rollout:
            guard let w = RolloutJob.shared.workload else { return nil }
            return Route.kubernetes(KubeFocus(tab: .workloads, id: w.id, namespace: w.namespace, name: w.name))
        case .netPerf:
            return NetPerfJob.shared.run == nil ? nil : Route.networkTest
        case .configMulti, .imagePull, .imageScan:
            return nil
        }
    }

    /// From the "Search clusters" quick action: the search over the cluster on screen's home,
    /// once unlocked.
    private func openSearch() {
        guard !model.lock.locked, NotificationRouter.shared.pendingSearch else { return }
        NotificationRouter.shared.pendingSearch = false
        path = [.search]
    }

    /// From a share link: like a quick action, the overview of the cluster it names (the
    /// context on screen when it is one of that cluster), then the screen it names over it.
    private func openShareLink() {
        guard !model.lock.locked, let url = NotificationRouter.shared.pendingShareLink else { return }
        NotificationRouter.shared.pendingShareLink = nil
        // An alert's Reboot, Sync…: its screen asks for it (none for a plain tap or link).
        let action = NotificationRouter.shared.pendingAlertAction
        NotificationRouter.shared.pendingAlertAction = nil
        NotificationRouter.shared.alertActionRequest = nil
        Task {
            guard let target = try? await TalosClient.parseShareLink(url) else {
                linkMessage = String(localized: "Not a valid Ichor link")
                return
            }
            await open(target, action: action)
        }
    }

    /// The overview of `target`'s cluster (the context on screen when it is one of that
    /// cluster), then the screen it names over it: a share link, a search result.
    private func open(_ target: ShareTarget, action: AlertActionRequest? = nil) async {
        guard let context = model.summary?.contexts.context(forCluster: target.cluster, active: model.activeContext),
              model.selectCluster(fingerprint: context.fingerprint) else {
            linkMessage = String(localized: "This link is for a cluster that isn’t on this phone")
            return
        }
        path = []
        guard let route = await target.route(client: model.client, kube: model.activeIsKube) else { return }
        path = [actionRoute(route, for: action)]
        if action?.action != .reboot { NotificationRouter.shared.alertActionRequest = action }
    }

    /// The node hub with its reboot confirmation for an alert's Reboot (when this config may
    /// power nodes); `route` otherwise.
    private func actionRoute(_ route: Route, for action: AlertActionRequest?) -> Route {
        guard let action, let ref = route.nodeRef, action.isReboot(node: ref.address), model.allows(.power) else { return route }
        return .nodePower(ref, .reboot)
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
