import SwiftUI
import IchorCore

/// The texts of the EKS node group and GKE node pool rows and sheet.
enum NodeGroupText {
    /// The group header's error line: the action the role (AWS) or account (Google) may not
    /// call, or the provider's own words.
    static func error(_ report: NodeGroupsReport) -> String {
        guard !report.forbidden.isEmpty else { return report.error }
        if report.gke {
            return String(format: String(localized: "%@ is not allowed for this account"), report.forbidden)
        }
        return String(format: String(localized: "%@ is not allowed for this role"), report.forbidden)
    }

    /// The section title: "Node pools (EKS)" or "Node pools (GKE)".
    static func title(gke: Bool) -> String {
        gke ? String(localized: "Node pools (GKE)") : String(localized: "Node pools (EKS)")
    }

    /// "3 of 1–6", or "Autoscaling off" for a GKE pool without bounds.
    static func size(_ row: ManagedNodeGroupRow) -> String {
        row.autoscalingOff ? String(localized: "Autoscaling off") : hpaBoundsText(row.desired, row.min, row.max)
    }

    /// "50 GiB".
    static func disk(_ gib: Int) -> String { String(localized: "\(gib) GiB") }
}

/// Name, desired between its bounds, Kubernetes version, capacity type and the nodes it runs;
/// a group a minor behind its control plane says so.
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
                    NodeGroupText.size(row), row.version, row.capacityType,
                    AutoscaleText.poolNodes(String(row.nodes)),
                ]
                .filter { !$0.isEmpty }
                .joined(separator: "  ·  ")
            )
            .font(.caption).foregroundStyle(.secondary).lineLimit(1)
            if row.behind {
                Text("behind the control plane").font(.caption2.weight(.semibold)).foregroundStyle(Color.statusWarn)
            }
            if let issue = row.issues.first {
                Text(verbatim: issue.message).font(.caption).foregroundStyle(Color.statusBad).lineLimit(2)
            }
        }
    }
}

/// Every field of one managed node group, and the provider's health issues with the resources
/// they name. A GKE pool says "Image type" and has no size change or upgrade.
struct NodeGroupDetailSheet: View {
    let row: ManagedNodeGroupRow
    /// A GKE node pool: read-only.
    var gke = false
    /// The control plane's Kubernetes version: the upgrade a group behind it is offered.
    var clusterVersion = ""
    /// Reads the report again: after a size change or an upgrade starts and when its update ends.
    var onChanged: () async -> Void = {}

    @Environment(AppModel.self) private var model
    @State private var scaling = false
    @State private var upgrading = false
    /// The update the last size change or upgrade started, as last read; followed while the
    /// sheet is open.
    @State private var update: NodeGroupUpdate?

    var body: some View {
        AutoscalingSheetShell {
            Section {
                AutoscalingSheetHeader(name: row.name, line: nil, state: row.state) {
                    Text(verbatim: NodeGroupText.size(row)).font(.callout)
                    if row.created > 0 {
                        Text(verbatim: relativeTime(row.created)).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
            Section {
                field("Status", row.status)
                field("Desired size", String(row.desired))
                if !row.autoscalingOff {
                    field("Minimum", String(row.min))
                    field("Maximum", String(row.max))
                }
                field("Nodes in the cluster", String(row.nodes))
                field("Kubernetes version", row.version)
                field("AMI release", row.releaseVersion)
                if gke {
                    field("Image type", row.amiType)
                } else {
                    field("AMI type", row.amiType)
                }
                field("Capacity type", row.capacityType)
                field("Instance types", row.instanceTypes.joined(separator: ", "))
                if row.diskSize > 0 {
                    field("Disk", NodeGroupText.disk(row.diskSize))
                }
            }
            if !gke {
                sizeSection
                upgradeSection
            }
            if let update {
                Section {
                    updateRow(update)
                        // On a row, not the Section: a List gives a Section's modifiers to each row.
                        .task(id: update.id) { await follow() }
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

    /// "Change size…"; the update it starts is shown below the upgrade.
    private var sizeSection: some View {
        Section("Size") {
            Button("Change size…") { scaling = true }
                .disabled(!row.canChangeSize || update?.inProgress == true)
            if !row.canChangeSize {
                Text("Only an active or degraded group can change its size: wait for this one to finish.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .sheet(isPresented: $scaling) {
            NodeGroupScaleSheet(row: row) { started in
                update = started
                await onChanged()
            }
        }
    }

    /// "Upgrade…": the control plane's version or the latest node image; nothing while the
    /// group updates, and no node image for a custom AMI.
    private var upgradeSection: some View {
        let choices = row.upgradeChoices(clusterVersion: clusterVersion)
        return Section("Upgrade") {
            Button("Upgrade…") { upgrading = true }
                .disabled(choices.isEmpty || update?.inProgress == true)
            if !row.canChangeSize {
                Text("Only an active or degraded group can be upgraded: wait for this one to finish.")
                    .font(.footnote).foregroundStyle(.secondary)
            } else if row.customAMI {
                Text("This group runs a custom AMI: its image changes through its launch template, not from here.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
        .sheet(isPresented: $upgrading) {
            NodeGroupUpgradeSheet(row: row, choices: choices) { started in
                update = started
                await onChanged()
            }
        }
    }

    @ViewBuilder
    private func updateRow(_ update: NodeGroupUpdate) -> some View {
        if update.inProgress {
            Label(
                update.versionUpdate
                    ? "Update in progress: AWS is replacing the group's nodes."
                    : "Update in progress: AWS is changing the group's size.",
                systemImage: "arrow.triangle.2.circlepath"
            )
            .font(.footnote)
        } else if update.successful {
            Text(
                update.versionUpdate
                    ? "Update finished: the group's nodes have been replaced."
                    : "Update finished: the group has its new size."
            )
            .font(.footnote).foregroundStyle(.secondary)
        } else {
            Text(Self.failureText(update)).font(.footnote).foregroundStyle(Color.statusBad)
        }
    }

    /// Why the update ended: the pod AWS could not evict, named, or AWS' own words.
    private static func failureText(_ update: NodeGroupUpdate) -> String {
        if let eviction = update.podEvictionFailure {
            let named = (eviction.resourceIds + [eviction.message]).filter { !$0.isEmpty }.joined(separator: ": ")
            return String(
                localized:
                    "A pod could not be evicted (\(named)). A disruption budget may block it: Force ignores budgets.")
        }
        let failure = update.failure ?? ""
        return update.unreadable
            ? String(localized: "Could not follow the update: \(failure)")
            : String(localized: "The update did not finish: \(failure)")
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
