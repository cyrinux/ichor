import SwiftUI
import IchorCore

/// Phase timeline of the running maintenance, the drained pods with their state, then the
/// result.
struct MaintenanceRunView: View {
    let job: MaintenanceJob
    let hostname: String
    let onClose: () -> Void

    @State private var confirmStop = false

    var body: some View {
        List {
            if job.isActive {
                Section {
                    Label {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("Keep Ichor open until the maintenance ends").fontWeight(.semibold)
                            Text("Ichor drives every step: in the background iOS pauses it, and the maintenance stalls.")
                                .font(.footnote).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "iphone").foregroundStyle(.orange)
                    }
                }
            }
            if let target = job.target {
                Section {
                    LabeledContent("After the drain", value: target.action.localizedLabel)
                }
            }
            Section("Progress") {
                ForEach(job.timeline) { step in MaintenanceStepRow(step: step) }
            }
            if !job.pods.isEmpty {
                Section("Pods") {
                    ForEach(job.pods) { DrainPodRow(pod: $0) }
                }
            }
            resultSection
        }
        .themedBackground()
        .alert(Text("Stop the maintenance?"), isPresented: $confirmStop) {
            Button("Stop", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The run stops after the current step. \(hostname) stays cordoned: uncordon it from its menu when ready.")
        }
    }

    @ViewBuilder private var resultSection: some View {
        Section {
            switch job.outcome {
            case .succeeded?:
                Label {
                    Text(verbatim: successMessage)
                } icon: {
                    Image(systemName: "checkmark.seal.fill").foregroundStyle(.statusOK)
                }
            case .failed(let error)?:
                Label { Text(verbatim: error) } icon: { Image(systemName: "xmark.octagon.fill").foregroundStyle(.statusBad) }
            case nil:
                EmptyView()
            }
            if job.isActive {
                Button(job.stopping ? String(localized: "Stopping…") : String(localized: "Stop"), role: .destructive) {
                    confirmStop = true
                }
                .disabled(job.stopping)
            } else {
                Button("Close") {
                    job.clear()
                    onClose()
                }
            }
        } footer: {
            if job.isActive {
                Text("Stopping leaves the node cordoned.")
            }
        }
    }

    private var successMessage: String {
        switch job.target?.action ?? .reboot {
        case .reboot where job.target?.wasCordoned == true:
            String(localized: "\(hostname) is back and stays cordoned, as it was before the maintenance")
        case .reboot: String(localized: "\(hostname) is back and uncordoned")
        case .shutdown: String(localized: "\(hostname) is shutting down and stays cordoned")
        case .none: String(localized: "\(hostname) is drained and stays cordoned")
        }
    }
}

private struct MaintenanceStepRow: View {
    let step: MaintenanceStep

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            icon.frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(step.phase.localizedLabel)
                        .fontWeight(step.state == .current ? .semibold : .regular)
                        .foregroundStyle(step.state == .pending ? Color.secondary : Color.primary)
                    Spacer()
                    if step.at > 0 {
                        Text(Date(epochMillis: step.at), format: .dateTime.hour().minute().second())
                            .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }
                if !step.message.isEmpty {
                    Text(verbatim: step.message).font(.caption)
                        .foregroundStyle(step.state == .failed ? Color.statusBad : Color.secondary)
                }
            }
        }
    }

    @ViewBuilder private var icon: some View {
        switch step.state {
        case .done: Image(systemName: "checkmark.circle.fill").foregroundStyle(.statusOK)
        case .current: ProgressView()
        case .failed: Image(systemName: "xmark.circle.fill").foregroundStyle(.statusBad)
        case .pending: Image(systemName: "circle").foregroundStyle(.secondary)
        }
    }
}

extension MaintenancePhase {
    var localizedLabel: String {
        switch self {
        case .cordon: String(localized: "Cordon")
        case .drain: String(localized: "Drain")
        case .reboot: String(localized: "Reboot")
        case .shutdown: String(localized: "Shut down")
        case .waiting: String(localized: "Waiting for node")
        case .uncordon: String(localized: "Uncordon")
        }
    }
}
