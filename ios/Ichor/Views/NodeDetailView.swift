import SwiftUI
import IchorCore

struct NodeDetailView: View {
    let ref: NodeRef

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var tab = Tab.services
    @State private var services: LoadState<[ServiceInfo]> = .loading
    @State private var resources: LoadState<NodeResources> = .loading
    /// This node's clock offset, shown with its resources; nil until checked.
    @State private var clock: NodeTimeInfo?
    @State private var powerAction: PowerAction?
    @State private var running = false
    @State private var resultMessage: String?
    @State private var succeeded = false
    @State private var showingKernelLog = false

    init(ref: NodeRef, initialTab: Tab = .services, initialAction: PowerAction? = nil) {
        self.ref = ref
        _tab = State(initialValue: initialTab)
        _powerAction = State(initialValue: initialAction)
    }

    @State private var live = LiveStats()
    @State private var showingDebugShell = false
    @State private var showingMachineConfig = false
    @State private var processes = ProcessMonitor()
    @State private var pods = PodMonitor()
    @State private var cgroups = CgroupMonitor()
    /// One cgroups read for the pressure section of Resources (os:admin only).
    @State private var pressure: LoadState<CgroupReport> = .loading
    @State private var showingEvents = false
    @State private var showingNetwork = false
    @State private var showingHardware = false
    @State private var showingImages = false
    @State private var showingCapture = false
    @State private var showingUpgrade = false
    @State private var showingStorage = false
    @State private var showingResources = false
    /// Service start/stop/restart waiting for confirmation.
    @State private var serviceRequest: ServiceRequest?

    enum Tab: String, CaseIterable {
        case services = "Services", resources = "Resources", live = "Live", processes = "Processes", pods = "Pods", cgroups = "Cgroups"

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
                // Cgroups (a copy of /sys/fs/cgroup) only exists for admin configs.
                ForEach(Tab.allCases.filter { $0 != .cgroups || model.allows(.cgroups) }, id: \.self) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            .padding()

            // A role change (another config) can leave the hidden Cgroups tab selected.
            switch (tab == .cgroups && !model.allows(.cgroups)) ? Tab.services : tab {
            case .services:
                LoadStateView(state: services, retry: loadServices) { list in
                    List(list) { svc in serviceRow(svc) }
                    .refreshable { await loadServices() }
                    .themedBackground()
                }
                .task { if case .loading = services { await loadServices() } }
            case .resources:
                LoadStateView(state: resources, retry: loadResources) {
                    ResourcesList(resources: $0, clock: clock, pressure: model.allows(.cgroups) ? pressure : nil) { tab = .cgroups }
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
                    PodsView(node: ref.address, hostname: ref.hostname, monitor: pods)
                }
            case .cgroups:
                CgroupsView(node: ref.address, monitor: cgroups)
            }
        }
        .navigationTitle(ref.hostname)
        .navigationBarTitleDisplayMode(.inline)
        // What this node's Talos version can do: gates the menu and the tabs once known.
        .task { await model.loadFeatures(node: ref.address) }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if running {
                    ProgressView()
                } else {
                    Menu { menuItems } label: {
                        Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                    }
                }
            }
        }
        .navigationDestination(isPresented: $showingKernelLog) {
            LogsView(node: ref.address, hostname: ref.hostname, service: nil)
        }
        .navigationDestination(isPresented: $showingEvents) {
            EventsView(node: ref.address, hostnames: [ref.address: ref.hostname])
        }
        .navigationDestination(isPresented: $showingNetwork) {
            NetworkView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingImages) {
            ImagesView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingHardware) {
            HardwareView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingDebugShell) {
            DebugShellView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingMachineConfig) {
            MachineConfigView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingCapture) {
            CaptureView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingUpgrade) {
            UpgradeView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingStorage) {
            StorageView(node: ref.address, hostname: ref.hostname)
        }
        .navigationDestination(isPresented: $showingResources) {
            ResourceTypesView(node: ref.address, hostname: ref.hostname)
        }
        .sheet(item: $powerAction) { action in
            PowerSheet(action: action, hostname: ref.hostname, role: ref.role) { request in
                powerAction = nil
                Task { await perform(request) }
            }
        }
        .confirmationDialog(serviceRequest.map { confirmationTitle($0) } ?? "",
                            isPresented: $serviceRequest.isPresent(),
                            titleVisibility: .visible,
                            presenting: serviceRequest) { request in
            Button(request.action.localizedTitle, role: request.action == .start ? nil : ButtonRole.destructive) {
                Task { await perform(request) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { request in
            if isCriticalService(request.service) && request.action != .start {
                Text("\(request.service) is a critical system service: stopping or restarting it can make the node unreachable or disrupt the cluster.")
            } else {
                Text(request.action.localizedDetails)
            }
        }
        .alert(resultMessage ?? "", isPresented: $resultMessage.isPresent()) {
            Button("OK") { if succeeded { dismiss() } }
        }
    }

    private func loadServices() async {
        guard let client = model.client else { return }
        services = model.seeded(services, from: .services(node: ref.address))
        services = services.refreshed(with: await .from { try await model.fetch(.services(node: ref.address), with: client) })
    }

    private func loadResources() async {
        guard let client = model.client else { return }
        // The cgroups copy is the slow part: on its own, so pull-to-refresh ends with resources.
        if model.allows(.cgroups) {
            Task { pressure = pressure.refreshed(with: await .from { try await client.cgroups(node: ref.address) }) }
        }
        resources = model.seeded(resources, from: .resources(node: ref.address))
        resources = resources.refreshed(with: await .from { try await model.fetch(.resources(node: ref.address), with: client) })
        // Best-effort: a node that cannot answer shows the error in its row.
        do {
            clock = try await client.nodeTime(node: ref.address)
        } catch {
            clock = NodeTimeInfo(node: ref.address, error: error.localizedDescription)
        }
    }

    /// With the app lock on, destructive actions need a fresh Face ID / passcode check.
    private func perform(_ request: PowerRequest) async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: "\(request.localizedTitle) \(ref.hostname)") {
            succeeded = false
            resultMessage = failure
            return
        }
        running = true
        defer { running = false }
        do {
            try await client.perform(request, node: ref.address)
            succeeded = true
            resultMessage = String(localized: "\(ref.hostname): \(request.localizedTitle) requested")
        } catch {
            succeeded = false
            resultMessage = error.localizedDescription
        }
    }
}

extension NodeDetailView {
    private func support(_ feature: NodeFeature) -> FeatureSupport {
        model.support(feature, node: ref.address)
    }

    /// The node menu. Entries the role does not allow are hidden; entries this node's Talos
    /// version lacks stay, disabled, saying which version they need. Buttons, since a
    /// NavigationLink inside a Menu does not navigate.
    @ViewBuilder
    private var menuItems: some View {
        Button { showingKernelLog = true } label: {
            Label("Kernel log", systemImage: "terminal")
        }
        FeatureButton(title: String(localized: "Events"), systemImage: "list.bullet.rectangle", support: support(.events)) {
            showingEvents = true
        }
        FeatureButton(title: String(localized: "Network"), systemImage: "network", support: support(.network)) {
            showingNetwork = true
        }
        FeatureButton(title: String(localized: "Storage"), systemImage: "internaldrive", support: support(.mounts)) {
            showingStorage = true
        }
        FeatureButton(title: String(localized: "Images"), systemImage: "shippingbox", support: support(.images)) {
            showingImages = true
        }
        if model.allows(.resourceBrowser) {
            FeatureButton(title: String(localized: "Resources browser"), systemImage: "square.stack.3d.up", support: support(.resourceBrowser)) {
                showingResources = true
            }
        }
        FeatureButton(title: String(localized: "About this node"), systemImage: "info.circle", support: support(.hardware)) {
            showingHardware = true
        }
        // A Group: a view builder takes at most ten views.
        Group {
            if model.allows(.debugShell) {
                FeatureButton(title: String(localized: "Debug shell"), systemImage: "apple.terminal", support: support(.debugShell)) {
                    showingDebugShell = true
                }
            }
            if model.allows(.machineConfig) {
                FeatureButton(title: String(localized: "Machine config"), systemImage: "doc.text", support: support(.machineConfig)) {
                    showingMachineConfig = true
                }
            }
            if model.allows(.packetCapture) {
                FeatureButton(title: String(localized: "Capture packets"), systemImage: "antenna.radiowaves.left.and.right",
                              support: support(.packetCapture)) {
                    showingCapture = true
                }
            }
            if model.allows(.upgrade) {
                FeatureButton(title: String(localized: "Upgrade Talos…"), systemImage: "arrow.up.circle", support: support(.upgrade)) {
                    showingUpgrade = true
                }
            }
        }
        // Power actions only exist for configs whose role allows them.
        if model.allows(.power) {
            Divider()
            ForEach(PowerAction.allCases) { action in
                Button(role: .destructive) { powerAction = action } label: {
                    Label(action.localizedTitle, systemImage: "power")
                }
            }
        }
    }
}

struct ServiceRequest: Identifiable {
    let service: String
    let action: ServiceAction

    var id: String { "\(service)/\(action.rawValue)" }
}

extension NodeDetailView {
    /// A service row: opens its log; swipe or long press for start/stop/restart when allowed.
    private func serviceRow(_ svc: ServiceInfo) -> some View {
        NavigationLink(value: Route.logs(node: ref.address, hostname: ref.hostname, service: svc.id)) {
            ServiceRow(service: svc)
        }
        .swipeActions(edge: .trailing) {
            if model.allows(.serviceControl) && support(.serviceControl).supported && !running {
                ForEach(serviceActions(state: svc.state)) { action in
                    Button { serviceRequest = ServiceRequest(service: svc.id, action: action) } label: {
                        Label(action.localizedTitle, systemImage: action.symbol)
                    }
                    .tint(action.tint)
                }
            }
        }
        .contextMenu {
            if model.allows(.serviceControl) && !running {
                ForEach(serviceActions(state: svc.state)) { action in
                    FeatureButton(title: action.localizedTitle, systemImage: action.symbol, support: support(.serviceControl),
                                  role: action == .start ? nil : ButtonRole.destructive) {
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
            succeeded = false
            resultMessage = failure
            return
        }
        running = true
        do {
            try await client.serviceAction(request.action, service: request.service, node: ref.address)
            // Not `succeeded`: that dismisses the screen (power actions); the node stays usable here.
            succeeded = false
            resultMessage = request.action.requestedMessage(service: request.service, hostname: ref.hostname)
        } catch {
            succeeded = false
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

private struct ServiceRow: View {
    let service: ServiceInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                VStack(alignment: .leading) {
                    Text(service.id).font(.headline)
                    Text(service.state).font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                switch service.health {
                case "healthy": StatusPill(label: String(localized: "Healthy"), color: .green)
                case "unhealthy": StatusPill(label: String(localized: "Unhealthy"), color: .red)
                default: StatusPill(label: String(localized: "No check"), color: .gray)
                }
            }
            let detail = service.health == "unhealthy" ? service.message : service.lastEvent
            if let detail, !detail.isEmpty {
                Text(detail).font(.caption).foregroundStyle(service.health == "unhealthy" ? .red : .secondary)
            }
        }
    }
}

private struct ResourcesList: View {
    let resources: NodeResources
    let clock: NodeTimeInfo?
    /// nil when the role cannot read cgroups: no pressure section then.
    let pressure: LoadState<CgroupReport>?
    let onPressureDetails: () -> Void

    var body: some View {
        List {
            Section("System") {
                LabeledContent("Uptime", value: uptime)
                LabeledContent("CPU", value: resources.cpuModel.isEmpty ? String(localized: "\(resources.cpuCount) threads") : "\(resources.cpuCount) × \(resources.cpuModel)")
                LabeledContent("Load (1/5/15)", value: String(format: "%.2f  %.2f  %.2f", resources.load1, resources.load5, resources.load15))
                if resources.cpuCount > 0 { UsageBar(fraction: resources.load1 / Double(resources.cpuCount)) }
                if let clock { TimeOffsetRow(info: clock, title: String(localized: "Clock offset")) }
            }
            Section("Memory") {
                let used = resources.memTotal - min(resources.memAvailable, resources.memTotal)
                LabeledContent("Used", value: "\(formatBytes(used)) / \(formatBytes(resources.memTotal))")
                UsageBar(fraction: usedFraction(total: resources.memTotal, available: resources.memAvailable))
            }
            // Right after memory: pressure says whether CPU, memory or disk actually hold tasks back.
            if let pressure { PressureSection(state: pressure, onDetails: onPressureDetails) }
            if !resources.mounts.isEmpty {
                Section("Disks") {
                    ForEach(resources.mounts) { mount in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(mount.mountedOn)
                                Spacer()
                                Text(verbatim: "\(formatBytes(mount.size - min(mount.available, mount.size))) / \(formatBytes(mount.size))").font(.caption)
                            }
                            Text(mount.filesystem).font(.caption.monospaced()).foregroundStyle(.secondary)
                            UsageBar(fraction: usedFraction(total: mount.size, available: mount.available))
                        }
                    }
                }
            }
        }
        .themedBackground()
    }

    private var uptime: String {
        guard resources.bootTime > 0 else { return "—" }
        return localizedDuration(Int64(Date().timeIntervalSince1970) - Int64(resources.bootTime))
    }
}
