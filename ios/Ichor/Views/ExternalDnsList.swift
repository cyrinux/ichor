import SwiftUI
import IchorCore

/// ExternalDNS: every controller with its settings (provider, policy, owner id, domain filters,
/// sources, interval), then the hostnames it is asked to publish, problems first. A record's row
/// opens its source object's summary. ExternalDNS keeps no status on records: nothing here says
/// one exists at the provider.
struct ExternalDnsList: View {
    let status: ExternalDnsStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
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
                    RecordRow(record: record)
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
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
