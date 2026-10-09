import SwiftUI
import IchorCore

/// The Kubernetes pods of one node or workload (`selection`, os:admin), page by page in the API
/// server's order: the first page at once, the next ones on scroll (the Linear plan document "U11. Large clusters: home at scale, namespace-first paged lists",
/// Phase 5), narrowed to a phase, with the Pods tab's rows: logs and delete. Never kept as the
/// last known state.
struct SelectedPodsView: View {
    let selection: PodSelection

    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var phase = PodPhaseFilter.all
    /// The phase `list` loads.
    @State private var listed = PodPhaseFilter.all
    @State private var list: PagedList<KubePod>
    @State private var actions = PodActions()
    /// The node's Kubernetes name, asked of Talos once for every phase's list.
    @State private var kubeNode: KubeNodeNameCache

    /// What decides the rows: the phase, and the cluster, API address and screenshot mode.
    private struct Trigger: Hashable {
        let phase: PodPhaseFilter
        let context: String
        let server: String?
        let generation: Int
    }

    /// What decides whether the rows are followed live: the rows, the loads of them that
    /// settled (the watch starts over after each), and the app being active.
    private struct LiveTrigger: Hashable {
        let rows: Trigger
        let settles: Int
        let active: Bool
    }

    private var trigger: Trigger {
        Trigger(phase: phase, context: model.activeContext, server: model.client?.kubeServer, generation: model.dataGeneration)
    }

    init(selection: PodSelection) {
        self.selection = selection
        let cache = KubeNodeNameCache()
        _kubeNode = State(initialValue: cache)
        _list = State(initialValue: Self.makeList(selection, phase: .all, kubeNode: cache))
    }

    var body: some View {
        VStack(spacing: 0) {
            Picker("Phase", selection: $phase) {
                ForEach(PodPhaseFilter.allCases, id: \.self) { Text($0.label).tag($0) }
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.vertical, 8)
            Divider()
            LoadStateView(state: list.state, retry: { await list.refresh(model: model) }) { load in
                VStack(spacing: 0) {
                    PagedProgressBar(progress: list.progress)
                    rows(load)
                }
            }
        }
        .task(id: trigger) {
            if listed != phase {
                list = Self.makeList(selection, phase: phase, kubeNode: kubeNode)
                listed = phase
            }
            await list.show(KubeScope(), model: model)
        }
        .task(id: LiveTrigger(rows: trigger, settles: list.settles, active: scenePhase == .active)) { await follow() }
        .podActions(actions) { await list.refresh(model: model) }
        .loadsKubeActionAccess(namespace: accessNamespace)
    }

    /// Keeps a workload's pods live while on screen and the app active: the Go core's watch
    /// replaces the list, then adds, updates and removes rows as the API server reports them.
    /// It starts once a load settled, and over again after each one (a refresh, a phase
    /// change): the list the watch sends then is newer than the load's, so no change seen
    /// meanwhile is lost. A watch that ends (a refusal, the network) is followed again after a
    /// while; pull-to-refresh stays. A node's pods are not followed.
    private func follow() async {
        guard scenePhase == .active, list.settles > 0, case .workload(let kind, let namespace, let name) = selection else { return }
        while !Task.isCancelled {
            guard let client = model.client else { return }
            for await event in client.workloadPodsWatch(kind: kind, namespace: namespace, name: name, phase: phase) {
                if case .change(let change) = event {
                    list.apply { $0.applying(change, key: \.id) }
                }
            }
            try? await Task.sleep(for: .seconds(kubeWatchRetrySeconds))
        }
    }

    /// The workload's namespace; "" for a node's pods (every namespace).
    private var accessNamespace: String {
        if case .workload(_, let namespace, _) = selection { return namespace }
        return ""
    }

    private func rows(_ load: PagedLoad<KubePod>) -> some View {
        List {
            KubeDeniedSection(actions: [.deletePod], namespace: accessNamespace)
            Section {
                // In the server's order: sorting would move rows as pages arrive.
                ForEach(load.items) { pod in
                    PodRow(pod: pod, showNamespace: selection.showsNamespace, showNode: selection.showsNode,
                           deleting: actions.deleting.contains(pod.id), onLogs: { actions.logsPod = pod }) {
                        actions.confirm = pod
                    }
                    .podLogsSwipe { actions.logsPod = pod }
                    .contextMenu {
                        Button { actions.logsPod = pod } label: { Label("Logs", systemImage: "doc.text") }
                    }
                }
                if load.hasMore { LoadMoreRow { list.loadMore(model: model) } }
            }
        }
        .overlay {
            if load.items.isEmpty { ContentUnavailableView("No pods", systemImage: "cube") }
        }
        .refreshable { await list.refresh(model: model) }
        .themedBackground()
    }

    /// The list of `selection`'s pods in `phase`: the first page as full objects (images,
    /// containers), the next ones as Table rows (L9, L10).
    private static func makeList(_ selection: PodSelection, phase: PodPhaseFilter, kubeNode: KubeNodeNameCache) -> PagedList<KubePod> {
        PagedList<KubePod>(base: "selectedpods", persist: false, eagerRows: selectedPodsPageSize,
                           detailed: { pods in pods.allSatisfy { !$0.images.isEmpty } }) { client, _, token in
            switch selection {
            case .node(let address):
                let name = try await kubeNode.resolve { try await client.kubeNodeName(node: address) }
                return try await client.nodePodsPage(kubeNode: name, phase: phase, token: token, table: !token.isEmpty)
            case .workload(let kind, let namespace, let name):
                return try await client.workloadPodsPage(kind: kind, namespace: namespace, name: name, phase: phase,
                                                         token: token, table: !token.isEmpty)
            }
        }
    }
}

/// A Talos node's Kubernetes name, asked once (it does not change while the screen is open).
actor KubeNodeNameCache {
    private var known: String?

    func resolve(_ fetch: @Sendable () async throws -> String) async throws -> String {
        if let known { return known }
        let name = try await fetch()
        known = name
        return name
    }
}

extension PodPhaseFilter {
    var label: String {
        switch self {
        case .all: String(localized: "All")
        case .running: String(localized: "Running")
        case .notCompleted: String(localized: "Not completed")
        }
    }
}

/// The Kubernetes pods scheduled on a node: the API server's view, next to the node's Talos
/// (CRI) Pods tab.
struct NodeKubePodsView: View {
    let node: String
    let hostname: String

    var body: some View {
        SelectedPodsView(selection: .node(node))
            .navigationTitle(Text("Kubernetes pods"))
            .navigationBarTitleDisplayMode(.inline)
    }
}

/// The pods of a workload, pushed from its actions sheet or an app's sheet.
struct WorkloadPodsView: View {
    let workload: KubeWorkload
    let selection: PodSelection

    var body: some View {
        SelectedPodsView(selection: selection)
            .navigationTitle(Text("Pods of \(workload.name)"))
            .navigationBarTitleDisplayMode(.inline)
    }
}
