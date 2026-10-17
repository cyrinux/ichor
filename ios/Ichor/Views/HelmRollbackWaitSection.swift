import SwiftUI
import IchorCore

/// After a rollback with Wait on (helm rollback --wait): each Deployment, StatefulSet and
/// DaemonSet of the release with its ready pods, then how the wait ended. Stop following
/// leaves the sheet; the rollback stays done either way.
struct HelmRollbackWaitSection: View {
    /// nil until the first read.
    let progress: HelmRollbackFollowProgress?
    /// nil while it follows.
    let outcome: FollowOutcome?
    let leave: () -> Void

    var body: some View {
        Section {
            if let progress {
                if progress.total > 0 {
                    ProgressView(value: Double(progress.done), total: Double(progress.total))
                        .accessibilityValue(Text(verbatim: "\(progress.done)/\(progress.total)"))
                }
                ForEach(progress.workloads) { HelmRollbackWorkloadRow(workload: $0) }
            } else if outcome == nil {
                HStack {
                    Spacer()
                    ProgressView()
                    Spacer()
                }
            }
        } header: {
            Text("Waiting for the workloads")
        }
        if let outcome, let text = outcomeText(outcome) {
            Section {
                text.font(.callout).foregroundStyle(outcomeColor(outcome))
            }
        }
        Section {
            Button(action: leave) {
                Group {
                    if outcome == nil {
                        Text("Stop following")
                    } else {
                        Text("Done").bold()
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets())
        }
    }

    private func outcomeText(_ outcome: FollowOutcome) -> Text? {
        switch outcome {
        case .succeeded:
            return Text("All workloads are ready")
        case .failed(let message):
            if let workload = progress?.failedWorkload {
                return Text("\(workload.label) did not become ready")
            }
            return message.isEmpty ? nil : Text(verbatim: message)
        case .stillRunning:
            return Text("Still rolling out after 5 minutes: check the workloads")
        case .terminated, .unfollowed:
            return nil
        }
    }

    private func outcomeColor(_ outcome: FollowOutcome) -> Color {
        switch outcome {
        case .succeeded: Color.statusOK
        case .failed: Color.statusBad
        default: Color.statusWarn
        }
    }
}

/// A workload of the wait: which one, ready of desired, and its bar.
private struct HelmRollbackWorkloadRow: View {
    let workload: HelmRollbackWorkload

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: workload.label)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 8)
                Text(verbatim: "\(workload.ready)/\(workload.desired)")
                    .font(.callout)
                    .monospacedDigit()
                    .foregroundStyle(countColor)
            }
            ProgressView(value: workload.fraction)
                .tint(countColor)
            if workload.manual {
                Text("Pods are replaced only when deleted").font(.caption).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var countColor: Color {
        if workload.failed { return Color.statusBad }
        if workload.done || workload.manual { return Color.statusOK }
        return Color.statusWarn
    }
}
