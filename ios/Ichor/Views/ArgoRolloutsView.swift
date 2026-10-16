import SwiftUI
import IchorCore

/// Argo Rollouts' canary and blue-green rollouts (os:admin), by namespace, worst first: strategy,
/// phase, the canary's steps with its weight, and why it is paused; one opens its sheet with
/// promote, promote fully, abort, retry and pause. Polls every 3 s while a rollout moves, and
/// for a while after an action.
struct ArgoRolloutsView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<ArgoRolloutsStatus> = .loading
    /// The rollout whose sheet is open ("namespace/name"): the sheet follows the polled status.
    @State private var selected: String?
    /// Rollouts with an action in flight.
    @State private var running: Set<String> = []
    @State private var resultMessage: String?
    /// Polls until then even before the rollout shows a change: the controller takes a moment.
    @State private var boostUntil = Date.distantPast

    private var status: ArgoRolloutsStatus? {
        if case .loaded(let status, _, _) = state { return status }
        return nil
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { status in
            if status.installed {
                list(status)
            } else {
                ContentUnavailableView("Argo Rollouts is not installed in this cluster.", systemImage: "arrow.triangle.branch")
                    .themedBackground()
            }
        }
        .task { await load() }
        .task { await poll() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .navigationTitle(Text("Argo Rollouts"))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: Binding(get: { selected.flatMap { id in status?.rollouts.first { $0.id == id } } },
                             set: { selected = $0?.id })) { rollout in
            ArgoRolloutSheet(rollout: rollout, busy: running.contains(rollout.id)) { action in
                // Back to the list: the result's alert shows there, over the status it changes.
                selected = nil
                Task { await run(action, on: rollout) }
            }
        }
        .messageAlert($resultMessage)
    }

    private func list(_ status: ArgoRolloutsStatus) -> some View {
        List {
            ForEach(status.byNamespace, id: \.namespace) { group in
                Section {
                    ForEach(group.rollouts) { rollout in
                        Button { selected = rollout.id } label: {
                            ArgoRolloutRow(rollout: rollout, busy: running.contains(rollout.id))
                        }
                    }
                } header: {
                    Text(verbatim: group.namespace)
                }
            }
        }
        .overlay {
            if status.rollouts.isEmpty {
                ContentUnavailableView("No rollout in this cluster.", systemImage: "arrow.triangle.branch")
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.argoRollouts() })
    }

    /// Reads again every 3 s while a rollout moves or an action was just asked.
    private func poll() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(3))
            if Date() < boostUntil || status?.needsPolling == true { await load() }
        }
    }

    private func run(_ action: ArgoRolloutAction, on rollout: ArgoRollout) async {
        guard let client = model.client, !running.contains(rollout.id) else { return }
        running.insert(rollout.id)
        defer { running.remove(rollout.id) }
        do {
            try await client.argoRolloutAction(action, on: rollout)
            resultMessage = String(localized: "\(action.title) requested for \(rollout.id)")
            boostUntil = Date().addingTimeInterval(15)
            await load()
        } catch {
            resultMessage = String(localized: "\(action.title) failed on \(rollout.id): \(error.localizedDescription)")
        }
    }
}

/// Glyph, name, strategy and phase, then the canary's steps with its weight, then why it waits.
private struct ArgoRolloutRow: View {
    let rollout: ArgoRollout
    let busy: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Image(systemName: rollout.glyph).foregroundStyle(rollout.level.color).accessibilityHidden(true)
                Text(verbatim: rollout.name).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                if busy { ProgressView().controlSize(.small) }
                Tag(rollout.strategyLabel)
            }
            HStack(spacing: 6) {
                if !rollout.phase.isEmpty {
                    Text(verbatim: rollout.phase).foregroundStyle(rollout.level.color)
                }
                if rollout.aborted { Tag(String(localized: "Aborted"), color: .statusBad) }
            }
            .font(.caption)
            ArgoRolloutStepsLine(rollout: rollout)
            if let paused = rollout.pausedLine {
                Text(verbatim: paused).font(.caption).foregroundStyle(.statusWarn).lineLimit(2)
            }
        }
        .foregroundStyle(.primary)
    }
}

/// A canary's step bar with "Step 3 of 5" and its weight; nothing for a rollout without steps.
struct ArgoRolloutStepsLine: View {
    let rollout: ArgoRollout

    var body: some View {
        if !rollout.steps.isEmpty {
            let weight = "\(rollout.weight)%"
            VStack(alignment: .leading, spacing: 2) {
                ArgoRolloutStepBar(rollout: rollout)
                HStack {
                    Text("Step \(String(rollout.displayStep)) of \(String(rollout.steps.count))")
                    Spacer()
                    Text("Weight \(weight)")
                }
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
            }
        }
    }
}

/// One segment per canary step: green when done, the current one in the rollout's colour, the
/// track for the rest.
struct ArgoRolloutStepBar: View {
    let rollout: ArgoRollout

    var body: some View {
        HStack(spacing: 3) {
            ForEach(Array(rollout.steps.indices), id: \.self) { i in
                Capsule().fill(color(at: i))
            }
        }
        .frame(height: 8)
        .accessibilityElement()
        .accessibilityValue(Text(verbatim: "\(rollout.displayStep)/\(rollout.steps.count)"))
    }

    private func color(at i: Int) -> Color {
        if i < rollout.step { return .green }
        if i == rollout.step { return rollout.aborted ? .red : .orange }
        return Color(.tertiarySystemFill)
    }
}

extension ArgoRollout {
    var strategyLabel: String {
        isBlueGreen ? String(localized: "Blue-green") : String(localized: "Canary")
    }

    var glyph: String {
        switch level {
        case .critical: "xmark.octagon.fill"
        case .warning: paused ? "pause.circle.fill" : "arrow.triangle.2.circlepath"
        case .ok: "checkmark.circle.fill"
        case .idle, .unknown: "circle.dashed"
        }
    }

    /// "Paused 12 minutes ago · CanaryPauseStep", or "Waits for a promotion" for a blue-green
    /// holding its new version; nil when not paused.
    var pausedLine: String? {
        guard paused else { return nil }
        if awaitsPromotion && pauseReasons.isEmpty { return String(localized: "Waits for a promotion") }
        let since = pausedSince > 0 ? String(localized: "Paused \(relativeTime(pausedSince))") : String(localized: "Paused")
        return ([since] + pauseReasons).joined(separator: " · ")
    }
}

extension ArgoRolloutAction {
    var title: String {
        switch self {
        case .promote: String(localized: "Promote")
        case .promoteFull: String(localized: "Promote fully")
        case .abort: String(localized: "Abort")
        case .retry: String(localized: "Retry")
        case .pause: String(localized: "Pause")
        }
    }

    /// The confirmation's title for the rollout `name`.
    func question(_ name: String) -> String {
        switch self {
        case .promote: String(localized: "Promote \(name)?")
        case .promoteFull: String(localized: "Promote \(name) fully?")
        case .abort: String(localized: "Abort \(name)?")
        case .retry: String(localized: "Retry \(name)?")
        case .pause: String(localized: "Pause \(name)?")
        }
    }

    /// What the action does, under the confirmation's title.
    var explanation: String {
        switch self {
        case .promote: String(localized: "The rollout moves on to its next step; a blue-green switches the traffic to the new version.")
        case .promoteFull: String(localized: "The remaining steps and analyses are skipped: the new version takes all the traffic now.")
        case .abort: String(localized: "The stable version takes the traffic back and the new pods are scaled down. Retry starts the update again.")
        case .retry: String(localized: "The aborted update starts again from its first step.")
        case .pause: String(localized: "The rollout stays at its current step until it is promoted.")
        }
    }
}
