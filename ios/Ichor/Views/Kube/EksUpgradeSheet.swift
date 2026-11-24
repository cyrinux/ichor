import SwiftUI
import IchorCore

/// The texts of the control plane row and sheet.
enum EksUpgradeText {
    /// The row's line: the version and what comes next, or the update running.
    static func line(_ plan: EksUpgradePlan, update: NodeGroupUpdate?) -> String {
        switch plan.state(update: update) {
        case .running:
            let status = (update ?? plan.updating)?.status ?? ""
            return String(
                localized: "Kubernetes \(plan.version) · upgrade: \(status)"
            )
        case .available:
            return String(
                localized: "Kubernetes \(plan.version) · upgrade to \(plan.next) available"
            )
        case .unchecked:
            return String(
                localized: "Kubernetes \(plan.version) · \(plan.next) next (availability not checked)"
            )
        case .upToDate:
            return String(
                localized: "Kubernetes \(plan.version) · up to date"
            )
        case .unknown:
            return error(plan)
        }
    }

    /// The plan's error line: the AWS action the role may not call, or AWS' own words.
    static func error(_ plan: EksUpgradePlan) -> String {
        guard !plan.forbidden.isEmpty else { return plan.error }
        return String(format: String(localized: "%@ is not allowed for this role"), plan.forbidden)
    }

    /// The next version: up to date, the version, or the version not checked with AWS.
    static func next(_ plan: EksUpgradePlan) -> String {
        if plan.next.isEmpty { return plan.version.isEmpty ? "" : String(localized: "up to date") }
        if plan.nextKnown { return plan.next }
        return String(
            localized: "\(plan.next) (availability not checked)"
        )
    }

    static let irreversible = String(
        localized: "This takes 20 to 40 minutes and cannot be undone or cancelled."
    )
}

/// "Control plane" at the top of the EKS group: Kubernetes version, the next minor or the
/// update running.
struct EksControlPlaneRowView: View {
    let plan: EksUpgradePlan

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text("Control plane").font(.callout).foregroundStyle(.primary)
                Spacer()
                switch plan.state(update: nil) {
                case .running:
                    AutoscaleChip(text: String(localized: "upgrading"), color: .statusWarn)
                case .available, .unchecked:
                    AutoscaleChip(text: String(localized: "upgrade available"), color: .statusWarn)
                case .upToDate, .unknown:
                    EmptyView()
                }
            }
            Text(verbatim: EksUpgradeText.line(plan, update: nil))
                .font(.caption).foregroundStyle(.secondary).lineLimit(2)
        }
    }
}

/// The plan before anything starts: current and next version, end of standard support, the
/// blockers, what the upgrade cannot undo, the node groups to upgrade afterwards; then
/// "Upgrade…" with the cluster's name typed. The update is followed every 30 s while open,
/// from the plan's when the app was reopened. Never retried.
struct EksUpgradeSheet: View {
    let plan: EksUpgradePlan
    /// Reads the report again: after the upgrade starts and when its update ends.
    var onChanged: () async -> Void = {}

    @Environment(AppModel.self) private var model
    @State private var confirming = false
    @State private var applying = false
    @State private var failure: String?
    /// The update this sheet started or followed, as last read.
    @State private var started: NodeGroupUpdate?

    private var update: NodeGroupUpdate? { started ?? plan.updating }

    var body: some View {
        AutoscalingSheetShell {
            Section {
                AutoscalingSheetHeader(
                    name: plan.cluster, line: String(localized: "Control plane"), badge: { EmptyView() }
                ) {
                    if !plan.error.isEmpty {
                        Text(verbatim: EksUpgradeText.error(plan)).font(.footnote).foregroundStyle(Color.statusBad)
                    }
                }
            }
            Section {
                field("Kubernetes version", plan.version)
                field("Status", plan.status)
                field("Platform version", plan.platformVersion)
                field("Next version", EksUpgradeText.next(plan))
                if plan.supportEnds > 0 {
                    field("End of standard support", supportEnds)
                }
            }
            Section("Blockers") {
                if plan.blockers.isEmpty {
                    Text("none").foregroundStyle(.secondary)
                }
                ForEach(plan.blockers, id: \.self) { blocker in
                    Text(verbatim: blocker).foregroundStyle(Color.statusBad)
                }
            }
            if !plan.next.isEmpty {
                upgradeSection
            }
            if let update {
                Section {
                    updateRow(update)
                        // On a row, not the Section: a List gives a Section's modifiers to each row.
                        .task(id: update.id) { await follow() }
                }
            }
        }
        .sheet(isPresented: $confirming) { confirmation }
    }

    private var supportEnds: String {
        Date(timeIntervalSince1970: TimeInterval(plan.supportEnds) / 1000).formatted(date: .abbreviated, time: .omitted)
    }

    private var upgradeSection: some View {
        let groups = plan.groups.joined(separator: ", ")
        return Section("Upgrade") {
            Text(verbatim: EksUpgradeText.irreversible)
            if !plan.groups.isEmpty {
                Text(
                    String(
                        localized: "Then upgrade each node group to \(plan.next) from its sheet: \(groups)."
                    )
                )
                .font(.footnote).foregroundStyle(.secondary)
            }
            if let failure {
                Label {
                    Text(verbatim: failure).textSelection(.enabled)
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill")
                }
                .foregroundStyle(.statusBad)
            }
            Button {
                confirming = true
            } label: {
                HStack {
                    if applying { ProgressView() }
                    Text(applying ? String(localized: "Upgrading \(plan.cluster)") : String(localized: "Upgrade…"))
                }
            }
            .disabled(!plan.canUpgrade || applying || update?.inProgress == true)
        }
    }

    private var confirmation: some View {
        let summary = String(
            localized: "The control plane goes from Kubernetes \(plan.version) to \(plan.next)."
        )
        return HostnameConfirmationSheet(
            title: String(
                localized: "Upgrade \(plan.cluster) to Kubernetes \(plan.next)?"
            ),
            message: [summary, EksUpgradeText.irreversible].joined(separator: "\n\n"),
            hostname: plan.cluster,
            actionTitle: String(localized: "Upgrade")
        ) {
            confirming = false
            Task { await apply() }
        }
    }

    @ViewBuilder
    private func updateRow(_ update: NodeGroupUpdate) -> some View {
        if update.inProgress {
            Label(
                "Upgrade in progress: AWS is upgrading the control plane. The app can be closed.",
                systemImage: "arrow.triangle.2.circlepath"
            )
            .font(.footnote)
        } else if update.successful {
            Text("Upgrade finished: the control plane runs the new version.")
                .font(.footnote).foregroundStyle(.secondary)
        } else {
            Text(String(localized: "The update did not finish: \(update.failure ?? "")"))
                .font(.footnote).foregroundStyle(Color.statusBad)
        }
    }

    private func apply() async {
        guard let client = model.client else { return }
        applying = true
        failure = nil
        do {
            started = try await client.eksUpgrade(version: plan.next)
            await onChanged()
        } catch {
            failure = error.localizedDescription
        }
        applying = false
    }

    /// Reads the update every 30 s while it is in progress and the sheet is open; its end reads
    /// the report again. A failed read is a moment the API is unreachable during the upgrade: it
    /// stays in progress and is read again.
    private func follow() async {
        while let current = update, current.inProgress, let client = model.client {
            try? await Task.sleep(for: .seconds(30))
            if Task.isCancelled { return }
            guard let read = try? await client.eksUpdate(id: current.id) else { continue }
            started = read
            if !read.inProgress { await onChanged() }
        }
    }

    @ViewBuilder
    private func field(_ label: LocalizedStringKey, _ value: String) -> some View {
        if !value.isEmpty {
            LabeledContent(label) { Text(verbatim: value).monospaced() }
        }
    }
}
