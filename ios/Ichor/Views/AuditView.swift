import SwiftUI
import IchorCore

/// Who loads the Kubernetes API server and what they do wrong, from the control planes' audit
/// logs read through the Talos API (os:admin): each problem with the client at fault, what it
/// does and what to change, then the busiest clients and what was read. Reading moves tens of
/// MB: only when asked, never polled.
struct AuditView: View {
    @Environment(AppModel.self) private var model
    @State private var minutes = 15
    @State private var state: LoadState<AuditReport>?

    private static let windows = [5, 15, 60]

    var body: some View {
        List {
            Section {
                Text("Ichor reads the Kubernetes audit log of each control plane through the Talos API and looks for the clients that load the API server or keep failing. Talos logs every request by default; reading takes a few tens of MB per node, compressed.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                Picker(selection: $minutes) {
                    ForEach(Self.windows, id: \.self) { Text("Last \($0) min").tag($0) }
                } label: {
                    EmptyView()
                }
                .pickerStyle(.segmented)
                Button {
                    Task { await load() }
                } label: {
                    HStack {
                        Text("Analyze")
                        if isLoading {
                            Spacer()
                            ProgressView()
                        }
                    }
                }
                .disabled(isLoading)
                if isLoading {
                    Text("Reading the audit logs of the control planes…").font(.caption).foregroundStyle(.secondary)
                }
            }
            if let state {
                AuditResult(state: state)
            }
        }
        .themedBackground()
        .navigationTitle("Who loads the API")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var isLoading: Bool {
        if case .some(.loading) = state { return true }
        return false
    }

    private func load() async {
        guard let client = model.client else { return }
        state = .loading
        let window = minutes
        state = await .from { try await client.auditAnalysis(minutes: window) }
    }
}

private struct AuditResult: View {
    let state: LoadState<AuditReport>

    var body: some View {
        switch state {
        case .loading:
            EmptyView()
        case .failed(let message):
            Section { ErrorOrNoticeText(message: message) }
        case .loaded(let report, _, _):
            AuditReportSections(report: report)
        }
    }
}

private struct AuditReportSections: View {
    let report: AuditReport

    var body: some View {
        Section {
            Text("\(report.requests) requests over \(localizedDuration(Int64(report.seconds))) from \(report.nodes.count) control planes, \(formatMegabytes(report.bytesRead)) read")
                .font(.callout)
        }
        if report.requests == 0 {
            Section { Text("No request in the audit logs: auditing may be turned off (audit policy level None).").foregroundStyle(.secondary) }
        } else {
            Section("What is wrong") {
                if report.findings.isEmpty {
                    Text("Nothing wrong found: every client behaves.").foregroundStyle(.secondary)
                }
                ForEach(Array(report.findings.enumerated()), id: \.offset) { _, finding in
                    AuditFindingRow(finding: finding)
                }
            }
            if !report.actors.isEmpty {
                Section("Busiest clients") {
                    ForEach(report.actors) { AuditActorView(row: $0) }
                }
            }
        }
        Section("Control planes read") {
            ForEach(report.nodes) { node in
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text(verbatim: node.node).font(.caption.monospaced())
                        Spacer()
                        if node.error.isEmpty {
                            Text("\(formatMegabytes(node.bytes)) · \(node.events) events").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    if !node.error.isEmpty { ErrorOrNoticeText(message: node.error) }
                }
            }
        }
    }
}

extension AuditSeverity {
    var symbol: String {
        switch self {
        case .critical: "exclamationmark.octagon.fill"
        case .warning: "exclamationmark.triangle.fill"
        case .info: "info.circle"
        }
    }

    var color: Color {
        switch self {
        case .critical: .red
        case .warning: .orange
        case .info: .secondary
        }
    }
}

/// One problem: who, what happens, the objects it touches and what to change.
private struct AuditFindingRow: View {
    let finding: AuditFinding

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: finding.severity.symbol).foregroundStyle(finding.severity.color).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                if finding.aboutServer {
                    Text("API server").font(.subheadline.weight(.semibold))
                } else if let actor = finding.actor {
                    Text(verbatim: actor.label).font(.subheadline.monospaced().weight(.semibold)).lineLimit(1).truncationMode(.middle)
                    if let agent = actor.agentDetail {
                        Text(verbatim: agent).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Text(verbatim: finding.title).font(.callout)
                if finding.objects > 1 {
                    Text("\(finding.objects) in all, e.g. \(finding.examples.joined(separator: ", "))")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                if let fix = finding.fix {
                    Text(verbatim: fix).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 2)
    }
}

/// A busy client: its rate and share, what it mostly does, its errors.
private struct AuditActorView: View {
    let row: AuditActorRow

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text(verbatim: row.actor.label).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                Spacer()
                Text(verbatim: formatRate(row.rate)).font(.callout.monospaced())
            }
            if let agent = row.actor.agentDetail {
                Text(verbatim: agent).font(.caption).foregroundStyle(.secondary)
            }
            Text("\(Int((row.share * 100).rounded()))% of requests, mostly \(row.topVerb) \(row.topResource)")
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if row.throttled > 0 || row.errors > 0 {
                HStack(spacing: 6) {
                    if row.throttled > 0 { InfoChip(text: String(localized: "\(row.throttled) throttled"), color: .red) }
                    if row.errors > 0 { InfoChip(text: String(localized: "\(row.errors) errors"), color: .orange) }
                }
            }
        }
    }
}
