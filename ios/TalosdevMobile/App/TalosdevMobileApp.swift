import SwiftUI
import TalosdevMobileCore

@main
struct TalosdevMobileApp: App {
    @State private var model = AppModel()
    @State private var support = SupportPrompt()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        BackgroundMonitor.register()
        BackgroundMonitor.registerCategories()
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .environment(support)
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
}

struct NodeRef: Hashable {
    let address: String
    let hostname: String
    let role: String
}

struct MainNavigation: View {
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
                    case .importConfig: ImportView { path.removeAll() }
                    case .debugShell(let node, let hostname): DebugShellView(node: node, hostname: hostname)
                    }
                }
        }
    }
}
