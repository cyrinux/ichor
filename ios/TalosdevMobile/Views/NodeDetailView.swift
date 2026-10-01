import SwiftUI
import TalosdevMobileCore

struct NodeDetailView: View {
    let ref: NodeRef

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var tab = Tab.services
    @State private var services: LoadState<[ServiceInfo]> = .loading
    @State private var resources: LoadState<NodeResources> = .loading
    @State private var powerAction: PowerAction?
    @State private var running = false
    @State private var resultMessage: String?
    @State private var succeeded = false
    @State private var showingKernelLog = false

    init(ref: NodeRef) {
        self.ref = ref
    }

    @State private var live = LiveStats()
    @State private var showingDebugShell = false

    enum Tab: String, CaseIterable { case services = "Services", resources = "Resources", live = "Live" }

    var body: some View {
        VStack(spacing: 0) {
            Picker("View", selection: $tab) {
                ForEach(Tab.allCases, id: \.self) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.segmented)
            .padding()

            switch tab {
            case .services:
                LoadStateView(state: services, retry: loadServices) { list in
                    List(list) { svc in
                        NavigationLink(value: Route.logs(node: ref.address, hostname: ref.hostname, service: svc.id)) {
                            ServiceRow(service: svc)
                        }
                    }
                    .refreshable { await loadServices() }
                    .themedBackground()
                }
                .task { if case .loading = services { await loadServices() } }
            case .resources:
                LoadStateView(state: resources, retry: loadResources) { ResourcesList(resources: $0) }
                    .refreshable { await loadResources() }
                    .task { if case .loading = resources { await loadResources() } }
            case .live:
                LiveView(node: ref.address, stats: live)
            }
        }
        .navigationTitle(ref.hostname)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if running {
                    ProgressView()
                } else {
                    Menu {
                        // A NavigationLink inside a Menu does not navigate.
                        Button { showingKernelLog = true } label: {
                            Label("Kernel log", systemImage: "terminal")
                        }
                        if model.allows(.debugShell) {
                            // NavigationLink does not navigate from inside a Menu.
                            Button { showingDebugShell = true } label: {
                                Label("Debug shell", systemImage: "apple.terminal")
                            }
                        }
                        // Power actions only exist for configs whose role allows them.
                        if model.allows(.power) {
                            Divider()
                            ForEach(PowerAction.allCases) { action in
                                Button(role: .destructive) { powerAction = action } label: {
                                    Label(action.title, systemImage: "power")
                                }
                            }
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                    }
                }
            }
        }
        .navigationDestination(isPresented: $showingKernelLog) {
            LogsView(node: ref.address, hostname: ref.hostname, service: nil)
        }
        .navigationDestination(isPresented: $showingDebugShell) {
            DebugShellView(node: ref.address, hostname: ref.hostname)
        }
        .sheet(item: $powerAction) { action in
            PowerSheet(action: action, hostname: ref.hostname, role: ref.role) { request in
                powerAction = nil
                Task { await perform(request) }
            }
        }
        .alert(resultMessage ?? "", isPresented: Binding(get: { resultMessage != nil }, set: { if !$0 { resultMessage = nil } })) {
            Button("OK") { if succeeded { dismiss() } }
        }
    }

    private func loadServices() async {
        guard let client = model.client else { return }
        services = await .from { try await client.services(node: ref.address) }
    }

    private func loadResources() async {
        guard let client = model.client else { return }
        resources = await .from { try await client.resources(node: ref.address) }
    }

    /// With the app lock on, destructive actions need a fresh Face ID / passcode check.
    private func perform(_ request: PowerRequest) async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: "\(request.title) \(ref.hostname)") {
            succeeded = false
            resultMessage = failure
            return
        }
        running = true
        defer { running = false }
        do {
            try await client.perform(request, node: ref.address)
            succeeded = true
            resultMessage = "\(ref.hostname): \(request.title.lowercased()) requested"
        } catch {
            succeeded = false
            resultMessage = error.localizedDescription
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
                case "healthy": StatusPill(label: "Healthy", color: .green)
                case "unhealthy": StatusPill(label: "Unhealthy", color: .red)
                default: StatusPill(label: "No check", color: .gray)
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

    var body: some View {
        List {
            Section("System") {
                LabeledContent("Uptime", value: uptime)
                LabeledContent("CPU", value: resources.cpuModel.isEmpty ? "\(resources.cpuCount) threads" : "\(resources.cpuCount) × \(resources.cpuModel)")
                LabeledContent("Load (1/5/15)", value: String(format: "%.2f  %.2f  %.2f", resources.load1, resources.load5, resources.load15))
                if resources.cpuCount > 0 { UsageBar(fraction: resources.load1 / Double(resources.cpuCount)) }
            }
            Section("Memory") {
                let used = resources.memTotal - min(resources.memAvailable, resources.memTotal)
                LabeledContent("Used", value: "\(formatBytes(used)) / \(formatBytes(resources.memTotal))")
                UsageBar(fraction: usedFraction(total: resources.memTotal, available: resources.memAvailable))
            }
            if !resources.mounts.isEmpty {
                Section("Disks") {
                    ForEach(resources.mounts) { mount in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(mount.mountedOn)
                                Spacer()
                                Text("\(formatBytes(mount.size - min(mount.available, mount.size))) / \(formatBytes(mount.size))").font(.caption)
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
        return formatDuration(Int64(Date().timeIntervalSince1970) - Int64(resources.bootTime))
    }
}
