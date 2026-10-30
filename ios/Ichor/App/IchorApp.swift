import CoreSpotlight
import SwiftUI
import Ichorgo
import IchorCore
import UserNotifications

@main
struct IchorApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @State private var model = AppModel(
        settings: SettingsStore(), lock: LockStore(), nodes: NodesStore(), git: ClusterGitStore())
    @State private var support = SupportPrompt()
    @State private var ai = AISettings()
    @State private var gitHosts = GitHostStore()
    @State private var stagedEdits = StagedEdits()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        // First, so the startup trace (when on) and the diagnostics log see the whole launch.
        TalosClient.startDiagStartupTrace()
        TalosClient.applyStoredStartupTrace()
        StartupTrace.markOnce("app.init start")
        // Before any Talos call, including the background refresh task registered below.
        TalosClient.applyStoredPrivacyMask()
        TalosClient.applyStoredAuditEvents()
        TalosClient.setDataDirectory()
        // The diagnostics log's files live in the data directory; MetricKit's reports go to it.
        TalosClient.applyStoredDiagPersist()
        CrashDiagnostics.start()
        // Where Go keeps the sign-ins of kubeconfig clusters, before any Kubernetes call.
        KubeAuthStore.register()
        // GKE's Sign in with Google, when the build carries Ichor's iOS client
        // (Config/GoogleSignIn.xcconfig): the option appears on the GKE sign-in sheet.
        if let clientID = Bundle.main.object(forInfoDictionaryKey: "GoogleSignInClientID") as? String, !clientID.isEmpty
        {
            IchorgoSetGoogleSignInClient("ios", clientID)
        }
        // The on-device model: nothing is loaded until the user asks it a question.
        IchorgoSetLocalModel(LocalModelHost.shared)
        // A model download started before a relaunch goes on in its background session.
        ModelDownloadSession.shared.resume()
        SupportBundleStore.removeStaleParts()
        BackgroundMonitor.register()
        BackgroundMonitor.registerCategories()
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
        // The runs' Live Activities, and their buttons' handler.
        OperationActivities.start()
        // "Open cluster" and "Run a checkup" (Shortcuts, Siri, Control Center).
        ClusterScreenLinks.install()
        // The Apple Watch's link: ready before a background run sends it the last check.
        WatchLink.shared.activate()
        // TipKit, for the demo tour's coach marks (DemoTour).
        DemoTour.configure()
        StartupTrace.markOnce("app.init end")
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .environment(model.settings)
                .environment(model.lockStore)
                .environment(model.nodes)
                .environment(model.git)
                .environment(support)
                .environment(ai)
                .environment(gitHosts)
                .environment(stagedEdits)
                .task { support.onLaunch() }
                // Files sent with "Send to Ichor" while the app was closed (scenePhase's first
                // .active may come before the root's onChange is attached).
                .task { IncomingConfig.receivePending() }
                // A share link or a config file while the app runs (the scene delegate takes them at launch).
                .onOpenURL { url in
                    if url.isFileURL { IncomingConfig.receive(url) }
                    if let kind = OperationKind(link: url) {
                        // A run's Live Activity: its screen.
                        NotificationRouter.shared.pendingOperation = kind
                    } else if isFleetLink(url) {
                        // The fleet widget: the fleet screen.
                        NotificationRouter.shared.pendingFleet = true
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
                    if phase == .active { ForegroundSignInRenewal.run(model: model) }
                    // Files sent with "Send to Ichor" while the app was away.
                    if phase == .active { IncomingConfig.receivePending() }
                    if phase == .background { ConfigMultiJob.shared.didEnterBackground() }
                }
                .preferredColorScheme(model.settings.theme.colorScheme)
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
    @Environment(LockStore.self) private var lockStore
    @Environment(SettingsStore.self) private var settings
    @Environment(\.scenePhase) private var scenePhase
    @State private var everUnlocked = false

    var body: some View {
        ZStack {
            if everUnlocked || !lockStore.lock.locked {
                content
                    // Under the cover below: VoiceOver must not read the old names either.
                    .accessibilityHidden(settings.maskTransition)
            }
            // The screenshot mode switching: no screen shows until its names are the new ones.
            if settings.maskTransition {
                MaskTransitionCover()
            }
            if lockStore.lock.locked {
                LockView()
            } else if lockStore.lock.enabled && scenePhase != .active {
                // Keeps cluster data out of the app-switcher snapshot.
                PrivacyCover()
            }
            // The "tap your security key" prompt, over the lock screen or whatever asked for a check.
            SecurityKeyPromptHost()
            // A UI test whose demo could not be added says why (never set in a real launch).
            if let failure = UITestLaunch.shared.failure {
                Text(verbatim: failure)
                    .padding()
                    .background(.red.opacity(0.85))
                    .accessibilityIdentifier(SmokeID.launchError)
            }
        }
        // A UI test's demo, once the configs are loaded (nothing in a real launch).
        .task { await UITestLaunch.shared.prepare(model) }
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .background: lockStore.didEnterBackground()
            case .active: lockStore.willEnterForeground()
            default: break
            }
        }
        .onChange(of: lockStore.lock.locked) { _, locked in
            if !locked { everUnlocked = true }
        }
        // Lock off at launch: keep the content (and its navigation) mounted if it relocks later.
        .onAppear { if !lockStore.lock.locked { everUnlocked = true } }
    }

    @ViewBuilder
    private var content: some View {
        if !model.loaded {
            ProgressView().task { await model.load() }
        } else if !model.unreadable.isEmpty {
            // Fail closed: neither store is shown while one cannot be read.
            ConfigUnreadableView()
        } else if !model.hasConfig {
            // A backup sent to a new phone lands here: restored without asking.
            NavigationStack { ImportView() }
                .restoresReceivedBackup()
        } else if !lockStore.lock.enabled && AppLockState.required(for: model.summary?.contexts ?? []) {
            // Right after the first import, or on updating from an optional lock.
            LockOnboardingView()
        } else {
            MainNavigation()
        }
    }
}

/// Over the screens while the screenshot mode is turned on or off (SettingsStore.maskTransition):
/// its icon and a progress indicator, and no name.
private struct MaskTransitionCover: View {
    var body: some View {
        ZStack {
            Color(.systemBackground).ignoresSafeArea()
            VStack(spacing: 16) {
                Image(systemName: "eye.slash")
                    .font(.system(size: 44))
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                ProgressView()
                Text("Switching names…")
                    .font(.headline)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
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
    /// Step 0 of Add a node (and all of it on Omni): the booter command, ISO and iPXE links that start a new node.
    case bootMedia
    /// Node hub opened on its cordon / uncordon confirmation (from a row's menu).
    case nodeCordon(NodeRef)
    /// Node tabs opened on a tab (a hub row, a share link that names a tab).
    case nodeTab(NodeRef, tab: NodeTabsView.Tab)
    /// Every node of a large cluster (see isDenseCluster), as the overview loaded them; filter preselects one.
    /// On an iPad, every cluster's, beside the selected node's hub (a link to a node selects it).
    case nodes(filter: NodeFilter?, nodes: [NodeOverview], selected: NodeRef? = nil)
    /// Every node of a large cluster added from a kubeconfig, as its home loaded them; filter preselects one,
    /// query fills the search (a node or disk alert names its node).
    case kubeNodes(filter: NodeFilter?, nodes: [KubeNodeInfo], query: String = "")
    case logs(node: String, hostname: String, service: String?)
    /// Log of one Kubernetes container (from the Pods tab).
    case containerLogs(node: String, hostname: String, container: LogContainer)
    case insights
    /// Free room per node and N+1 (os:admin), from the Talos overview; the Kubernetes home and
    /// the checkup open their own.
    case capacity
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
    /// system to open on (nil: the first); solo: that system alone, for one the Kubernetes home
    /// files in another family.
    case dataServices(hints: String, downNodes: Set<String>, kind: DataServiceKind? = nil, solo: Bool = false)
    /// The Data services screen on a CRD plugin's tab (a plugin tile); hints and downNodes as
    /// for dataServices.
    case dataServicesPlugin(hints: String, downNodes: Set<String>, plugin: String)
    /// A CRD plugin of another category than Data services, on a screen of its own (a tile of
    /// the Kubernetes home).
    case plugin(id: String)
    /// Argo CD Applications (os:admin); downNodes as for dataServices; selected: the app shown
    /// beside the list on an iPad (a link to an app).
    case argoCD(downNodes: Set<String>, selected: ArgoAppRoute? = nil)
    /// Every Argo CD sync window, freezes first (from a freeze reminder).
    case argoWindows
    /// One Argo CD Application (a share link; the list pushes its own ArgoAppRoute).
    case argoApp(namespace: String, name: String)
    /// One Flux object (a share link; the list pushes its own FluxAppRoute).
    case fluxApp(kind: String, namespace: String, name: String)
    /// The directory dir (from the repository root) of an app's source, at the commit the cluster runs.
    case gitOpsSource(GitSource, dir: String, app: GitOpsAppRef)
    /// One file of an app's source, read only.
    case gitOpsFile(GitSource, path: String, app: GitOpsAppRef)
    /// Flux Kustomizations, HelmReleases and sources (os:admin); downNodes as for dataServices;
    /// selected: the object shown beside the list on an iPad (a link to one).
    case flux(downNodes: Set<String>, selected: FluxAppRoute? = nil)
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
    /// A node's taints and labels; on a cluster without Talos, node and hostname are its
    /// Kubernetes name.
    case nodeMeta(node: String, hostname: String)
    /// Events timeline for one node, or all of them (node nil); hostnames by address.
    case events(node: String?, hostnames: [String: String])
    /// The apps running in the cluster; hostnames by address, for the nodes pods run on;
    /// filter: the chip it opens on.
    case apps(hostnames: [String: String], filter: AppFilter = .all)
    /// Issue a talosconfig (os:admin): renew this device's certificate, or one for another device.
    case issueConfig(renew: Bool)
    case rotateCA
    /// AI diagnosis (optional, see AISettings); note: what the opening screen already knows.
    case diagnosis(note: String)
    /// The action audit log; cluster: a context name, "" for every cluster.
    case activity(cluster: String)
    /// The redacted diagnostics log and MetricKit's crash reports, exported as a zip.
    case diagnostics
    /// Every cluster's nodes, pods, workloads and GitOps apps, as last seen.
    case search
    /// Every cluster's last background check, worst first ("All clusters").
    case fleet
    /// The cluster's runbooks (docs/runbooks.md), one runbook's plan, and its run.
    case runbooks
    /// The images and charts with a newer version (the cluster menu's "Updates…").
    case updates
    case runbookPlan(id: String)
    case runbookRun(id: String)
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
    @Environment(LockStore.self) private var lockStore
    @Environment(SettingsStore.self) private var settings
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var path: [Route] = []
    /// The outcome of "+1 h" from a freeze reminder.
    @State private var freezeMessage: String?
    /// Why a share link opened nothing.
    @State private var linkMessage: String?

    var body: some View {
        shell
            // Wake-on-LAN settings and results, for the node menus of every screen.
            .wakeOnLanPresenter()
            // The screenshot mode switching (under its cover): the screens pushed with the old names
            // go, the settings pages the switch was flipped on stay.
            .onChange(of: settings.maskTransition) { _, switching in
                guard switching else { return }
                path = pathKeptOnMaskSwitch(path) { route in
                    if case .settingsPage = route { return true }
                    return route == .settings
                }
            }
            .onChange(of: NotificationRouter.shared.pendingRenewal) { _, pending in
                if pending { openRenewal() }
            }
            .onChange(of: NotificationRouter.shared.pendingCluster) { _, pending in
                if pending != nil { openCluster() }
            }
            .onChange(of: NotificationRouter.shared.pendingHome) { _, pending in
                guard pending else { return }
                NotificationRouter.shared.pendingHome = false
                path = []
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
            .onChange(of: NotificationRouter.shared.pendingFleet) { _, pending in
                if pending { openFleet() }
            }
            .onChange(of: NotificationRouter.shared.pendingOperation) { _, pending in
                if pending != nil { openOperation() }
            }
            // Mounted under the lock screen once unlocked: a link waits for the next unlock.
            .onChange(of: lockStore.lock.locked) { _, locked in
                if !locked, NotificationRouter.shared.pendingShareLink != nil { openShareLink() }
                if !locked, NotificationRouter.shared.pendingSearch { openSearch() }
                if !locked, NotificationRouter.shared.pendingFleet { openFleet() }
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
                if NotificationRouter.shared.pendingFleet { openFleet() }
                if NotificationRouter.shared.pendingGitOps != nil { openGitOps() }
                if NotificationRouter.shared.pendingOperation != nil { openOperation() }
                openImport()
            }
            .messageAlert($freezeMessage)
            .messageAlert($linkMessage)
            .restoresReceivedBackup()
    }

    /// A regular-width iPad: the clusters and sections in a sidebar beside the stack. Elsewhere
    /// (iPhone, a narrow Split View or Slide Over) the stack alone. Both share `path`, so a
    /// resize keeps the screens open.
    @ViewBuilder private var shell: some View {
        if UIDevice.current.userInterfaceIdiom == .pad && sizeClass == .regular {
            SidebarNavigation(path: $path) { stack }
        } else {
            stack
        }
    }

    private var stack: some View {
        NavigationStack(path: $path) {
            HomeView(path: $path)
                .navigationDestination(for: Route.self) { destination($0) }
        }
    }

    /// The screen of each route, for both shells.
    @ViewBuilder private func destination(_ route: Route) -> some View {
        switch route {
        case .node(let ref):
            NodeDetailView(ref: ref).recordsOpened(
                .node(address: ref.address, hostname: ref.hostname, tab: ""), label: ref.hostname)
        case .nodeLive(let ref):
            NodeTabsView(ref: ref, initialTab: .live).recordsOpened(
                .node(address: ref.address, hostname: ref.hostname, tab: ""), label: ref.hostname)
        case .nodePower(let ref, let action): NodeDetailView(ref: ref, initialAction: action)
        case .clusterUpgrade(let version): ClusterUpgradePlanView(initialVersion: version)
        case .k8sUpgrade: K8sUpgradePlanView()
        case .joinNode:
            // Omni joins the machine itself: Add a node is only its boot media there.
            if model.activeSummary?.omni == true { BootMediaView(path: $path) } else { JoinNodeView(path: $path) }
        case .bootMedia: BootMediaView(path: $path)
        case .nodeCordon(let ref): NodeDetailView(ref: ref, initialCordon: true)
        case .nodeTab(let ref, let tab):
            NodeTabsView(ref: ref, initialTab: tab).recordsOpened(
                .node(address: ref.address, hostname: ref.hostname, tab: ""), label: ref.hostname)
        case .nodes(let filter, let nodes, let selected):
            NodesView(nodes: nodes, filter: filter, selected: selected, path: $path)
        case .kubeNodes(let filter, let nodes, let query):
            KubeNodesView(nodes: nodes, filter: filter, query: query, path: $path)
        case .logs(let node, let hostname, let service):
            LogsView(node: node, hostname: hostname, service: service)
        case .containerLogs(let node, let hostname, let container):
            LogsView(node: node, hostname: hostname, service: nil, container: container)
        case .insights: InsightsView()
        case .capacity: KubeCapacityView()
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
        case .kubernetes(let focus):
            KubernetesView(focus: focus).recordsOpened(focus.shareTarget, label: focus.name, detail: focus.namespace)
        case .networkTest: KubernetesView(networkTest: true)
        case .dataServices(let hints, let downNodes, let kind, let solo):
            DataServicesView(hints: hints, downNodes: downNodes, selected: kind, solo: solo)
        case .dataServicesPlugin(let hints, let downNodes, let plugin):
            DataServicesView(hints: hints, downNodes: downNodes, plugin: plugin)
        case .plugin(let id): PluginView(pluginID: id)
        case .argoCD(let downNodes, let selected): ArgoCDView(downNodes: downNodes, selected: selected)
        case .argoWindows: ArgoWindowsView()
        case .argoApp(let namespace, let name):
            ArgoAppView(namespace: namespace, name: name, downNodes: [])
                .recordsOpened(.argoApp(namespace: namespace, name: name), label: name, detail: namespace)
        case .fluxApp(let kind, let namespace, let name):
            FluxAppView(kind: kind, namespace: namespace, name: name, downNodes: [])
                .recordsOpened(.fluxApp(kind: kind, namespace: namespace, name: name), label: name, detail: namespace)
        case .gitOpsSource(let source, let dir, let app): SourceBrowserView(source: source, dir: dir, app: app)
        case .gitOpsFile(let source, let path, let app): SourceFileView(source: source, path: path, app: app)
        case .flux(let downNodes, let selected): FluxView(downNodes: downNodes, selected: selected)
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
        case .nodeMeta(let node, let hostname): NodeMetaEditorView(node: node, hostname: hostname)
        case .events(let node, let hostnames): EventsView(node: node, hostnames: hostnames)
        case .apps(let hostnames, let filter): AppsView(hostnames: hostnames, filter: filter)
        case .issueConfig(let renew): IssueConfigView(initialMode: renew ? .renew : .otherDevice)
        case .rotateCA: RotateCAView()
        case .activity(let cluster): ActivityLogView(cluster: cluster)
        case .diagnostics: DiagnosticsView()
        case .search: SearchView(open: { target in Task { await open(target) } }, openRoute: { path.append($0) })
        case .fleet: FleetView(path: $path)
        case .runbooks: RunbooksView(path: $path)
        case .updates: UpdatesView()
        case .runbookPlan(let id): RunbookPlanView(id: id, path: $path)
        case .runbookRun(let id): RunbookRunView(id: id, path: $path)
        }
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
            freezeMessage = String(
                localized: "Could not extend the freeze: \(String(localized: "the cluster cannot be reached now."))")
            return
        }
        Task {
            let project = extend.project
            let namespace = extend.namespace
            let options = ArgoFreezeOptions(minutes: freezeExtendMinutes, window: extend.window)
            do {
                try await client.argoFreeze(namespace: namespace, project: project, action: .extend, options: options)
                let until = Date(epochMillis: extend.end).addingTimeInterval(TimeInterval(freezeExtendMinutes * 60))
                freezeMessage = String(
                    localized: "Freeze extended until \(until.formatted(date: .omitted, time: .shortened)).")
                // The windows screen and the next reminder (the extended window has a new id).
                let key = model.argoKey
                let cluster = model.activeSummary
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
        guard !lockStore.lock.locked, let kind = NotificationRouter.shared.pendingOperation else { return }
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
            return target.kube
                ? Route.drain(node: target.node, hostname: target.hostname)
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
        case .runbook:
            guard let run = RunbookJob.shared.run else { return nil }
            return Route.runbookRun(id: run.id)
        case .caRotation:
            return CARotationJob.shared.state == nil ? nil : Route.rotateCA
        case .configMulti, .imagePull, .imageScan, .podSweep:
            return nil
        }
    }

    /// From the "Search clusters" quick action: the search over the cluster on screen's home,
    /// once unlocked.
    private func openSearch() {
        guard !lockStore.lock.locked, NotificationRouter.shared.pendingSearch else { return }
        NotificationRouter.shared.pendingSearch = false
        path = [.search]
    }

    /// From the fleet widget: the fleet screen over the cluster on screen's home, once unlocked.
    private func openFleet() {
        guard !lockStore.lock.locked, NotificationRouter.shared.pendingFleet else { return }
        NotificationRouter.shared.pendingFleet = false
        path = [.fleet]
    }

    /// From a share link: like a quick action, the overview of the cluster it names (the
    /// context on screen when it is one of that cluster), then the screen it names over it.
    private func openShareLink() {
        guard !lockStore.lock.locked, let url = NotificationRouter.shared.pendingShareLink else { return }
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
            model.selectCluster(fingerprint: context.fingerprint)
        else {
            linkMessage = String(localized: "This link is for a cluster that isn’t on this phone")
            return
        }
        // Not during a screenshot mode switch, nor with a route read under the setting before it:
        // the route carries node names (see pathKeptOnMaskSwitch).
        guard !settings.maskTransition else { return }
        let generation = model.dataGeneration
        path = []
        guard let route = await target.route(client: model.client, kube: model.activeIsKube),
            !settings.maskTransition, generation == model.dataGeneration
        else { return }
        let shown = await splitRoute(actionRoute(route, for: action))
        guard !settings.maskTransition, generation == model.dataGeneration else { return }
        path = [shown]
        if action?.action != .reboot { NotificationRouter.shared.alertActionRequest = action }
    }

    /// On an iPad side by side (ListDetailLayout): a node, an Argo CD app or a Flux object opens
    /// its list with it selected beside it; `route` otherwise, and when the nodes cannot be read.
    private func splitRoute(_ route: Route) async -> Route {
        guard ListDetailLayout.isSplit(sizeClass) else { return route }
        switch route {
        case .argoApp(let namespace, let name):
            return .argoCD(downNodes: [], selected: ArgoAppRoute(namespace: namespace, name: name, downNodes: []))
        case .fluxApp(let kind, let namespace, let name):
            return .flux(
                downNodes: [], selected: FluxAppRoute(kind: kind, namespace: namespace, name: name, downNodes: []))
        case .node(let ref):
            guard !model.activeIsKube, let overview = try? await model.client?.overview(), overview.outage == nil else {
                return route
            }
            return .nodes(filter: nil, nodes: overview.nodes.overviewOrder, selected: ref)
        default:
            return route
        }
    }

    /// The node hub with its reboot confirmation for an alert's Reboot (when this config may
    /// power nodes); `route` otherwise.
    private func actionRoute(_ route: Route, for action: AlertActionRequest?) -> Route {
        guard let action, let ref = route.nodeRef, action.isReboot(node: ref.address), model.allows(.power) else {
            return route
        }
        return .nodePower(ref, .reboot)
    }

    /// From a config file opened with Ichor: the import screen, which previews it (and takes
    /// the text from NotificationRouter itself, also when it is already on screen).
    private func openImport() {
        guard !lockStore.lock.locked, NotificationRouter.shared.pendingImportText != nil,
            path.last != .importConfig
        else {
            return
        }
        path.append(.importConfig)
    }

    /// From a GitOps app alert: the Argo CD or Flux screen of the cluster on screen, when this
    /// config may use the Kubernetes API.
    private func openGitOps() {
        guard let destination = NotificationRouter.shared.pendingGitOps else { return }
        NotificationRouter.shared.pendingGitOps = nil
        guard model.allows(.workloads) else { return }
        let route: Route =
            switch destination {
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
