import SwiftUI
import IchorCore

/// The Jobs of the Kubernetes screens' namespace scope: failed ones first (with why), then
/// suspended and running ones, newest first, each with how long it ran, its completions and
/// the CronJob that started it. A Job opens its summary; swiping it offers the object screen's
/// generic delete.
struct KubeJobsView: View {
    @Environment(AppModel.self) private var model
    @State private var query = ""
    @State private var deleting: JobRow?

    private static let jobs = KubeAPIResource(group: "batch", resource: "jobs", kind: "Job", scalable: true)
    private static let cronJobs = KubeAPIResource(group: "batch", resource: "cronjobs", kind: "CronJob")

    var body: some View {
        KubeScopedList(
            title: Text("Jobs"),
            prompt: Text("Search jobs, CronJobs or reasons"),
            query: $query,
            fetch: { client, scope in try await client.jobs(namespace: scope.namespace) },
            namespaces: { value in value.jobs.map(\.namespace) }
        ) { jobs, scope, reload in
            list(jobs, showNamespace: scope.namespace == nil, reload: reload)
        }
    }

    private func list(_ jobs: KubeJobs, showNamespace: Bool, reload: @escaping () async -> Void) -> some View {
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
        .sheet(item: $deleting) { job in
            KubeObjectDeleteSheet(resource: Self.jobs, namespace: job.namespace, name: job.name, denial: nil) {
                Task { await reload() }
            }
        }
        .refreshable { await reload() }
        .themedBackground()
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
