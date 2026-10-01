import SwiftUI
import TalosdevMobileCore

/// SMART / NVMe health, one card per disk; a node that cannot report it shows why in a
/// secondary info row.
struct DiskHealthSection: View {
    let state: LoadState<NodeDiskHealth>
    let support: FeatureSupport

    var body: some View {
        Section("Disk health") {
            if let notice = support.localizedNotice {
                VersionNoticeRow(text: notice)
            } else {
                SectionStateView(state: state) { report in
                    if !report.supported {
                        VersionNoticeRow(text: FeatureSupport(supported: false, reason: report.reason).localizedNotice
                            ?? String(localized: "This node does not report disk health."))
                    } else if !report.reason.isEmpty {
                        // SMART collection is not configured: the disks are listed without a verdict.
                        VersionNoticeRow(text: report.reason)
                    } else if report.disks.isEmpty {
                        Text("No disk reports its health").font(.footnote).foregroundStyle(.secondary)
                    }
                    ForEach(sortDiskHealth(report.disks)) { DiskHealthCard(disk: $0) }
                }
            }
        }
    }
}

private struct DiskHealthCard: View {
    let disk: DiskHealthInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                VStack(alignment: .leading, spacing: 1) {
                    Text(verbatim: disk.device).font(.headline.monospaced())
                    let identity = [disk.model, disk.serial].filter { !$0.isEmpty }
                    if !identity.isEmpty {
                        Text(verbatim: identity.joined(separator: "  ·  ")).font(.caption).foregroundStyle(.secondary)
                            .lineLimit(1).truncationMode(.middle)
                    }
                }
                Spacer()
                pill
            }
            if !disk.message.isEmpty {
                Text(verbatim: disk.message).font(.caption).foregroundStyle(disk.state == .failing ? Color.red : Color.secondary)
            }
            if let temperature = disk.temperatureC {
                LabeledContent("Temperature", value: formatTemperature(temperature)).font(.caption)
            }
            if let hours = disk.powerOnHours {
                LabeledContent("Power-on time", value: String(localized: "\(hours) h")).font(.caption)
            }
            if let wear = disk.wearPercent {
                let level = wearLevel(wear)
                LabeledContent("Wear") {
                    Text(verbatim: "\(Int(wear.rounded())) %")
                        .foregroundStyle(level == .normal ? Color.secondary : level.color)
                }
                .font(.caption)
                UsageLevelBar(percent: wear)
            }
            ForEach(disk.criticalWarnings, id: \.self) { warning in
                Label {
                    Text(verbatim: warning)
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill")
                }
                .font(.caption)
                .foregroundStyle(.red)
            }
            if !disk.attributes.isEmpty {
                DisclosureGroup {
                    ForEach(disk.attributes.indices, id: \.self) { index in
                        LabeledContent {
                            Text(verbatim: disk.attributes[index].v).font(.caption.monospaced()).textSelection(.enabled)
                        } label: {
                            Text(verbatim: disk.attributes[index].k).font(.caption)
                        }
                    }
                } label: {
                    Text("Attributes (\(disk.attributes.count))").font(.caption)
                }
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder private var pill: some View {
        switch disk.state {
        case .healthy: StatusPill(label: String(localized: "Healthy"), color: .green)
        case .failing: StatusPill(label: String(localized: "Failing"), color: .red)
        case .unknown: StatusPill(label: String(localized: "Unknown"), color: .gray)
        }
    }
}
