import IchorCore
import SwiftUI

/// The followed image run (a Hetzner snapshot, an AWS AMI or an Azure image): its steps with the
/// command's last line, Stop, then the result.
struct ImageRunSections: View {
    /// Called when the result is closed, with how the run ended.
    let onClose: (JoinJob.Outcome?) -> Void

    @State private var confirmStop = false
    private var job: JoinJob { .shared }
    private var kind: ImageRunKind { job.request?.imageKind ?? .hetznerSnapshot }

    // Explicit: the private @State makes the memberwise init private.
    init(onClose: @escaping (JoinJob.Outcome?) -> Void) {
        self.onClose = onClose
    }

    var body: some View {
        Section {
            ForEach(job.timeline) { step in
                RunStepRow(
                    label: step.phase.localizedLabel(image: kind), state: step.state, at: step.at,
                    message: step.message)
            }
        } header: {
            // Literals in each branch: a ternary would make a String, shown unlocalized.
            switch kind {
            case .hetznerSnapshot: Text("Make a Talos snapshot")
            case .awsAMI: Text("Make a Talos AMI")
            case .azureImage: Text("Make a Talos image")
            }
        }
        Section {
            switch job.outcome {
            case .snapshotMade(let made)?:
                Text("Snapshot \(made.description) is ready: pick it to create the server.")
                closeButton()
            case .imageMade(let made)?:
                Text(verbatim: kind.madeText(made))
                closeButton()
            case .failed(let error)?:
                Text(verbatim: error).foregroundStyle(.statusBad)
                closeButton()
            case .joined?, .appearedInOmni?:
                closeButton()
            case nil:
                Button(
                    job.stopping ? String(localized: "Stopping…") : String(localized: "Stop and clean up"),
                    role: .destructive
                ) {
                    confirmStop = true
                }
                .disabled(job.stopping)
            }
        } footer: {
            if job.isActive { stopHint }
        }
        .alert(Text("Stop and clean up"), isPresented: $confirmStop) {
            Button("Stop and clean up", role: .destructive) { job.requestStop() }
            Button("Cancel", role: .cancel) {}
        } message: {
            stopBody
        }
    }

    @ViewBuilder private var stopHint: some View {
        switch kind {
        case .hetznerSnapshot: Text("Stopping deletes the throwaway server and its key.")
        case .awsAMI: Text("Stopping terminates the throwaway instance.")
        case .azureImage: Text("Stopping deletes the helper VM and its data disk.")
        }
    }

    @ViewBuilder private var stopBody: some View {
        switch kind {
        case .hetznerSnapshot:
            Text("The run stops. The throwaway server, its key and an unfinished snapshot are deleted.")
        case .awsAMI:
            Text("The run stops. The throwaway instance is terminated, and an unfinished snapshot or AMI is deleted.")
        case .azureImage:
            Text(
                "The run stops. The helper VM, billed until it is deleted, is deleted with its data disk, and an unfinished image too."
            )
        }
    }

    private func closeButton() -> some View {
        Button("Close") {
            let outcome = job.outcome
            job.clear()
            onClose(outcome)
        }
    }
}

extension ImageRunKind {
    /// "AMI … is ready", or the Azure image's line, for what the run made.
    func madeText(_ made: MadeImage) -> String {
        switch self {
        case .azureImage: String(localized: "Image \(made.name) is ready: it is picked for the new node.")
        case .awsAMI, .hetznerSnapshot: String(localized: "AMI \(made.name) is ready: it is picked for the new node.")
        }
    }

    /// The Live Activity's title.
    var activityTitle: String {
        switch self {
        case .hetznerSnapshot: String(localized: "Making a Talos snapshot")
        case .awsAMI: String(localized: "Making a Talos AMI")
        case .azureImage: String(localized: "Making a Talos image")
        }
    }
}

extension JoinPhase {
    /// An image run's labels where a helper instance or VM, not a server with a key, does the work.
    func localizedLabel(image: ImageRunKind?) -> String {
        switch (image, self) {
        case (.awsAMI?, .creating): String(localized: "Start the throwaway instance")
        case (.awsAMI?, .cleanup): String(localized: "Terminate the throwaway instance")
        case (.azureImage?, .creating): String(localized: "Start the helper VM")
        case (.azureImage?, .snapshot): String(localized: "Make the managed image")
        case (.azureImage?, .cleanup): String(localized: "Delete the helper VM and its data disk")
        default: localizedLabel
        }
    }
}
