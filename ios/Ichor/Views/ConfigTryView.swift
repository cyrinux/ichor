import SwiftUI
import IchorCore

/// A machine config tried on a node: the countdown until the node reverts by itself, Keep
/// or Revert now, then how it ended. Leaving stops following, and the node reverts.
struct ConfigTryView: View {
    let node: String
    let hostname: String
    let base: String
    let draft: String
    let timeoutSeconds: Int

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var handle: ConfigTryHandle?
    @State private var progress: ConfigTryProgress?
    @State private var outcome: ConfigTryOutcome?
    /// Keep or Revert now was tapped: no second tap until the node answers.
    @State private var requested = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                Spacer()
                if let outcome {
                    result(outcome)
                } else if let progress, progress.phase == .trying {
                    countdown(progress)
                } else {
                    working
                }
                Spacer()
                if outcome == nil {
                    Text("If you leave or lose the connection, the node reverts by itself.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
            }
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .themedBackground()
            .navigationTitle(String(localized: "Try · \(hostname)"))
            .navigationBarTitleDisplayMode(.inline)
        }
        .task { await run() }
        .onDisappear(perform: leave)
    }

    @ViewBuilder private var working: some View {
        ProgressView().controlSize(.large)
        Text(verbatim: phaseLabel).note()
        warning
    }

    private var phaseLabel: String {
        switch progress?.phase {
        case .keeping?: String(localized: "Keeping the change…")
        case .reverting?: String(localized: "Going back to the previous config…")
        default: String(localized: "Applying the change…")
        }
    }

    /// Why keeping or reverting failed while the try goes on.
    @ViewBuilder private var warning: some View {
        if let message = progress?.message, !message.isEmpty {
            Label { Text(verbatim: message) } icon: { Image(systemName: "exclamationmark.triangle.fill") }
                .font(.footnote)
                .foregroundStyle(.statusWarn)
        }
    }

    @ViewBuilder private func countdown(_ progress: ConfigTryProgress) -> some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            let left = progress.deadlineDate.map { clock(configSecondsLeft(deadline: $0, now: context.date)) } ?? "—"
            VStack(spacing: 8) {
                Text(verbatim: left)
                    .font(.system(size: 64, weight: .semibold, design: .rounded).monospacedDigit())
                    .accessibilityHidden(true)
                Text("Reverts automatically in \(left)").font(.headline)
            }
        }
        warning
        Button {
            requested = true
            handle?.keep()
        } label: {
            Text("Keep").fontWeight(.semibold).frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        .disabled(requested)
        Button("Revert now", role: .destructive) {
            requested = true
            handle?.revert()
        }
        .disabled(requested)
    }

    @ViewBuilder private func result(_ outcome: ConfigTryOutcome) -> some View {
        switch outcome {
        case .kept:
            Image(systemName: "checkmark.seal.fill").font(.system(size: 56)).foregroundStyle(.statusOK)
            Text("The change is now permanent.").font(.headline).multilineTextAlignment(.center)
        case .reverted:
            Image(systemName: "arrow.uturn.backward.circle.fill").font(.system(size: 56)).foregroundStyle(.secondary)
            Text("The node is back on its previous config.").font(.headline).multilineTextAlignment(.center)
        case .failed(let message):
            Image(systemName: "xmark.octagon.fill").font(.system(size: 56)).foregroundStyle(.statusBad)
            Text(verbatim: message).multilineTextAlignment(.center).textSelection(.enabled)
        }
        Button { dismiss() } label: {
            Text("Done").frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
    }

    /// "4:07"
    private func clock(_ seconds: Int) -> String {
        "\(seconds / 60):" + String(format: "%02d", seconds % 60)
    }

    private func run() async {
        guard handle == nil else { return }
        guard let client = model.client else {
            outcome = .failed(String(localized: "The config could not be applied."))
            return
        }
        let run = client.tryMachineConfig(node: node, base: base, draft: draft, timeoutSeconds: timeoutSeconds)
        handle = run
        // The screen stays on: a paused app loses the connection, and the node reverts.
        UIApplication.shared.isIdleTimerDisabled = true
        for await event in run.events {
            switch event {
            case .progress(let update):
                progress = update
                requested = false
            case .done(let result):
                outcome = result
                announce(resultAnnouncement(result))
            }
        }
        restoreIdleTimer()
    }

    private func resultAnnouncement(_ outcome: ConfigTryOutcome) -> String {
        switch outcome {
        case .kept: String(localized: "The change is now permanent.")
        case .reverted: String(localized: "The node is back on its previous config.")
        case .failed(let message): message
        }
    }

    /// Leaving while the try runs stops following it; the node reverts at the end of its timeout.
    private func leave() {
        if outcome == nil { handle?.cancel() }
        restoreIdleTimer()
    }

    private func restoreIdleTimer() {
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
    }
}
