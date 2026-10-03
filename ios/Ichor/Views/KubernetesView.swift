import SwiftUI
import IchorCore

/// The cluster's Kubernetes side, through the Kubernetes API with the admin kubeconfig Talos
/// issues (os:admin): workloads with rollout restart, and pods. The namespace filter and the
/// search carry over between the two.
struct KubernetesView: View {
    enum Tab: Hashable { case workloads, pods }

    @State private var tab = Tab.workloads
    @State private var namespace: String?
    @State private var query = ""

    var body: some View {
        Group {
            switch tab {
            case .workloads: WorkloadsList(namespace: $namespace, query: query)
            case .pods: PodsList(namespace: $namespace, query: query)
            }
        }
        .safeAreaInset(edge: .top) {
            Picker(selection: $tab) {
                Text("Workloads").tag(Tab.workloads)
                Text("Pods").tag(Tab.pods)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.bottom, 6)
            .background(.bar)
        }
        .searchable(text: $query, prompt: Text("Name, kind or image"))
        .navigationTitle(Text(verbatim: "Kubernetes"))
        .navigationBarTitleDisplayMode(.inline)
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

/// Deployments, StatefulSets and DaemonSets with a rolling restart like `kubectl rollout restart`.
private struct WorkloadsList: View {
    @Binding var namespace: String?
    let query: String

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<[KubeWorkload]> = .loading
    @State private var confirm: KubeWorkload?
    @State private var restarting: Set<String> = []
    @State private var resultMessage: String?

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
                                    restarting: restarting.contains(workload.id)) { confirm = workload }
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
        .confirmationDialog(confirm.map { String(localized: "Restart \($0.kind) \($0.name)?") } ?? "",
                            isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }),
                            titleVisibility: .visible,
                            presenting: confirm) { workload in
            Button("Restart", role: .destructive) {
                Task { await restart(workload) }
            }
            Button("Cancel", role: .cancel) {}
        } message: { workload in
            if workload.desired <= 1 {
                Text("Its pods in \(workload.namespace) are replaced with a rolling update, like kubectl rollout restart.") +
                    Text(verbatim: " ") + Text("With a single pod, the workload is briefly unavailable.")
            } else {
                Text("Its pods in \(workload.namespace) are replaced with a rolling update, like kubectl rollout restart.")
            }
        }
        .alert(resultMessage ?? "", isPresented: Binding(get: { resultMessage != nil }, set: { if !$0 { resultMessage = nil } })) {
            Button("OK") {}
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.workloads() }
    }

    private func restart(_ workload: KubeWorkload) async {
        guard let client = model.client, !restarting.contains(workload.id) else { return }
        restarting.insert(workload.id)
        defer { restarting.remove(workload.id) }
        do {
            try await client.rolloutRestart(workload)
            resultMessage = String(localized: "\(workload.name) is restarting")
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
    let onRestart: () -> Void

    var body: some View {
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
                    let at = Date(timeIntervalSince1970: TimeInterval(workload.restartedAt) / 1000)
                    Text("restarted \(at.formatted(.relative(presentation: .named)))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            if restarting {
                ProgressView()
            } else {
                Button(action: onRestart) { Image(systemName: "arrow.clockwise.circle") }
                    .buttonStyle(.borderless)
                    .disabled(!workload.canRestart)
                    .accessibilityLabel(Text("Restart \(workload.name)"))
            }
        }
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
