import SwiftUI
import TalosdevMobileCore
import UserNotifications

@main
struct TalosdevMobileApp: App {
    @State private var model = AppModel()
    @State private var support = SupportPrompt()
    @State private var ai = AISettings()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        // Before any Talos call, including the background refresh task registered below.
        TalosClient.applyStoredPrivacyMask()
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
                }
                .preferredColorScheme(model.theme.colorScheme)
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
    case logs(node: String, hostname: String, service: String?)
    case etcd
    case kubespan
    case health
    case settings
    case importConfig
    case debugShell(node: String, hostname: String)
    /// Events timeline for one node, or all of them (node nil); hostnames by address.
    case events(node: String?, hostnames: [String: String])
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
                    case .logs(let node, let hostname, let service):
                        LogsView(node: node, hostname: hostname, service: service)
                    case .etcd: EtcdView()
                    case .kubespan: KubeSpanView()
                    case .health: HealthView()
                    case .settings: SettingsView()
                    case .diagnosis(let note): DiagnosisView(initialNote: note)
                    case .importConfig: ImportView { path.removeAll() }
                    case .debugShell(let node, let hostname): DebugShellView(node: node, hostname: hostname)
                    case .events(let node, let hostnames): EventsView(node: node, hostnames: hostnames)
                    case .issueConfig(let renew): IssueConfigView(initialMode: renew ? .renew : .otherDevice)
                    }
                }
        }
        .onChange(of: NotificationRouter.shared.pendingRenewal) { _, pending in
            if pending { openRenewal() }
        }
        .onAppear { if NotificationRouter.shared.pendingRenewal { openRenewal() } }
    }

    /// From the certificate-expiry alert: the renewal screen, or the settings (which show the
    /// expiry) when this config may not issue certificates.
    private func openRenewal() {
        NotificationRouter.shared.pendingRenewal = false
        path = [model.allows(.issueConfig) ? .issueConfig(renew: true) : .settings]
    }
}
