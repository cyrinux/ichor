import SwiftUI
import IchorCore

/// The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
/// issues (os:admin): workloads with rollout restart, pods, CronJobs with a manual run,
/// and a network test between two nodes. The namespace listed (remembered per cluster, see
/// KubeScopeStore) and the search carry over between the lists, each loaded page by page
/// (plans/roadmap/large-clusters.md). The toolbar opens
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
    @State private var query = ""
    /// The cluster's namespaces; nil while unknown (loading, or failed).
    @State private var namespaces: KubeNamespaces?
    /// The scope picked in the demo or screenshot mode, not kept on the device.
    @State private var localScope: KubeScope?
    /// Bumped when a scope is picked: the stored one is read again.
    @State private var scopeEdits = 0
    /// Kept across tabs: switching tabs neither reloads a list nor stops its load.
    @State private var workloads = PagedList<KubeWorkload>(base: "workloads") { client, namespace, token in
        try await client.workloadsPage(namespace: namespace, token: token)
    }
    // The first page as full objects: a small cluster, loaded in one page, keeps its images
    // and containers; the next pages as Table rows, 10-20 times smaller (L9, L10). Kept rows
    // of Table pages have no images: image search would miss them.
    @State private var pods = PagedList<KubePod>(base: "pods", detailed: { pods in pods.allSatisfy { !$0.images.isEmpty } }) { client, namespace, token in
        try await client.podsPage(namespace: namespace, token: token, table: !token.isEmpty)
    }
    @State private var cronJobs = PagedList<KubeCronJob>(base: "cronjobs") { client, namespace, token in
        try await client.cronJobsPage(namespace: namespace, token: token)
    }
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
            case .workloads: WorkloadsList(list: workloads, control: scopeControl, query: query)
            case .pods: PodsList(list: pods, control: scopeControl, query: query, onFlows: podFlows)
            case .cronJobs: CronJobsList(list: cronJobs, control: scopeControl, query: query)
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
        .task(id: "\(model.activeContext)|\(model.client?.kubeServer ?? "")|\(model.dataGeneration)") {
            await loadNamespaces()
        }
        .task(id: model.summary?.contexts.map(\.fingerprint)) {
            if let summary = model.summary { KubeScopeStore.keep(fingerprints: summary.contexts.map(\.fingerprint)) }
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

    /// The cluster whose scope is kept on the device: not the demo, not in screenshot mode
    /// (then the scope picked lasts while the screen does).
    private var scopeCluster: String? {
        model.activeSummary.flatMap { $0.demo || $0.fingerprint.isEmpty || model.privacyMask ? nil : $0.fingerprint }
    }

    /// The scope of the lists (L5, L6): the one picked, else the default for what the
    /// namespaces say. None when namespaces cannot be listed and the context names none: the
    /// user types one.
    private var scopeControl: KubeScopeControl {
        let scope = defaultScope(remembered: rememberedScope, namespaces: namespaces)
        return KubeScopeControl(scope: scope ?? KubeScope(), namespaces: namespaces, ready: scope != nil) { picked in
            if let cluster = scopeCluster { KubeScopeStore.set(picked, for: cluster) } else { localScope = picked }
            scopeEdits += 1
        }
    }

    /// The scope picked for the active cluster, nil for the default.
    private var rememberedScope: KubeScope? {
        _ = scopeEdits
        guard let cluster = scopeCluster else { return localScope }
        return KubeScopeStore.scope(for: cluster)
    }

    /// The cluster's namespaces; forbidden is an answer, not a failure.
    private func loadNamespaces() async {
        guard let client = model.client else { return }
        namespaces = nil
        let listed = try? await client.namespaces()
        guard !Task.isCancelled else { return }
        namespaces = listed
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
    let list: PagedList<KubeWorkload>
    let control: KubeScopeControl
    let query: String

    @Environment(AppModel.self) private var model
    @State private var confirm: KubeWorkload?
    @State private var restarting: Set<String> = []
    @State private var resultMessage: String?
    /// Just restarted: its rollout is shown live until the sheet is closed.
    @State private var following: KubeWorkload?
    /// Its scale and history sheet is open.
    @State private var actions: KubeWorkload?

    var body: some View {
        KubeListFrame(control: control, list: list, query: query, namespaces: workloadNamespaces) { load in
            let selected = control.scope.namespace
            // Sorted once complete: rows do not jump as pages arrive.
            let shown = filterWorkloads(load.items, namespace: selected, query: query, sorted: load.done)
            List {
                Section {
                    ForEach(shown) { workload in
                        WorkloadRow(workload: workload, showNamespace: selected == nil,
                                    restarting: restarting.contains(workload.id),
                                    onOpen: { actions = workload }) { confirm = workload }
                    }
                    if load.hasMore && query.isEmpty { LoadMoreRow { list.loadMore(model: model) } }
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
            .refreshable { await list.refresh(model: model) }
            .themedBackground()
        }
        .restartConfirmation($confirm) { workload in Task { await restart(workload) } }
        .sheet(item: $following) { workload in RolloutStatusSheet(workload: workload) { await load() } }
        .sheet(item: $actions) { workload in WorkloadActionsSheet(workload: workload) { await load() } }
        .messageAlert($resultMessage)
    }

    private func load() async {
        await list.refresh(model: model)
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
