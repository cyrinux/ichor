import SwiftUI
import IchorCore

/// The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
/// issues (os:admin): workloads with rollout restart, pods, CronJobs with a manual run,
/// and a network test between two nodes. The namespace listed (remembered per cluster, see
/// KubeScopeStore) and the search carry over between the lists, each loaded page by page. The
/// toolbar opens the API server's health, the network and admission policies and, with Cilium, the live flows;
/// it also sets the API address to use instead of the kubeconfig's, for a cluster the phone
/// reaches another way (not in screenshot
/// mode: the alert would show the real address). A share link's focus opens a tab, scoped to
/// and searched for one item, whose sheet opens once its row loads. Like the overview's, its
/// toolbar is arranged by the user: which actions are icons and which are in the ⋯ menu.
struct KubernetesView: View {
    enum Tab: Hashable { case workloads, pods, cronJobs, network }

    /// A screen pushed from the toolbar or a pod.
    enum NetScreen: Hashable {
        case checkup
        case capacity
        case apiHealth
        case policies
        case flows(HubbleFilter)
        /// Every kind the API server serves (the resource browser).
        case resources
        case helm
        case storage
        case services
        case jobs
        /// The cluster's events kept live.
        case events
        /// Kyverno's admission policies (`policies` is the network ones).
        case admissionPolicies
        /// What scales the cluster: HPAs, KEDA, VPA, Karpenter.
        case autoscaling
        /// KubeVirt (and Harvester) virtual machines.
        case virtualMachines
        case crossplane
        case tailscale
        case argoRollouts
        case clusterLifecycle
        /// Data services on one system (nil: the hub), alone (solo) for one the Kubernetes home files elsewhere.
        case dataServices(kind: DataServiceKind?, solo: Bool)
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
    @State private var pods = PagedList<KubePod>(
        base: "pods", detailed: { pods in pods.allSatisfy { !$0.images.isEmpty } }
    ) { client, namespace, token in
        try await client.podsPage(namespace: namespace, token: token, table: !token.isEmpty)
    }
    @State private var cronJobs = PagedList<KubeCronJob>(base: "cronjobs") { client, namespace, token in
        try await client.cronJobsPage(namespace: namespace, token: token)
    }
    @AppStorage(KubernetesBar.storageKey) private var barText = ""
    @State private var customizingBar = false
    /// The pods tab's "Clean up dead pods" sheet.
    @State private var sanitizing = false
    /// App-wide: leaving the screen keeps a running network test.
    private var netPerf: NetPerfJob { .shared }
    /// Read once on open: Live flows needs Cilium, or Calico with Whisker.
    @State private var cilium: CiliumStatus?
    /// Read once on open, as on the Kubernetes home: whether the cluster runs Crossplane, the
    /// Tailscale operator, Argo Rollouts and Cluster API / Sidero Metal (each read answers
    /// "not installed" quickly). Unknown or unreadable: not offered.
    @State private var operators = KubeOperators()
    /// The systems Data services reads here, to offer those filed outside it; empty until read or when unreadable.
    @State private var dataKinds: Set<DataServiceKind> = []
    @State private var netScreen: NetScreen?
    /// The namespace a share link scoped the lists to, not kept, until another is picked.
    @State private var linkedNamespace: String?
    /// The row id of the share link's item, until its sheet opens.
    @State private var focusID: String?
    private let focusTab: Tab

    /// networkTest: opened on the network test (a followed test's Live Activity).
    init(focus: KubeFocus? = nil, networkTest: Bool = false) {
        let tab: Tab =
            switch focus?.tab {
            case _ where networkTest: .network
            case .pods?: .pods
            case .cronJobs?: .cronJobs
            default: .workloads
            }
        focusTab = tab
        _tab = State(initialValue: tab)
        _query = State(initialValue: focus?.name ?? "")
        _linkedNamespace = State(initialValue: focus.flatMap { $0.namespace.isEmpty ? nil : $0.namespace })
        _focusID = State(initialValue: focus.flatMap { $0.id.isEmpty ? nil : $0.id })
    }

    /// The share link's item, on its own tab only (rows of another tab share its ids).
    private var focus: Binding<String?> {
        Binding(get: { tab == focusTab ? focusID : nil }, set: { focusID = $0 })
    }

    /// The tab as a share link names it; nil for the network test.
    private var sharedTab: KubeFocus.Tab? {
        switch tab {
        case .workloads: .workloads
        case .pods: .pods
        case .cronJobs: .cronJobs
        case .network: nil
        }
    }

    private var bar: KubernetesBar { .parse(barText) }

    /// Only what can run here: the share link of a list tab, Live flows with Cilium, the
    /// address where it can be set.
    private func offered(_ action: KubernetesAction) -> Bool {
        switch action {
        case .share: sharedTab != nil
        case .flows: cilium?.installed == true
        case .crossplane: operators.crossplane
        case .tailscale: operators.tailscale
        case .argoRollouts: operators.argoRollouts
        case .clusterLifecycle: operators.clusterLifecycle
        case .certManager, .castai: action.dataServiceKind.map(dataKinds.contains) ?? false
        default: true
        }
    }

    @ViewBuilder
    private func barButton(_ action: KubernetesAction) -> some View {
        if action == .share, let sharedTab {
            ShareLinkButton(target: .kubernetes(tab: sharedTab))
        } else {
            Button {
                open(action)
            } label: {
                Label {
                    action.title
                } icon: {
                    Image(systemName: action.systemImage)
                }
            }
        }
    }

    private func open(_ action: KubernetesAction) {
        switch action {
        case .checkup: netScreen = .checkup
        case .capacity: netScreen = .capacity
        case .networkPolicies: netScreen = .policies
        case .share: break
        case .apiHealth: netScreen = .apiHealth
        case .flows: netScreen = .flows(HubbleFilter())
        case .resources: netScreen = .resources
        case .helm: netScreen = .helm
        case .storage: netScreen = .storage
        case .services: netScreen = .services
        case .jobs: netScreen = .jobs
        case .events: netScreen = .events
        case .admissionPolicies: netScreen = .admissionPolicies
        case .autoscaling: netScreen = .autoscaling
        case .virtualMachines: netScreen = .virtualMachines
        case .crossplane: netScreen = .crossplane
        case .tailscale: netScreen = .tailscale
        case .argoRollouts: netScreen = .argoRollouts
        case .clusterLifecycle: netScreen = .clusterLifecycle
        case .dataServices: netScreen = .dataServices(kind: nil, solo: false)
        case .certManager, .castai: netScreen = .dataServices(kind: action.dataServiceKind, solo: true)
        }
    }

    var body: some View {
        SwipeTabPager(tabs: [Tab.workloads, .pods, .cronJobs, .network], selection: $tab) { shown in
            switch shown {
            case .workloads: WorkloadsList(list: workloads, control: scopeControl, query: query, focus: focus)
            // Clean up dead pods for the namespace listed (every namespace when none is picked).
            case .pods:
                PodsList(
                    list: pods, control: scopeControl, query: query, onFlows: podFlows, focus: focus,
                    onCleanUp: { sanitizing = true })
            case .cronJobs: CronJobsList(list: cronJobs, control: scopeControl, query: query, focus: focus)
            case .network: NetPerfView(session: netPerf)
            }
        }
        // What the credentials may do in the namespace listed; nothing asked for every namespace
        // (a refusal everywhere says nothing of the namespace a row is in).
        .loadsKubeActionAccess(namespace: scopeControl.scope.namespace)
        // A new address: the lists load again through it.
        .id(model.client?.kubeServer)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                // As arranged: the bar's icons, the rest behind ⋯ (with the arrangement itself).
                ForEach(bar.icons.filter(offered)) { action in barButton(action) }
                Menu {
                    // In sections (Explore, Health, Installed here); the saved order holds inside each.
                    let menu = bar.menu.filter(offered)
                    ForEach(groupedMenu(menu)) { section in
                        Section {
                            ForEach(section.actions) { action in barButton(action) }
                        } header: {
                            section.group.title
                        }
                    }
                    if !menu.isEmpty { Divider() }
                    Button {
                        customizingBar = true
                    } label: {
                        Label("Customize top bar", systemImage: "pencil")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .accessibilityLabel(Text("More"))
            }
        }
        .sheet(isPresented: $customizingBar) { ActionBarEditorSheet(KubernetesAction.self) }
        .sheet(isPresented: $sanitizing) {
            SanitizeSheet(namespace: scopeControl.scope.namespace ?? "")
        }
        .navigationDestination(item: $netScreen) { screen in
            switch screen {
            case .checkup: CheckupView()
            case .capacity: KubeCapacityView()
            case .apiHealth: ApiHealthView()
            case .policies: NetPoliciesView()
            case .flows(let filter): LiveFlowsView(cilium: cilium ?? CiliumStatus(), filter: filter)
            case .resources: KubeBrowserView()
            case .helm: HelmReleasesView()
            case .storage: KubeStorageView()
            case .services: KubeServicesView()
            case .jobs: KubeJobsView()
            case .events: KubeLiveEventsView()
            case .admissionPolicies: PoliciesView()
            case .autoscaling: AutoscalingView()
            case .virtualMachines: VirtualMachinesView()
            case .crossplane: CrossplaneView()
            case .tailscale: TailscaleView()
            case .argoRollouts: ArgoRolloutsView()
            case .clusterLifecycle: ClusterLifecycleView()
            case .dataServices(let kind, let solo):
                DataServicesView(hints: "", downNodes: [], selected: kind, solo: solo)
            }
        }
        .task(id: model.client?.kubeServer) {
            guard let client = model.client else { return }
            cilium = try? await client.cilium()
            operators = await KubeOperators.read(with: client)
            dataKinds = Set((try? await client.dataServices(hints: ""))?.detected ?? [])
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
        .searchable(
            text: $query, prompt: tab == .cronJobs ? Text("Name, schedule or image") : Text("Name, kind or image")
        )
        .navigationTitle(Text(verbatim: "Kubernetes"))
        .navigationBarTitleDisplayMode(.inline)
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
            linkedNamespace = nil
            if let cluster = scopeCluster { KubeScopeStore.set(picked, for: cluster) } else { localScope = picked }
            scopeEdits += 1
        }
    }

    /// The scope picked for the active cluster, nil for the default.
    private var rememberedScope: KubeScope? {
        _ = scopeEdits
        if let linkedNamespace { return KubeScope(namespace: linkedNamespace, chosen: true) }
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
    /// A share link's workload: its sheet opens once listed.
    @Binding var focus: String?

    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
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
                // Each kind listed has its own restart permission.
                KubeDeniedSection(
                    actions: kubeDistinctActions(shown.map { KubeAction.restart(kind: $0.kind) }),
                    namespace: selected ?? "")
                Section {
                    ForEach(shown) { workload in
                        WorkloadRow(
                            workload: workload, showNamespace: selected == nil,
                            restarting: restarting.contains(workload.id),
                            onOpen: { actions = workload }
                        ) { confirm = workload }
                    }
                    if load.hasMore && query.isEmpty { LoadMoreRow { list.loadMore(model: model) } }
                }
            }
            .emptyOverlay(shown.isEmpty, query: query) {
                ContentUnavailableView("No workloads", systemImage: "square.stack.3d.up")
            }
            .refreshable { await list.refresh(model: model) }
            .themedBackground()
        }
        .restartConfirmation($confirm) { workload in Task { await restart(workload) } }
        .sheet(item: $following) { workload in RolloutStatusSheet(workload: workload) { await load() } }
        .sheet(item: $actions) { workload in WorkloadActionsSheet(workload: workload) { await load() } }
        .task(id: list.loadedItems.map(\.id)) {
            guard let focus, let workload = list.loadedItems.first(where: { $0.id == focus }) else { return }
            actions = workload
            self.focus = nil
        }
        .task(
            id: ChangeTrigger(settles: list.settles, active: scenePhase == .active, namespace: control.scope.namespace)
        ) {
            await followChanges()
        }
        .messageAlert($resultMessage)
    }

    private struct ChangeTrigger: Hashable {
        let settles: Int
        let active: Bool
        let namespace: String?
    }

    /// Reads the list again each time a workload of the scope changes, while on screen and the
    /// app active: the Go core signals at most every 2 s, never for the lists it reads at the
    /// start. Restarted after each load (the refresh it causes too); a watch that ends is
    /// followed again after a while. Pull-to-refresh stays.
    private func followChanges() async {
        guard scenePhase == .active, list.settles > 0 else { return }
        let namespace = control.scope.namespace
        while !Task.isCancelled {
            guard let client = model.client else { return }
            for await event in client.changeWatch(namespace: namespace, kinds: KubeChange.workloadKinds) {
                if case .update = event { await list.refresh(model: model) }
            }
            try? await Task.sleep(for: .seconds(kubeWatchRetrySeconds))
        }
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
            // Followed app-wide too, with its Live Activity, after the sheet closes.
            RolloutJob.shared.start(client: client, workload: workload)
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
                .kubeGated(KubeAction.restart(kind: workload.kind), in: workload.namespace)
                .accessibilityLabel(Text("Restart \(workload.name)"))
            }
        }
        // Swipe right: the restart, which only opens its confirmation. VoiceOver lists it too.
        .swipeActions(edge: .leading) {
            if !restarting {
                Button(action: onRestart) {
                    Label("Restart", systemImage: "arrow.clockwise")
                }
                .tint(.blue)
                .disabled(!workload.canRestart)
                .kubeGated(KubeAction.restart(kind: workload.kind), in: workload.namespace)
            }
        }
        .contextMenu {
            if !restarting && workload.canRestart {
                restartMenuItem
            }
            Button(action: onOpen) {
                Label("Scale…", systemImage: "arrow.up.and.down")
            }
            ShareLinkButton(target: shareTarget)
            PinButton(target: shareTarget, label: workload.name)
        }
    }

    private var shareTarget: ShareTarget {
        .workload(kind: workload.kind, namespace: workload.namespace, name: workload.name)
    }

    /// The restart as a menu item, with the reason when the account may not.
    @ViewBuilder private var restartMenuItem: some View {
        if let action = KubeAction.restart(kind: workload.kind) {
            KubeGatedMenuButton(
                String(localized: "Restart"), systemImage: "arrow.clockwise", action: action, in: workload.namespace,
                perform: onRestart)
        } else {
            Button(action: onRestart) {
                Label("Restart", systemImage: "arrow.clockwise")
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
                if workload.autoscaler != nil {
                    Text("auto")
                        .font(.caption2.weight(.bold))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .foregroundStyle(workload.autoscaler?.stateOk == true ? Color.secondary : Color.statusWarn)
                        .background(
                            (workload.autoscaler?.stateOk == true ? Color.secondary : Color.statusWarn).opacity(0.15),
                            in: RoundedRectangle(cornerRadius: 4, style: .continuous)
                        )
                        .accessibilityLabel(Text("Autoscaled"))
                }
                if workload.restartedAt > 0 {
                    Text("restarted \(relativeTime(workload.restartedAt))")
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

extension KubernetesActionGroup {
    /// The section's title in the Kubernetes screen's menu and the bar editor.
    var title: Text {
        switch self {
        case .explore: Text("Explore")
        case .health: Text("Health")
        case .installed: Text("Installed here")
        }
    }
}

extension KubernetesAction: BarActionLook {
    var groupTitle: Text? { group.title }

    var systemImage: String {
        switch self {
        case .checkup: "stethoscope"
        case .networkPolicies: "shield.lefthalf.filled"
        case .share: "link"
        case .apiHealth: "heart.text.square"
        case .flows: "point.3.filled.connected.trianglepath.dotted"
        case .resources: "square.grid.3x3"
        case .helm: "shippingbox"
        // As the Kubernetes home shows them.
        case .storage: "externaldrive"
        case .services: "network"
        case .jobs: "checklist"
        case .events: "list.bullet.rectangle"
        case .capacity: "gauge.with.dots.needle.33percent"
        case .admissionPolicies: "checkmark.shield"
        case .autoscaling: "arrow.up.and.down"
        case .virtualMachines: "desktopcomputer"
        case .crossplane: "square.stack.3d.up"
        case .tailscale: "network.badge.shield.half.filled"
        case .argoRollouts: "arrow.triangle.branch"
        case .clusterLifecycle: "server.rack"
        case .dataServices: "externaldrive.connected.to.line.below"
        case .certManager, .castai: dataServiceKind?.symbol ?? "externaldrive"
        }
    }

    var title: Text {
        switch self {
        case .checkup: Text(CheckupText.checkupTitle)
        case .networkPolicies: Text("Network policies")
        case .share: Text("Share link")
        case .apiHealth: Text("API server")
        case .flows: Text("Live flows")
        case .resources: Text("Resources")
        case .helm: Text("Helm releases")
        case .storage: Text("Storage")
        case .services: Text("Services")
        case .jobs: Text("Jobs")
        case .events: Text("Kubernetes events")
        case .capacity: Text(verbatim: CapacityText.capacityTitle)
        case .admissionPolicies: Text("Policies")
        case .autoscaling: Text("Autoscaling")
        case .virtualMachines: Text("Virtual machines")
        case .crossplane: Text("Crossplane")
        case .tailscale: Text("Tailscale")
        case .argoRollouts: Text("Argo Rollouts")
        case .clusterLifecycle: Text("Cluster lifecycle")
        case .dataServices: Text("Data services")
        // Product names: never translated.
        case .certManager, .castai: Text(verbatim: dataServiceKind?.title ?? "")
        }
    }
}

/// Which operator screens the cluster has, as the Kubernetes home finds them: each read answers
/// "not installed" quickly when its API group is not served; a failed read (no access) offers nothing.
struct KubeOperators: Equatable {
    var crossplane = false
    var tailscale = false
    var argoRollouts = false
    var clusterLifecycle = false

    static func read(with client: TalosClient) async -> KubeOperators {
        var found = KubeOperators()
        found.crossplane = (try? await client.crossplane().installed) ?? false
        found.tailscale = (try? await client.tailscale().installed) ?? false
        found.argoRollouts = (try? await client.argoRollouts().installed) ?? false
        found.clusterLifecycle = (try? await client.clusterLifecycle().installed) ?? false
        return found
    }
}
