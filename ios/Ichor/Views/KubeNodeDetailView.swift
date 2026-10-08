import SwiftUI
import IchorCore

/// A node of a cluster added from a kubeconfig, as the Kubernetes API describes it: its status,
/// roles, addresses, software and capacity, the pods it runs and its events, with the cordon,
/// the drain, its YAML and a share link in the menu. Opened with the node as the home loaded
/// it; a pull asks again. No Talos tab: the cluster has no Talos API. Same as Android's
/// KubeNodeDetailScreen.
struct KubeNodeDetailView: View {
    /// The screen's tabs; a share link names the Pods one "kube-pods" (ShareTarget.nodeTabs).
    enum Tab: String, CaseIterable, Hashable {
        case details, pods, events

        var label: String {
            switch self {
            case .details: String(localized: "Details")
            case .pods: String(localized: "Pods")
            case .events: String(localized: "Events")
            }
        }
    }

    /// The core/v1 Node resource, for the YAML; editable, as `kubectl edit node` is.
    static let nodeResource = KubeAPIResource(resource: "nodes", kind: "Node", namespaced: false,
                                              verbs: ["get", "list", "update", "patch"])

    @Environment(AppModel.self) private var model
    @State private var node: KubeNodeInfo
    @State private var tab: Tab
    /// A refresh no longer listed the node.
    @State private var gone = false
    @State private var refreshError: String?
    /// The cordon to change, after a confirmation.
    @State private var cordoning: KubeNodeInfo?
    @State private var draining = false
    @State private var showingYAML = false

    init(node: KubeNodeInfo, initialTab: Tab = .details) {
        _node = State(initialValue: node)
        _tab = State(initialValue: initialTab)
    }

    var body: some View {
        VStack(spacing: 0) {
            Picker("View", selection: $tab) {
                ForEach(Tab.allCases, id: \.self) { Text(verbatim: $0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            .padding()

            switch tab {
            case .details:
                details
            case .pods:
                SelectedPodsView(selection: .kubeNode(node.name))
            case .events:
                List {
                    Section { KubeEventsRows(namespace: "", kind: "Node", name: node.name) }
                }
                .themedBackground()
            }
        }
        .navigationTitle(Text(verbatim: node.name))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button(node.cordoned ? String(localized: "Uncordon") : String(localized: "Cordon"), systemImage: "nosign") {
                        cordoning = node
                    }
                    Button("Drain…", systemImage: "rectangle.portrait.and.arrow.right") { draining = true }
                    Button("YAML", systemImage: "curlybraces") { showingYAML = true }
                    // Named as Kubernetes does and by its address: a phone holding a talosconfig
                    // for the cluster opens the Talos node of the same name.
                    ShareLinkButton(target: .node(address: node.internalIP ?? "", hostname: node.name, tab: tab == .pods ? "kube-pods" : ""))
                } label: {
                    Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                }
            }
        }
        .kubeCordonDialogs(cordoning: $cordoning, reload: refresh)
        .navigationDestination(isPresented: $draining) {
            MaintenanceView(node: node.name, hostname: node.name, drainOnly: true)
        }
        .navigationDestination(isPresented: $showingYAML) {
            KubeObjectView(resource: Self.nodeResource, namespace: "", name: node.name)
        }
    }

    /// Status pills, then the node's identity, software, capacity and, when the cloud says, its provenance.
    private var details: some View {
        List {
            if gone {
                Section { Text("This node is no longer listed.").font(.callout).foregroundStyle(.secondary) }
            }
            if let refreshError {
                Section { ErrorOrNoticeText(message: refreshError) }
            }
            identitySection
            softwareSection
            capacitySection
            if hasProvenance { cloudSection }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }

    private var identitySection: some View {
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        return Section {
            HStack(spacing: 6) {
                if node.ready {
                    StatusPill(label: String(localized: "Ready"), color: node.status == .ready ? .green : .statusWarn)
                } else {
                    StatusPill(label: String(localized: "Not ready"), color: .red)
                }
                if node.cordoned { StatusPill(label: String(localized: "Cordoned"), color: .statusWarn) }
                // Kubernetes' own condition names: what kubectl shows too.
                ForEach(node.pressure, id: \.self) { StatusPill(label: $0, color: .statusWarn) }
            }
            LabeledContent("Roles", value: node.roles.isEmpty ? String(localized: "No role label") : node.roles.joined(separator: ", "))
            if let ip = node.internalIP, !ip.isEmpty {
                LabeledContent("Internal IP") { Text(verbatim: ip).monospaced() }
            }
            if let ip = node.externalIP, !ip.isEmpty {
                LabeledContent("External IP") { Text(verbatim: ip).monospaced() }
            }
            if node.created > 0 {
                LabeledContent("Created", value: CheckupText.kubeEventsAgo(checkupAge(node.created * 1000, now: now)))
            }
        } header: {
            Text("Status")
        }
    }

    private var softwareSection: some View {
        Section("Software") {
            if let kubelet = node.kubelet, !kubelet.isEmpty {
                LabeledContent("Kubelet") { Text(verbatim: kubelet).monospaced() }
            }
            if let image = node.osImage, !image.isEmpty { LabeledContent("OS image", value: image) }
            if let kernel = node.kernel, !kernel.isEmpty {
                LabeledContent("Kernel") { Text(verbatim: kernel).monospaced() }
            }
            if let runtime = node.runtime, !runtime.isEmpty {
                LabeledContent("Container runtime") { Text(verbatim: runtime).monospaced() }
            }
            if let arch = node.arch, !arch.isEmpty { LabeledContent("Architecture", value: arch) }
        }
    }

    private var capacitySection: some View {
        Section("Capacity") {
            LabeledContent("CPU cores", value: formatCores(node.cpu))
            LabeledContent("Memory", value: formatBytes(Int64(node.memory)))
            LabeledContent("Pod limit", value: "\(node.podLimit)")
        }
    }

    /// Pool, machine type, spot or on-demand: where the cloud put the node, when it says.
    private var hasProvenance: Bool {
        node.localizedPoolLabel != nil || !(node.instanceType ?? "").isEmpty || node.localizedCapacityLabel != nil
    }

    private var cloudSection: some View {
        Section("Cloud") {
            if let pool = node.localizedPoolLabel { LabeledContent("Node pool", value: pool) }
            if let type = node.instanceType, !type.isEmpty {
                LabeledContent("Machine type") { Text(verbatim: type).monospaced() }
            }
            if let capacity = node.localizedCapacityLabel { LabeledContent("Capacity type", value: capacity) }
        }
    }

    /// The node as Kubernetes lists it now; a node that left the list says so, the rest stays.
    private func refresh() async {
        guard let client = model.client else { return }
        do {
            let overview = try await client.kubeNodes()
            // The credentials may not list nodes: keep what was shown rather than nothing.
            guard !overview.forbidden else { return }
            if let fresh = overview.nodes.first(where: { $0.name == node.name }) {
                node = fresh
                gone = false
            } else {
                gone = true
            }
            refreshError = nil
        } catch {
            refreshError = error.localizedDescription
        }
    }
}
