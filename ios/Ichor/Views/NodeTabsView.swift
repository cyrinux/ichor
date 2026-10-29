import SwiftUI
import IchorCore

/// A node's status tabs (services, resources, live stats, processes, containers, cgroups),
/// pushed from the Status rows of the node hub, a share link that names a tab, or a row's Live.
struct NodeTabsView: View {
    let ref: NodeRef

    @Environment(AppModel.self) private var model
    @State private var tab: Tab
    @State private var services: LoadState<[ServiceInfo]> = .loading
    @State private var resources: LoadState<NodeResources> = .loading
    /// This node's clock offset, shown with its resources; nil until checked.
    @State private var clock: NodeTimeInfo?
    @State private var running = false
    @State private var resultMessage: String?
    @State private var live = LiveStats()
    @State private var processes = ProcessMonitor()
    @State private var pods = PodMonitor()
    @State private var cgroups = CgroupMonitor()
    /// One cgroups read for the pressure section of Resources (os:admin only).
    @State private var pressure: LoadState<CgroupReport> = .loading
    /// Service start/stop/restart waiting for confirmation.
    @State private var serviceRequest: ServiceRequest?
    /// The cluster's history over `period` (uptime strip, memory line); nil without one.
    @State private var history: HistoryQueryResult?
    @State private var period = HistoryPeriod.week

    init(ref: NodeRef, initialTab: Tab = .services) {
        self.ref = ref
        _tab = State(initialValue: initialTab)
    }

    /// The raw values are the share link's tab names, lowercased.
    enum Tab: String, CaseIterable {
        case services = "Services", resources = "Resources", live = "Live", processes = "Processes", pods = "Pods",
            cgroups = "Cgroups"

        var label: String {
            switch self {
            case .services: String(localized: "Services")
            case .resources: String(localized: "Resources")
            case .live: String(localized: "Live")
            case .processes: String(localized: "Processes")
            case .pods: String(localized: "Pods")
            case .cgroups: String(localized: "Cgroups")
            }
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            Picker("View", selection: $tab) {
                ForEach(shownTabs, id: \.self) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            .padding()

            if let history, let uptime = history.node(ref.address),
                uptime.uptimePercent != nil || !uptime.intervals.isEmpty
            {
                NodeUptimeStrip(uptime: uptime, from: history.from, to: history.to, period: $period)
                    .padding(.horizontal)
                    .padding(.bottom, 8)
            }

            // Swiped like the Android pager; the Live and Cgroups charts scrub sideways instead.
            SwipeTabPager(tabs: shownTabs, selection: shownTab, fixed: [.live, .cgroups]) { page($0) }
        }
        .navigationTitle(ref.hostname)
        .navigationBarTitleDisplayMode(.inline)
        // What this node's Talos version can do: gates the tabs once known.
        .task { await model.loadFeatures(node: ref.address) }
        // Masked like the screen in screenshot mode: reloaded when it changes.
        .task(id: "\(period.rawValue)#\(model.dataGeneration)") {
            history = await HistoryStore.query(
                cluster: model.activeSummary.map(monitorClusterKey) ?? "", period: period)
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if running {
                    ProgressView()
                } else {
                    ShareLinkButton(
                        target: .node(address: ref.address, hostname: ref.hostname, tab: tab.rawValue.lowercased()))
                }
            }
            // The node itself (its hub), whatever the tab on screen.
            ToolbarItem(placement: .primaryAction) {
                PinButton(target: .node(address: ref.address, hostname: ref.hostname, tab: ""), label: ref.hostname)
            }
        }
        .confirmationDialog(
            serviceRequest.map { confirmationTitle($0) } ?? "",
            isPresented: $serviceRequest.isPresent(),
            titleVisibility: .visible,
            presenting: serviceRequest
        ) { request in
            Button(request.action.localizedTitle, role: request.action == .start ? nil : ButtonRole.destructive) {
                Task { await perform(request) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { request in
            if isCriticalService(request.service) && request.action != .start {
                Text(
                    "\(request.service) is a critical system service: stopping or restarting it can make the node unreachable or disrupt the cluster."
                )
            } else {
                Text(request.action.localizedDetails)
            }
        }
        .alert(resultMessage ?? "", isPresented: $resultMessage.isPresent()) {
            Button("OK") {}
        }
    }

    /// Cgroups (a copy of /sys/fs/cgroup) only exists for admin configs.
    private var shownTabs: [Tab] { Tab.allCases.filter { $0 != .cgroups || model.allows(.cgroups) } }

    /// A role change (another config) can leave the hidden Cgroups tab selected: Services then.
    private var shownTab: Binding<Tab> {
        Binding(get: { shownTabs.contains(tab) ? tab : .services }, set: { tab = $0 })
    }

    @ViewBuilder
    private func page(_ shown: Tab) -> some View {
        switch shown {
        case .services:
            LoadStateView(state: services, retry: loadServices) { list in
                List(list) { svc in serviceRow(svc) }
                    .refreshable { await loadServices() }
                    .themedBackground()
            }
            .task { if case .loading = services { await loadServices() } }
        case .resources:
            LoadStateView(state: resources, retry: loadResources) {
                ResourcesList(
                    resources: $0, clock: clock, pressure: model.allows(.cgroups) ? pressure : nil,
                    memory: history?.memory(of: ref.address) ?? [], window: historyWindow
                ) { tab = .cgroups }
            }
            .refreshable { await loadResources() }
            .task { if case .loading = resources { await loadResources() } }
        case .live:
            LiveView(node: ref.address, stats: live)
        case .processes:
            FeatureGated(support: support(.processes)) {
                ProcessesView(node: ref.address, monitor: processes)
            }
        case .pods:
            FeatureGated(support: support(.containers)) {
                PodsView(
                    node: ref.address, hostname: ref.hostname, monitor: pods, restartSupport: support(.containerRestart)
                )
            }
        case .cgroups:
            CgroupsView(node: ref.address, monitor: cgroups)
        }
    }

    private func loadServices() async {
        guard let client = model.client else { return }
        services = model.seeded(services, from: .services(node: ref.address))
        services = services.refreshed(
            with: await .from { try await model.fetch(.services(node: ref.address), with: client) })
    }

    private func loadResources() async {
        guard let client = model.client else { return }
        // The cgroups copy is the slow part: on its own, so pull-to-refresh ends with resources.
        if model.allows(.cgroups) {
            Task { pressure = pressure.refreshed(with: await .from { try await client.cgroups(node: ref.address) }) }
        }
        resources = model.seeded(resources, from: .resources(node: ref.address))
        resources = resources.refreshed(
            with: await .from { try await model.fetch(.resources(node: ref.address), with: client) })
        // Best-effort: a node that cannot answer shows the error in its row.
        do {
            clock = try await client.nodeTime(node: ref.address)
        } catch {
            clock = NodeTimeInfo(node: ref.address, error: error.localizedDescription)
        }
    }

    /// The history's window (epoch ms), for the memory line.
    private var historyWindow: ClosedRange<Int64> {
        guard let history, history.to > history.from else { return 0...1 }
        return history.from...history.to
    }

    private func support(_ feature: NodeFeature) -> FeatureSupport {
        model.support(feature, node: ref.address)
    }
}

struct ServiceRequest: Identifiable {
    let service: String
    let action: ServiceAction

    var id: String { "\(service)/\(action.rawValue)" }
}

extension NodeTabsView {
    /// A service row: opens its log; swipe or long press for start/stop/restart when allowed.
    private func serviceRow(_ svc: ServiceInfo) -> some View {
        NavigationLink(value: Route.logs(node: ref.address, hostname: ref.hostname, service: svc.id)) {
            NodeServiceRow(service: svc)
        }
        .swipeActions(edge: .trailing) {
            if model.allows(.serviceControl) && support(.serviceControl).supported && !running {
                ForEach(serviceActions(state: svc.state)) { action in
                    Button {
                        serviceRequest = ServiceRequest(service: svc.id, action: action)
                    } label: {
                        Label(action.localizedTitle, systemImage: action.symbol)
                    }
                    .tint(action.tint)
                }
            }
        }
        .contextMenu {
            if model.allows(.serviceControl) && !running {
                ForEach(serviceActions(state: svc.state)) { action in
                    FeatureButton(
                        title: action.localizedTitle, systemImage: action.symbol, support: support(.serviceControl),
                        role: action == .start ? nil : ButtonRole.destructive
                    ) {
                        serviceRequest = ServiceRequest(service: svc.id, action: action)
                    }
                }
            }
        }
    }

    private func confirmationTitle(_ request: ServiceRequest) -> String {
        request.action.confirmationTitle(service: request.service, hostname: ref.hostname)
    }

    /// Asks for Face ID / passcode when the app lock is on, runs the action, then reloads the list.
    private func perform(_ request: ServiceRequest) async {
        serviceRequest = nil
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: confirmationTitle(request)) {
            resultMessage = failure
            return
        }
        running = true
        do {
            try await client.serviceAction(request.action, service: request.service, node: ref.address)
            resultMessage = request.action.requestedMessage(service: request.service, hostname: ref.hostname)
        } catch {
            resultMessage = error.localizedDescription
        }
        running = false
        await loadServices()
    }
}

extension ServiceAction {
    var symbol: String {
        switch self {
        case .start: "play.fill"
        case .stop: "stop.fill"
        case .restart: "arrow.clockwise"
        }
    }

    var tint: Color {
        switch self {
        case .start: .green
        case .stop: .red
        case .restart: .orange
        }
    }
}
