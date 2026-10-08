import SwiftUI
import IchorCore

/// The Kubernetes API server's health and what puts pressure on it (os:admin): its readyz and
/// livez checks, the load over the last seconds, which clients (flow schemas) send requests,
/// how full each priority level is, the busiest requests, what waits in the queues and who
/// sent it, open watches and the largest object counts. Reading it takes a few seconds (two
/// /metrics scrapes): loaded on demand, never polled.
struct ApiHealthView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ApiHealthReport> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            List {
                ApiVerdictSection(report: report)
                // The metrics group clients; the audit log names them, and what they do wrong.
                // It is read through the Talos API: a cluster added from a kubeconfig has none.
                Section {
                    if model.activeIsKube {
                        Text("The audit log is read through the Talos API: this cluster was added from a kubeconfig.")
                            .font(.footnote).foregroundStyle(.secondary)
                    } else {
                        NavigationLink { AuditView() } label: {
                            Label("Find who loads it (audit log)", systemImage: "person.fill.questionmark")
                        }
                    }
                }
                if !report.metricsError.isEmpty {
                    Section { ErrorOrNoticeText(message: String(localized: "The metrics could not be read: \(report.metricsError)")) }
                } else {
                    ApiLoadSection(report: report)
                    ApiClientsSection(report: report)
                    ApiPrioritiesSection(priorities: report.priorities)
                    ApiRequestsSection(requests: report.requests)
                    ApiQueuedSection(queued: report.queuedRequests)
                    ApiCountsSection(title: "Watches by resource", rows: report.watchedKinds)
                    ApiCountsSection(title: "Most stored objects", rows: report.objects)
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle("API server")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.apiHealth() })
    }
}

extension ApiStatus {
    var label: String {
        switch self {
        case .ok: String(localized: "Healthy")
        case .busy: String(localized: "Busy")
        case .throttling: String(localized: "Throttling")
        case .unhealthy: String(localized: "Unhealthy")
        }
    }

    var hint: LocalizedStringKey {
        switch self {
        case .ok: "Every check passes and requests run without waiting."
        case .busy: "Requests wait in queues, or a share of the answers are server errors."
        case .throttling: "The server turns requests away (429): a client sends more than its share."
        case .unhealthy: "A readiness or liveness check fails."
        }
    }

    var color: Color {
        switch self {
        case .ok: .green
        case .busy: .orange
        case .throttling, .unhealthy: .red
        }
    }
}

/// The verdict and why, version and uptime, what the rates cover, and the failing checks.
private struct ApiVerdictSection: View {
    let report: ApiHealthReport

    var body: some View {
        Section {
            HStack {
                StatusPill(label: report.status.label, color: report.status.color)
                Spacer()
                if !report.version.isEmpty {
                    Text(verbatim: report.uptimeSeconds > 0 ? "\(report.version) · \(localizedDuration(report.uptimeSeconds))" : report.version)
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }
            }
            Text(report.status.hint).font(.callout)
            ForEach([("readyz", report.ready), ("livez", report.live)], id: \.0) { name, probe in
                if !probe.error.isEmpty {
                    ErrorOrNoticeText(message: String(localized: "\(name) did not answer: \(probe.error)"))
                }
            }
            if !report.ready.checks.isEmpty {
                Text("\(report.ready.checks.filter(\.ok).count) of \(report.ready.checks.count) readiness checks pass")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            ForEach(report.failedChecks) { check in
                Text(verbatim: check.reason.isEmpty ? check.name : "\(check.name): \(check.reason)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.red)
            }
        } footer: {
            if report.metricsError.isEmpty {
                if report.ratesAreLive {
                    Text("Rates over the last \(Int(report.windowSeconds.rounded())) s")
                } else {
                    Text("Averages since the API server started: two servers answered the scrapes.")
                }
            }
        }
    }
}

private struct ApiLoadSection: View {
    let report: ApiHealthReport

    var body: some View {
        Section("Load") {
            row("Requests", formatRate(report.requestRate))
            row("Server errors (5xx)", formatRate(report.errorRate), alert: report.errorRate > 0 ? .orange : nil)
            row("Throttled (429)", formatRate(report.throttledRate), alert: report.throttledRate > 0 ? .red : nil)
            row("Rejected by priority and fairness", formatRate(report.rejectedRate), alert: report.rejectedRate > 0 ? .red : nil)
            LabeledContent("In flight") {
                Text("\(report.inflightRead) read · \(report.inflightMutate) write").monospacedDigit()
            }
            row("Queued", "\(report.queued)", alert: report.queued > 0 ? .orange : nil)
            row("Open watches", "\(report.watches)")
            row("Watch events", formatRate(report.watchEventRate))
            if report.etcdLatencyMs > 0 { row("etcd latency", formatMs(report.etcdLatencyMs)) }
        }
    }

    private func row(_ label: LocalizedStringKey, _ value: String, alert: Color? = nil) -> some View {
        LabeledContent(label) {
            Text(verbatim: value).monospacedDigit().foregroundStyle(alert ?? .secondary)
        }
    }
}

/// The flow schemas, busiest first, with a bar for their share of the busiest one.
private struct ApiClientsSection: View {
    let report: ApiHealthReport

    var body: some View {
        if !report.clients.isEmpty {
            let top = report.clients.map(\.rate).max() ?? 0
            Section {
                ForEach(report.clients) { flow in
                    VStack(alignment: .leading, spacing: 4) {
                        ApiNameValue(name: flow.name, value: formatRate(flow.rate), sub: flow.priority)
                        if top > 0 { ProgressView(value: min(flow.rate / top, 1)).tint(.accentColor) }
                        if flow.rejectedRate > 0 || flow.queued > 0 {
                            HStack(spacing: 6) {
                                if flow.rejectedRate > 0 { InfoChip(text: String(localized: "\(formatRate(flow.rejectedRate)) rejected"), color: .red) }
                                if flow.queued > 0 { InfoChip(text: String(localized: "\(flow.queued) queued"), color: .orange) }
                                if flow.waitMs > 0 { InfoChip(text: String(localized: "waits \(formatMs(flow.waitMs))"), color: .orange) }
                            }
                        }
                    }
                }
            } header: {
                Text("Who sends requests")
            } footer: {
                Text("Clients grouped by flow schema, busiest first. Per-user detail needs the audit log.")
            }
        }
    }
}

private struct ApiPrioritiesSection: View {
    let priorities: [ApiPriority]

    var body: some View {
        if !priorities.isEmpty {
            Section("Priority levels") {
                ForEach(priorities) { level in
                    let seats = Int(level.executing.rounded())
                    VStack(alignment: .leading, spacing: 4) {
                        if let share = level.share {
                            ApiNameValue(name: level.name, value: String(localized: "\(seats) of \(Int(level.limit.rounded())) seats"))
                            ProgressView(value: share).tint(share >= ApiPriority.full ? .red : share >= 0.6 ? .orange : .green)
                        } else {
                            ApiNameValue(name: level.name, value: String(localized: "\(seats) seats, no limit"))
                        }
                        if level.queued > 0 || level.rejectedRate > 0 {
                            HStack(spacing: 6) {
                                if level.queued > 0 { InfoChip(text: String(localized: "\(level.queued) queued"), color: .orange) }
                                if level.rejectedRate > 0 { InfoChip(text: String(localized: "\(formatRate(level.rejectedRate)) rejected"), color: .red) }
                            }
                        }
                    }
                }
            }
        }
    }
}

private struct ApiRequestsSection: View {
    let requests: [ApiRequestRow]

    var body: some View {
        Section("Busiest requests") {
            if requests.isEmpty {
                Text("No request in the window.").foregroundStyle(.secondary)
            }
            ForEach(requests) { row in
                HStack(spacing: 8) {
                    InfoChip(text: row.verb, monospaced: true, color: .accentColor)
                    VStack(alignment: .leading, spacing: 2) {
                        if row.resource.isEmpty {
                            Text("Non-resource paths").font(.callout)
                        } else {
                            Text(verbatim: row.resource).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                        }
                        if row.latencyMs > 0 {
                            Text("mean \(formatMs(row.latencyMs))").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    Spacer()
                    if row.errorRate > 0 { InfoChip(text: String(localized: "\(formatRate(row.errorRate)) errors"), color: .orange) }
                    Text(verbatim: formatRate(row.rate)).font(.callout.monospaced())
                }
            }
        }
    }
}

/// The requests waiting now, with who sent them.
private struct ApiQueuedSection: View {
    let queued: [ApiQueued]

    var body: some View {
        if !queued.isEmpty {
            Section("Waiting in queues now") {
                ForEach(Array(queued.enumerated()), id: \.offset) { _, q in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: q.user.isEmpty ? q.flowSchema : q.user).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                        Text(verbatim: "\(q.verb) \(q.path)").font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                        Text(verbatim: "\(q.flowSchema) → \(q.priority)").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }
}

private struct ApiCountsSection: View {
    let title: LocalizedStringKey
    let rows: [ApiCount]

    var body: some View {
        if !rows.isEmpty {
            Section(title) {
                ForEach(rows) { ApiNameValue(name: $0.resource, value: "\($0.count)") }
            }
        }
    }
}

private struct ApiNameValue: View {
    let name: String
    let value: String
    var sub = ""

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                if !sub.isEmpty { Text(verbatim: sub).font(.caption).foregroundStyle(.secondary) }
            }
            Spacer()
            Text(verbatim: value).font(.callout.monospaced())
        }
    }
}
