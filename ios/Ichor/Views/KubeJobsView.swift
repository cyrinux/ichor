import SwiftUI
import IchorCore

/// The Jobs of the Kubernetes screens' namespace scope: failed ones first (with why), then
/// suspended and running ones, newest first, each with how long it ran, its completions and
/// the CronJob that started it. A Job opens its summary; swiping it offers the object screen's
/// generic delete.
struct KubeJobsView: View {
    @Environment(AppModel.self) private var model
    @State private var scope = KubeBrowserScope()
    @State private var state: LoadState<KubeJobs> = .loading
    @State private var query = ""
    @State private var deleting: JobRow?

    /// What decides the list: the scope, and the cluster, API address and screenshot mode.
    private struct Trigger: Hashable {
        let scope: KubeScope
        let ready: Bool
        let source: String
    }

    private static let jobs = KubeAPIResource(group: "batch", resource: "jobs", kind: "Job", scalable: true)
    private static let cronJobs = KubeAPIResource(group: "batch", resource: "cronjobs", kind: "CronJob")

    var body: some View {
        let control = scope.control(model: model)
        VStack(spacing: 0) {
            KubeScopeBar(control: control, loaded: loadedNamespaces)
                .background(.bar)
            Divider()
            if !control.ready {
                ContentUnavailableView {
                    Label("Namespace", systemImage: "square.dashed")
                } description: {
                    Text("Type the namespace to list above.")
                }
                .frame(maxHeight: .infinity)
            } else {
                LoadStateView(state: state, retry: { await load(control.scope) }) { jobs in
                    list(jobs, showNamespace: control.scope.namespace == nil, scope: control.scope)
                }
            }
        }
        .searchable(text: $query, prompt: Text("Search jobs, CronJobs or reasons"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text("Jobs"))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $deleting) { job in
            KubeObjectDeleteSheet(resource: Self.jobs, namespace: job.namespace, name: job.name, denial: nil) {
                Task { await load(control.scope) }
            }
        }
        .task(id: kubeNamespacesKey(model)) { await scope.loadNamespaces(model: model) }
        .task(id: Trigger(scope: control.scope, ready: control.ready, source: kubeNamespacesKey(model))) {
            if control.ready { await load(control.scope) }
        }
    }

    private func list(_ jobs: KubeJobs, showNamespace: Bool, scope: KubeScope) -> some View {
        let shown = filterJobs(jobs.jobs, query: query)
        return List {
            ForEach(shown) { job in
                NavigationLink {
                    KubeObjectView(resource: Self.jobs, namespace: job.namespace, name: job.name)
                } label: {
                    JobRowView(job: job, showNamespace: showNamespace)
                }
                .swipeActions {
                    Button(role: .destructive) { deleting = job } label: { Label("Delete", systemImage: "trash") }
                }
                if !job.owner.isEmpty {
                    NavigationLink {
                        KubeObjectView(resource: Self.cronJobs, namespace: job.namespace, name: job.owner)
                    } label: {
                        Label { Text("CronJob \(job.owner)") } icon: { Image(systemName: "clock.arrow.circlepath") }
                            .font(.footnote)
                    }
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView("No jobs.", systemImage: "checklist")
        }
        .refreshable { await load(scope) }
        .themedBackground()
    }

    private var loadedNamespaces: [String] {
        guard case .loaded(let jobs, _, _) = state else { return [] }
        return Array(Set(jobs.jobs.map(\.namespace))).sorted()
    }

    private func load(_ scope: KubeScope) async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<KubeJobs> = await .from { try await client.jobs(namespace: scope.namespace) }
        guard key == kubeNamespacesKey(model), !Task.isCancelled else { return }
        state = state.refreshed(with: fetched)
    }
}

/// A Job: name and state (coloured by level), how long it ran, its completions, "manual",
/// and why it failed.
private struct JobRowView: View {
    let job: JobRow
    let showNamespace: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: showNamespace ? job.id : job.name)
                    .font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                stateLabel.font(.caption).foregroundStyle(levelColor)
            }
            let facts = [job.durationSeconds.map { localizedDuration($0) },
                         job.completions.isEmpty ? nil : String(localized: "Completions: \(job.completions)"),
                         job.manual ? String(localized: "manual") : nil].compactMap { $0 }
            if !facts.isEmpty {
                Text(verbatim: facts.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
            }
            if !job.reason.isEmpty {
                Text(verbatim: job.reason).font(.caption).foregroundStyle(levelColor)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var stateLabel: Text {
        switch job.runState {
        case nil: Text("suspended")
        case .running: Text("running")
        case .succeeded: Text("succeeded")
        case .failed: Text("failed")
        case .never: Text(verbatim: job.state)
        }
    }

    private var levelColor: Color {
        switch job.level {
        case .critical: .statusBad
        case .warning: .statusWarn
        case .ok: .secondary
        }
    }
}
