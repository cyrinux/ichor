import SwiftUI
import IchorCore

/// The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
/// issues (os:admin): workloads with rollout restart, pods, CronJobs with a manual run,
/// and a network test between two nodes. The namespace filter and the search carry over between the lists. The toolbar opens
/// the network policies and, with Cilium, the live flows; it also sets the API address to use
/// instead of the kubeconfig's, for a cluster the phone reaches another way (not in screenshot
/// mode: the alert would show the real address).
struct KubernetesView: View {
    enum Tab: Hashable { case workloads, pods, cronJobs, network }

    /// A network screen pushed from the toolbar or a pod.
    enum NetScreen: Hashable {
        case policies
        case flows(HubbleFilter)
    }

    @Environment(AppModel.self) private var model
    @State private var tab = Tab.workloads
    @State private var namespace: String?
    @State private var query = ""
    @State private var editingServer = false
    @State private var serverInput = ""
    @State private var serverError: String?
    /// Kept across tabs: only leaving the screen stops a running network test.
    @State private var netPerf = NetPerfSession()
    /// Read once on open: Live flows needs Cilium.
    @State private var cilium: CiliumStatus?
    @State private var netScreen: NetScreen?

    /// The cluster whose API address can be set: not the demo, not in screenshot mode.
    private var editable: ContextSummary? {
        model.activeSummary.flatMap { $0.demo || $0.fingerprint.isEmpty || model.privacyMask ? nil : $0 }
    }

    var body: some View {
        Group {
            switch tab {
            case .workloads: WorkloadsList(namespace: $namespace, query: query)
            case .pods: PodsList(namespace: $namespace, query: query, onFlows: podFlows)
            case .cronJobs: CronJobsList(namespace: $namespace, query: query)
            case .network: NetPerfView(session: netPerf)
            }
        }
        // A new address: the lists load again through it.
        .id(model.client?.kubeServer)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button { netScreen = .policies } label: { Label("Network policies", systemImage: "shield.lefthalf.filled") }
                    if let cilium, cilium.installed {
                        Button { netScreen = .flows(HubbleFilter()) } label: {
                            Label("Live flows", systemImage: "point.3.filled.connected.trianglepath.dotted")
                        }
                    }
                } label: {
                    Label("Network policies and flows", systemImage: "shield.lefthalf.filled")
                }
            }
            if editable != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        serverInput = model.client?.kubeServer ?? ""
                        editingServer = true
                    } label: {
                        Label("Kubernetes API address", systemImage: "server.rack")
                    }
                }
            }
        }
        .alert("Kubernetes API address", isPresented: $editingServer) {
            TextField(text: $serverInput, prompt: Text(verbatim: "k8s.example.com:6443")) { Text("Address") }
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("OK") { Task { await saveServer() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Used instead of the address in the kubeconfig Talos issues, e.g. a port forward, a load balancer or a public name: host, host:port or an https URL. Without a port, the kubeconfig’s is used. The certificate is still checked against the cluster’s own address. Leave empty to use the kubeconfig’s again. Only on this device.")
        }
        .messageAlert($serverError)
        .navigationDestination(item: $netScreen) { screen in
            switch screen {
            case .policies: NetPoliciesView()
            case .flows(let filter): LiveFlowsView(cilium: cilium ?? CiliumStatus(), filter: filter)
            }
        }
        .task(id: model.client?.kubeServer) {
            guard let client = model.client else { return }
            cilium = try? await client.cilium()
        }
        .safeAreaInset(edge: .top) {
            Picker(selection: $tab) {
                Text("Workloads").tag(Tab.workloads)
                Text("Pods").tag(Tab.pods)
                Text("CronJobs").tag(Tab.cronJobs)
                Text("Network").tag(Tab.network)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.bottom, 6)
            .background(.bar)
        }
        .searchable(text: $query, prompt: tab == .cronJobs ? Text("Name, schedule or image") : Text("Name, kind or image"))
        .navigationTitle(Text(verbatim: "Kubernetes"))
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: model.privacyMask) { _, masked in if masked { editingServer = false } }
        .onDisappear { if netPerf.isRunning { netPerf.leave() } }
    }

    /// Opens a pod's live flows from the Pods list: with Cilium only.
    private var podFlows: ((KubePod) -> Void)? {
        guard cilium?.installed == true else { return nil }
        return { pod in netScreen = .flows(HubbleFilter(namespace: pod.namespace, pod: pod.name)) }
    }

    private func saveServer() async {
        guard let context = editable else { return }
        do {
            model.setKubeServer(try await TalosClient.normalizeKubeServer(serverInput), for: context)
        } catch {
            serverError = error.localizedDescription
        }
    }
}

/// Picker of the namespaces present in the list; nil is every namespace.
struct NamespacePicker: View {
    let namespaces: [String]
    @Binding var namespace: String?

    var body: some View {
        Picker("Namespace", selection: $namespace) {
            Text("All namespaces").tag(String?.none)
            ForEach(namespaces, id: \.self) { Text(verbatim: $0).tag(String?.some($0)) }
        }
    }
}

/// Deployments, StatefulSets and DaemonSets with a rolling restart like `kubectl rollout restart`;
/// tap one for scale and, for a Deployment, its revisions to roll back to.
private struct WorkloadsList: View {
    @Binding var namespace: String?
    let query: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubeWorkload]> = .loading
    @State private var confirm: KubeWorkload?
    @State private var restarting: Set<String> = []
    @State private var resultMessage: String?
    /// Just restarted: its rollout is shown live until the sheet is closed.
    @State private var following: KubeWorkload?
    /// Its scale and history sheet is open.
    @State private var actions: KubeWorkload?

    var body: some View {
        LoadStateView(state: state, retry: load) { workloads in
            let namespaces = workloadNamespaces(workloads)
            let selected = namespace.flatMap { namespaces.contains($0) ? $0 : nil }
            let shown = filterWorkloads(workloads, namespace: selected, query: query)
            List {
                Section { NamespacePicker(namespaces: namespaces, namespace: $namespace) }
                Section {
                    ForEach(shown) { workload in
                        WorkloadRow(workload: workload, showNamespace: selected == nil,
                                    restarting: restarting.contains(workload.id),
                                    onOpen: { actions = workload }) { confirm = workload }
                    }
                }
            }
            .overlay {
                if shown.isEmpty {
                    if query.isEmpty {
                        ContentUnavailableView("No workloads", systemImage: "square.stack.3d.up")
                    } else {
                        ContentUnavailableView.search(text: query)
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        .restartConfirmation($confirm) { workload in Task { await restart(workload) } }
        .sheet(item: $following) { workload in RolloutStatusSheet(workload: workload) { await load() } }
        .sheet(item: $actions) { workload in WorkloadActionsSheet(workload: workload) { await load() } }
        .messageAlert($resultMessage)
    }

    private func load() async {
        guard let client = model.client else { return }
        state = model.seeded(state, from: .workloads, as: KubeWorkloadList.self) { $0.workloads }
        state = state.refreshed(with: await .from { try await model.fetch(.workloads, as: KubeWorkloadList.self, with: client).workloads })
    }

    private func restart(_ workload: KubeWorkload) async {
        guard let client = model.client, !restarting.contains(workload.id) else { return }
        restarting.insert(workload.id)
        defer { restarting.remove(workload.id) }
        do {
            try await client.rolloutRestart(workload)
            following = workload
            // Show the rollout starting: the controller already bumped the generation.
            await load()
        } catch {
            resultMessage = String(localized: "Could not restart \(workload.name): \(error.localizedDescription)")
        }
    }
}

private struct WorkloadRow: View {
    let workload: KubeWorkload
    let showNamespace: Bool
    let restarting: Bool
    let onOpen: () -> Void
    let onRestart: () -> Void

    var body: some View {
        HStack {
            Button(action: onOpen) { details }
                .buttonStyle(.plain)
            if restarting {
                ProgressView()
            } else {
                Button(action: onRestart) {
                    Image(systemName: "arrow.clockwise.circle")
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                    .buttonStyle(.borderless)
                    .disabled(!workload.canRestart)
                    .accessibilityLabel(Text("Restart \(workload.name)"))
            }
        }
    }

    /// Name, kind, readiness and last restart: opens the workload's actions.
    private var details: some View {
        HStack {
            VStack(alignment: .leading, spacing: 3) {
                Text(verbatim: workload.name)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(verbatim: showNamespace ? "\(workload.kind) · \(workload.namespace)" : workload.kind)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack(spacing: 8) {
                    Text("\(workload.ready)/\(workload.desired) ready") + Text(verbatim: " · ") + Text(stateLabel)
                }
                .font(.caption)
                .foregroundStyle(stateColor)
                .monospacedDigit()
                if workload.restartedAt > 0 {
                    let at = Date(epochMillis: workload.restartedAt)
                    Text("restarted \(at.formatted(.relative(presentation: .named)))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
        }
        .contentShape(Rectangle())
    }

    private var stateLabel: LocalizedStringKey {
        switch workload.workloadState {
        case .ready: "up to date"
        case .progressing: "rolling out"
        case .degraded: "degraded"
        case .paused: "paused"
        case .scaledDown: "scaled to zero"
        case .unknown: "Unknown"
        }
    }

    private var stateColor: Color {
        switch workload.workloadState {
        case .ready: .green
        case .progressing: .orange
        case .degraded: .red
        default: .secondary
        }
    }
}
