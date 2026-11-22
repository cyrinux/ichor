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
    /// Reads the report again: after a size change starts and when its update ends.
    var onChanged: () async -> Void = {}

    @Environment(AppModel.self) private var model
    @State private var scaling = false
    /// The update the last size change started, as last read; followed while the sheet is open.
    @State private var update: NodeGroupUpdate?

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
            sizeSection
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

    /// "Change size…" and the update it started: in progress, done, or why it failed.
    private var sizeSection: some View {
        Section("Size") {
            Button("Change size…") { scaling = true }
                .disabled(!row.canChangeSize || update?.inProgress == true)
            if !row.canChangeSize {
                Text("Only an active or degraded group can change its size: wait for this one to finish.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if let update {
                updateRow(update)
                    // On a row, not the Section: a List gives a Section's modifiers to each row.
                    .task(id: update.id) { await follow() }
            }
        }
        .sheet(isPresented: $scaling) {
            NodeGroupScaleSheet(row: row) { started in
                update = started
                await onChanged()
            }
        }
    }

    @ViewBuilder
    private func updateRow(_ update: NodeGroupUpdate) -> some View {
        if update.inProgress {
            Label("Update in progress: AWS is changing the group's size.", systemImage: "arrow.triangle.2.circlepath")
                .font(.footnote)
        } else if update.successful {
            Text("Update finished: the group has its new size.").font(.footnote).foregroundStyle(.secondary)
        } else {
            let failure = update.failure ?? ""
            Text(
                update.unreadable
                    ? String(localized: "Could not follow the update: \(failure)")
                    : String(localized: "The update did not finish: \(failure)")
            )
            .font(.footnote).foregroundStyle(Color.statusBad)
        }
    }

    /// Reads the update every 10 s while it is in progress and the sheet is open; its end reads
    /// the report again. A failed read ends it with the reason.
    private func follow() async {
        while let current = update, current.inProgress, let client = model.client {
            try? await Task.sleep(for: .seconds(10))
            if Task.isCancelled { return }
            do {
                update = try await client.nodeGroupUpdate(group: row.name, id: current.id)
            } catch {
                update = current.unreadable(error.localizedDescription)
            }
            if update?.inProgress != true { await onChanged() }
        }
    }

    @ViewBuilder
    private func field(_ label: LocalizedStringKey, _ value: String) -> some View {
        if !value.isEmpty {
            LabeledContent(label) { Text(verbatim: value).monospaced() }
        }
    }
}
