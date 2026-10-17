import SwiftUI
import IchorCore

/// The rollback's options, as helm rollback's flags: wait for the workloads (--wait, only when
/// the release has some), replace objects (--force) and keep the last N revisions
/// (--history-max). Replace and keep change the dry run: the sheet reads the plan again.
struct HelmRollbackOptionsSection: View {
    let plan: HelmRollbackPlan
    @Binding var options: HelmRollbackOptions
    /// The plan is being read again for the new options.
    let planning: Bool
    let disabled: Bool

    var body: some View {
        Section {
            if plan.canWait {
                Toggle(isOn: $options.wait) {
                    labelled(Text("Wait until its workloads are ready"),
                             hint: Text("Follows the release's Deployments, StatefulSets and DaemonSets for up to 5 minutes."))
                }
            }
            Toggle(isOn: $options.force) {
                labelled(Text("Replace objects instead of patching them"),
                         hint: Text("Like helm rollback --force: each changed object is written whole, for when a patch cannot bring it back."))
            }
            Stepper(value: $options.historyMax, in: HelmRollbackOptions.historyMaxRange) {
                labelled(Text("Keep the last revisions"), hint: Text("0 keeps them all. Like helm rollback --history-max."),
                         value: Text(verbatim: "\(options.historyMax)").monospacedDigit().bold())
            }
            if let revisions = plan.pruneList {
                Label {
                    Text("Revisions \(revisions) will be deleted")
                } icon: {
                    Image(systemName: "trash")
                }
                .font(.callout)
                .foregroundStyle(Color.statusWarn)
            }
        } header: {
            HStack(spacing: 6) {
                Text("Options")
                if planning {
                    ProgressView().controlSize(.mini)
                }
            }
        }
        .disabled(disabled)
    }

    /// A control's title, its value on the right when it has one, and the hint below.
    private func labelled(_ title: Text, hint: Text, value: Text? = nil) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                title
                if let value {
                    Spacer(minLength: 8)
                    value
                }
            }
            hint.font(.caption).foregroundStyle(.secondary)
        }
    }
}
