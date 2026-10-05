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
        } else if model.yaml == nil {
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
    /// Every node of a large cluster (see isDenseCluster), as the overview loaded them; filter preselects one.
    case nodes(filter: NodeFilter?, nodes: [NodeOverview])
    case logs(node: String, hostname: String, service: String?)
    /// Log of one Kubernetes container (from the Pods tab).
    case containerLogs(node: String, hostname: String, container: LogContainer)
    case insights
    /// PromQL panels from the cluster's Prometheus, Mimir, Thanos or VictoriaMetrics.
    case metrics
    case supportBundle
    /// The bundled release history.
    case changelog
    /// The bundled third-party licenses.
    case licenses
    case etcd
    case kubespan
    /// Kubernetes Deployments, StatefulSets and DaemonSets (os:admin).
    case workloads
    /// Longhorn, Garage and CloudNativePG health (os:admin); hints: catalog ids from the
    /// inventory, downNodes: hostnames Talos reports not ready, for the likely cause; kind: the
    /// system to open on (nil: the first).
    case dataServices(hints: String, downNodes: Set<String>, kind: DataServiceKind? = nil)
    /// Argo CD Applications (os:admin); downNodes as for dataServices.
    case argoCD(downNodes: Set<String>)
    /// Flux Kustomizations, HelmReleases and sources (os:admin); downNodes as for dataServices.
    case flux(downNodes: Set<String>)
    case health
    case settings
    case importConfig
    /// The imported clusters: switch, color, remove, add.
    case clusters
    case debugShell(node: String, hostname: String)
    /// Events timeline for one node, or all of them (node nil); hostnames by address.
    case events(node: String?, hostnames: [String: String])
    /// The apps running in the cluster; hostnames by address, for the nodes pods run on.
    case apps(hostnames: [String: String])
    /// Issue a talosconfig (os:admin): renew this device's certificate, or one for another device.
    case issueConfig(renew: Bool)
    /// AI diagnosis (optional, see AISettings); note: what the opening screen already knows.
    case diagnosis(note: String)
}

struct NodeRef: Hashable {
    let address: String
    let hostname: String
    let role: String
}

struct MainNavigation: View {
    @Environment(AppModel.self) private var model
    @State private var path: [Route] = []

    var body: some View {
        NavigationStack(path: $path) {
            OverviewView(path: $path)
                .navigationDestination(for: Route.self) { route in
                    switch route {
                    case .node(let ref): NodeDetailView(ref: ref)
                    case .nodeLive(let ref): NodeDetailView(ref: ref, initialTab: .live)
                    case .nodePower(let ref, let action): NodeDetailView(ref: ref, initialAction: action)
                    case .nodes(let filter, let nodes): NodesView(nodes: nodes, filter: filter, path: $path)
                    case .logs(let node, let hostname, let service):
                        LogsView(node: node, hostname: hostname, service: service)
                    case .containerLogs(let node, let hostname, let container):
                        LogsView(node: node, hostname: hostname, service: nil, container: container)
                    case .insights: InsightsView()
                    case .metrics: MetricsView()
                    case .supportBundle: SupportBundleView()
                    case .changelog: ChangelogView()
                    case .licenses: LicensesView()
                    case .etcd: EtcdView()
                    case .kubespan: KubeSpanView()
                    case .workloads: KubernetesView()
                    case .dataServices(let hints, let downNodes, let kind): DataServicesView(hints: hints, downNodes: downNodes, selected: kind)
                    case .argoCD(let downNodes): ArgoCDView(downNodes: downNodes)
                    case .flux(let downNodes): FluxView(downNodes: downNodes)
                    case .health: HealthView()
                    case .settings: SettingsView()
                    case .diagnosis(let note): DiagnosisView(initialNote: note)
                    case .importConfig: ImportView { path.removeAll() }
                    case .clusters: ClustersView()
                    case .debugShell(let node, let hostname): DebugShellView(node: node, hostname: hostname)
                    case .events(let node, let hostnames): EventsView(node: node, hostnames: hostnames)
                    case .apps(let hostnames): AppsView(hostnames: hostnames)
                    case .issueConfig(let renew): IssueConfigView(initialMode: renew ? .renew : .otherDevice)
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
        .onAppear {
            if NotificationRouter.shared.pendingRenewal { openRenewal() }
            if NotificationRouter.shared.pendingCluster != nil { openCluster() }
        }
    }

    /// From a quick action: that cluster's overview, no screen of the previous one over it.
    private func openCluster() {
        guard let fingerprint = NotificationRouter.shared.pendingCluster else { return }
        NotificationRouter.shared.pendingCluster = nil
        if model.selectCluster(fingerprint: fingerprint) { path = [] }
    }

    /// From the certificate-expiry alert: the renewal screen, or the settings (which show the
    /// expiry) when this config may not issue certificates.
    private func openRenewal() {
        NotificationRouter.shared.pendingRenewal = false
        path = [model.allows(.issueConfig) ? .issueConfig(renew: true) : .settings]
    }
}
