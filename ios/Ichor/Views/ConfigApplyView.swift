import SwiftUI
import IchorCore

/// A machine config applied for good: its phase (applying, then the reboot and the wait in
/// reboot mode), then how it ended. Leaving stops following: what was sent stays applied.
struct ConfigApplyView: View {
    let node: String
    let hostname: String
    let base: String
    let draft: String
    let mode: ConfigApplyMode
    /// Whether it failed, told once, before the screen is closed.
    let onOutcome: (_ failed: Bool) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var progress: ConfigApplyProgress?
    /// nil while running; then nil inside on success.
    @State private var outcome: String??
    @State private var task: Task<Void, Never>?

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                Spacer()
                if let outcome {
                    result(outcome)
                } else {
                    ProgressView().controlSize(.large)
                    Text(verbatim: phaseLabel).note()
                    if let message = progress?.message, !message.isEmpty {
                        Text(verbatim: message).font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
                    }
                }
                Spacer()
            }
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .themedBackground()
            .navigationTitle(modeTitle(mode))
            .navigationBarTitleDisplayMode(.inline)
        }
        .task { await run() }
        .onDisappear { if outcome == nil { task?.cancel() } }
    }

    private var phaseLabel: String {
        switch progress?.applyPhase {
        case .rebooting?: String(localized: "The node is rebooting…")
        case .waiting?: String(localized: "Waiting for the node to come back…")
        default: String(localized: "Applying the change…")
        }
    }

    @ViewBuilder private func result(_ error: String?) -> some View {
        if let error {
            Image(systemName: "xmark.octagon.fill").font(.system(size: 56)).foregroundStyle(.statusBad)
            Text(verbatim: error.isEmpty ? String(localized: "The config could not be applied.") : error)
                .multilineTextAlignment(.center).textSelection(.enabled)
        } else {
            Image(systemName: "checkmark.seal.fill").font(.system(size: 56)).foregroundStyle(.statusOK)
            Text(doneText).font(.headline).multilineTextAlignment(.center)
        }
        Button { dismiss() } label: {
            Text("Done").frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
    }

    private var doneText: String {
        switch mode {
        case .auto: String(localized: "The change is applied.")
        case .staged: String(localized: "The change is staged: it takes effect at the next reboot.")
        case .reboot: String(localized: "The node is back with the new config.")
        }
    }

    private func run() async {
        guard task == nil else { return }
        guard let client = model.client else {
            finish(String(localized: "The config could not be applied."))
            return
        }
        UIApplication.shared.isIdleTimerDisabled = true
        let events = client.applyMachineConfig(node: node, base: base, draft: draft, mode: mode)
        let running = Task {
            for await event in events {
                switch event {
                case .progress(let update): progress = update
                case .done(let error): finish(error)
                }
            }
        }
        task = running
        await running.value
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
    }

    private func finish(_ error: String?) {
        outcome = .some(error)
        onOutcome(error != nil)
        announce(error ?? doneText)
    }
}

/// The button label of a mode, also the apply screen's title.
func modeTitle(_ mode: ConfigApplyMode) -> String {
    switch mode {
    case .auto: String(localized: "Apply now")
    case .staged: String(localized: "Apply at the next reboot")
    case .reboot: String(localized: "Apply and reboot now")
    }
}
