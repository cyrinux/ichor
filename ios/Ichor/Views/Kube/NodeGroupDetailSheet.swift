import SwiftUI
import IchorCore

/// The texts of the EKS node group rows and sheet.
enum NodeGroupText {
    /// The group header's error line: the AWS action the role may not call, or AWS' own words.
    static func error(_ report: NodeGroupsReport) -> String {
        guard !report.forbidden.isEmpty else { return report.error }
        return String(format: String(localized: "%@ is not allowed for this role"), report.forbidden)
    }

    /// "50 GiB".
    static func disk(_ gib: Int) -> String { String(localized: "\(gib) GiB") }
}

/// Name, desired between its bounds, Kubernetes version, capacity type and the nodes it runs.
struct NodeGroupRowView: View {
    let row: ManagedNodeGroupRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: row.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                AutoscaleStateBadge(state: row.state)
            }
            Text(
                verbatim: [
                    hpaBoundsText(row.desired, row.min, row.max), row.version, row.capacityType,
                    AutoscaleText.poolNodes(String(row.nodes)),
                ]
                .filter { !$0.isEmpty }
                .joined(separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if let issue = row.issues.first {
                Text(verbatim: issue.message).font(.caption).foregroundStyle(Color.statusBad).lineLimit(2)
            }
        }
    }
}

/// Every field of one managed node group, and AWS' health issues with the resources they name.
struct NodeGroupDetailSheet: View {
    let row: ManagedNodeGroupRow

    var body: some View {
        AutoscalingSheetShell {
            Section {
                AutoscalingSheetHeader(name: row.name, line: nil, state: row.state) {
                    Text(verbatim: hpaBoundsText(row.desired, row.min, row.max)).font(.callout)
                    if row.created > 0 {
                        Text(verbatim: relativeTime(row.created)).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
            Section {
                field("Status", row.status)
                field("Desired size", String(row.desired))
                field("Minimum", String(row.min))
                field("Maximum", String(row.max))
                field("Nodes in the cluster", String(row.nodes))
                field("Kubernetes version", row.version)
                field("AMI release", row.releaseVersion)
                field("AMI type", row.amiType)
                field("Capacity type", row.capacityType)
                field("Instance types", row.instanceTypes.joined(separator: ", "))
                if row.diskSize > 0 {
                    field("Disk", NodeGroupText.disk(row.diskSize))
                }
            }
            Section("Health issues") {
                if row.issues.isEmpty {
                    Text("none").foregroundStyle(.secondary)
                }
                ForEach(row.issues, id: \.self) { issue in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: issue.code).font(.callout.monospaced()).foregroundStyle(Color.statusBad)
                        if !issue.message.isEmpty {
                            Text(verbatim: issue.message).font(.callout)
                        }
                        if !issue.resourceIds.isEmpty {
                            Text(verbatim: issue.resourceIds.joined(separator: ", "))
                                .font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func field(_ label: LocalizedStringKey, _ value: String) -> some View {
        if !value.isEmpty {
            LabeledContent(label) { Text(verbatim: value).monospaced() }
        }
    }
}
