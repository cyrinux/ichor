import SwiftUI
import IchorCore

/// What a try screen starts: the draft and how long the node waits before reverting.
struct ConfigTryRequest {
    let base: String
    let draft: String
    let timeoutSeconds: Int
}

/// A machine config tried on a node: the countdown until the node reverts by itself, Keep
/// or Revert now, then how it ended. The try is ConfigTryJob's: leaving the screen keeps it
/// going (a notification takes over), and the node reverts by itself at the deadline.
struct ConfigTryView: View {
    let node: String
    let hostname: String
    /// nil: only follows the try already running on the node.
    let request: ConfigTryRequest?
    /// How the try ended, told once, before the screen is closed.
    let onOutcome: (ConfigTryOutcome) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var started = false
    @State private var refused: String?

    private var job: ConfigTryJob { ConfigTryJob.shared }
    /// The job's try, when it is this node's.
    private var mine: Bool { job.target?.node == node }
    private var progress: ConfigTryProgress? { mine ? job.progress : nil }
    private var outcome: ConfigTryOutcome? { refused.map { .failed($0) } ?? (mine ? job.outcome : nil) }
    private var requested: Bool { mine && !job.waiting }

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
                    Text("You can leave this screen: a notification offers Keep for a while. If Ichor is suspended or the connection is lost, the node reverts by itself.")
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
            .toolbar {
                // The try goes on: the machine config screen shows it again, and so does the notification.
                if outcome == nil {
                    ToolbarItem(placement: .cancellationAction) { Button("Hide") { dismiss() } }
                }
            }
        }
        .task { start() }
        .onChange(of: job.outcome) { _, new in
            if mine, let new { onOutcome(new) }
        }
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
            job.keep()
        } label: {
            Text("Keep").fontWeight(.semibold).frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
        .disabled(requested)
        Button("Revert now", role: .destructive) {
            job.revert()
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
            Text(verbatim: message.isEmpty ? String(localized: "The config could not be applied.") : message)
                .multilineTextAlignment(.center).textSelection(.enabled)
        }
        Button {
            if mine { job.clear() }
            dismiss()
        } label: {
            Text("Done").frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .controlSize(.large)
    }

    /// "4:07"
    private func clock(_ seconds: Int) -> String {
        "\(seconds / 60):" + String(format: "%02d", seconds % 60)
    }

    /// Starts the requested try, unless one runs already (one at a time, across nodes).
    private func start() {
        guard !started else { return }
        started = true
        // The screen stays on while it is shown: the countdown is the point of it.
        UIApplication.shared.isIdleTimerDisabled = true
        guard let request else { return }
        if job.isActive {
            if !mine {
                let other = job.target?.hostname ?? ""
                refused = String(localized: "A config try is already running on \(other): keep or revert it first.")
                onOutcome(.failed(refused ?? ""))
            }
            return
        }
        guard let client = model.client else {
            refused = String(localized: "The config could not be applied.")
            onOutcome(.failed(refused ?? ""))
            return
        }
        if !mine { job.clear() }
        job.start(client: client, node: node, hostname: hostname, base: request.base, draft: request.draft,
                  timeoutSeconds: request.timeoutSeconds)
    }

    /// Leaving keeps the try going in ConfigTryJob; only the screen's idle timer goes back.
    private func leave() {
        restoreIdleTimer()
    }

    private func restoreIdleTimer() {
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
    }
}
