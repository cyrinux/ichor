import SwiftUI
import IchorCore

/// ExternalDNS: every controller with its settings (provider, policy, owner id, domain filters,
/// sources, interval), then the hostnames it is asked to publish, problems first. A record's row
/// opens its source object's summary. ExternalDNS keeps no status on records: nothing here says
/// one exists at the provider, so "Check names" resolves them from the phone and compares the
/// answers with the targets.
struct ExternalDnsList: View {
    let status: ExternalDnsStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var running = false
    @State private var result: DnsCheckResult?
    /// The records the last check was sent, in order: its results pair with them by position.
    @State private var checkedRecords: [ExternalDnsRecord] = []
    @State private var errorMessage: String?

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            // The check leads the list: below the controllers it was scrolled out of sight.
            if !status.records.isEmpty { checkSection }
            Section {
                if status.controllers.isEmpty && status.records.isEmpty {
                    Text("No ExternalDNS controllers or records.").foregroundStyle(.secondary)
                }
                ForEach(status.controllers) { controller in
                    ControllerRow(controller: controller)
                }
            }
            if status.truncated {
                Section {
                    Text(String(localized: "Showing the first \(status.records.count) of \(status.recordCount) hostnames."))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            Section {
                ForEach(status.records) { record in
                    RecordRow(record: record, verdict: verdicts[record.id])
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }

    /// The last check's verdicts by record id (name, type and source), for the record rows.
    private var verdicts: [String: DnsCheckEntry] {
        result?.verdicts(for: checkedRecords) ?? [:]
    }

    /// The "Check names" button, whether a check ran, what it does, and the last check's summary.
    /// The names go to the phone's DNS resolver: that is the feature, and the hint says so.
    /// Nothing runs without the tap; the verdicts stay on screen until the next check. Names with
    /// no target to compare with are counted apart.
    private var checkSection: some View {
        Section {
            HStack(spacing: 12) {
                Button { Task { await check() } } label: {
                    Text(String(localized: "Check names"))
                }
                .disabled(running)
                if running { ProgressView() }
            }
            if running {
                Text(String(localized: "Checking…")).font(.caption).foregroundStyle(.secondary)
            } else if result == nil && errorMessage == nil {
                Text(String(localized: "Not checked yet")).font(.caption).foregroundStyle(.secondary)
            }
            Text(String(localized: "Resolves every hostname from this phone and compares it with the record's targets. The names go to your DNS resolver."))
                .font(.caption).foregroundStyle(.secondary)
            if let errorMessage {
                Text(verbatim: errorMessage).font(.caption).foregroundStyle(.statusBad)
            }
            if let result {
                Text(verbatim: summary(result))
                    .font(.subheadline)
                    .foregroundStyle(summaryColor(result))
                let unverified = result.results.filter { $0.verdict == .unverified }.count
                if unverified > 0 {
                    Text(String(localized: "\(unverified) could not be compared"))
                        .font(.caption).foregroundStyle(.secondary)
                }
                if result.results.contains(where: { $0.verdict == .mismatch }) {
                    Text(String(localized: "A name that points elsewhere may be split-horizon DNS at home, a change still propagating, or another ExternalDNS owning it."))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    /// "3 of 5 names point at this cluster": every answer among the records' targets, of all of them.
    private func summary(_ result: DnsCheckResult) -> String {
        let pointing = result.results.filter { $0.verdict == .match }.count
        let total = result.results.count
        return pointing == 1
            ? String(localized: "\(pointing) of \(total) name points at this cluster")
            : String(localized: "\(pointing) of \(total) names point at this cluster")
    }

    /// Red when a name is gone (nothing publishes it), green when every one of them resolves here,
    /// amber otherwise: split-horizon DNS or propagation still worth a look.
    private func summaryColor(_ result: DnsCheckResult) -> Color {
        if result.results.contains(where: { $0.verdict == .missing }) { return .red }
        let pointing = result.results.filter { $0.verdict == .match }.count
        return pointing == result.results.count ? .green : attentionColor
    }

    private func check() async {
        guard !running else { return }
        running = true
        defer { running = false }
        do {
            let records = status.records
            result = try await TalosClient.dnsCheck(records, controllers: status.controllers,
                                                    demo: model.activeSummary?.demo == true)
            checkedRecords = records
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}

private struct ControllerRow: View {
    let controller: ExternalDnsController

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: controller.health).padding(.top, 5)
                Text(verbatim: controller.label).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                switch controller.health {
                case .critical:
                    StatusPill(label: String(localized: "not ready"), color: .red)
                case .warning where controller.dryRun:
                    StatusPill(label: String(localized: "dry-run"), color: attentionColor)
                case _ where controller.ready:
                    StatusPill(label: String(localized: "ready"), color: .green)
                default:
                    EmptyView()
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                LabeledContent("Provider", value: [controller.provider, controller.policy].filter { !$0.isEmpty }.joined(separator: " · "))
                LabeledContent("Owner id", value: controller.ownerId)
                if !controller.domainFilters.isEmpty { LabeledContent("Domain filters", value: controller.domainFilters.joined(separator: ", ")) }
                LabeledContent("Sources", value: controller.sources.joined(separator: ", "))
                LabeledContent("Interval", value: String(localized: "\(controller.intervalSeconds) s"))
                if !controller.txtPrefix.isEmpty || !controller.txtSuffix.isEmpty {
                    LabeledContent("TXT prefix/suffix", value: [controller.txtPrefix, controller.txtSuffix].filter { !$0.isEmpty }.joined(separator: " · "))
                }
                if !controller.annotationFilter.isEmpty { LabeledContent("Annotation filter", value: controller.annotationFilter) }
                ForEach(controller.reasons, id: \.self) { reason in
                    Text(verbatim: reasonText(reason)).font(.caption)
                        .foregroundStyle(reason == .notReady ? Color.red : attentionColor)
                }
            }
            .font(.caption)
        }
    }

    private func reasonText(_ reason: ExternalDnsReason) -> String {
        switch reason {
        case .notReady: String(localized: "no replica ready")
        case .dryRun: String(localized: "dry-run: it changes nothing")
        case .stale: String(localized: "a newer version is not observed yet")
        case .noTarget: String(localized: "no address to point at yet")
        case .unmanaged: String(localized: "outside every controller's domain filters")
        }
    }
}

private struct RecordRow: View {
    let record: ExternalDnsRecord
    /// What the last check found for this hostname, nil before it runs.
    let verdict: DnsCheckEntry?
    @Environment(AppModel.self) private var model

    var body: some View {
        NavigationLink {
            KubeObjectView(
                resource: KubeAPIResource(group: record.sourceGroup, version: record.sourceVersion.isEmpty ? "v1" : record.sourceVersion,
                                          resource: record.sourceResource, kind: record.sourceKind),
                namespace: record.sourceNamespace,
                name: record.sourceName,
            )
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .top, spacing: 12) {
                    HealthDot(health: record.health).padding(.top, 5)
                    Text(verbatim: record.dnsName).font(.subheadline.monospaced()).lineLimit(1)
                    Spacer()
                    Text(verbatim: record.recordType).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
                Text(verbatim: line).font(.caption)
                    .foregroundStyle(record.health.needsAttention ? record.health.color : .secondary)
                    .lineLimit(2)
                ForEach(record.reasons, id: \.self) { reason in
                    Text(verbatim: reasonText(reason)).font(.caption).foregroundStyle(record.health.color)
                }
                if let verdict { VerdictRow(entry: verdict) }
            }
        }
    }

    private var line: String {
        [record.targets.joined(separator: ", ").nonEmpty,
         String(localized: "from \(record.sourceLabel)"),
         record.controller.nonEmpty.map { String(localized: "by \($0)") }].compactMap { $0 }.joined(separator: " · ")
    }

    private func reasonText(_ reason: ExternalDnsReason) -> String {
        switch reason {
        case .notReady: String(localized: "no replica ready")
        case .dryRun: String(localized: "dry-run: it changes nothing")
        case .stale: String(localized: "a newer version is not observed yet")
        case .noTarget: String(localized: "no address to point at yet")
        case .unmanaged: String(localized: "outside every controller's domain filters")
        }
    }
}

/// What the last check found for one record, and who owns it when that is not this cluster.
private struct VerdictRow: View {
    let entry: DnsCheckEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                switch entry.verdict {
                case .match:
                    StatusPill(label: String(localized: "Resolves here"), color: .green)
                case .mismatch:
                    StatusPill(label: String(localized: "Points elsewhere"), color: attentionColor)
                case .missing:
                    StatusPill(label: String(localized: "Not found"), color: .red)
                case .unverified:
                    StatusPill(label: String(localized: "Resolves, nothing to compare"), color: .secondary)
                case .error:
                    StatusPill(label: String(localized: "Could not check"), color: .secondary)
                case nil:
                    EmptyView()
                }
                if entry.ownerMatch == false {
                    StatusPill(label: String(localized: "owned by \(entry.owner)"), color: attentionColor)
                }
            }
            if !entry.probed.isEmpty {
                Text(String(localized: "checked as \(entry.probed)"))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            if !entry.answers.isEmpty, entry.verdict != .error {
                Text(verbatim: entry.answers.joined(separator: ", "))
                    .font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            if entry.verdict == .error, !entry.message.isEmpty {
                Text(verbatim: entry.message).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
        }
    }
}
