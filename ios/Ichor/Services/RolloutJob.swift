import Foundation
import IchorCore

/// The rollout restart the phone started, followed app-wide until every new pod is ready (one
/// at a time; a new restart replaces the last): its Live Activity shows "3/5 ready" after the
/// user left the screen. The rollout watch (StartKubeRolloutWatch) feeds it; the rollout runs
/// in the cluster, so the app being suspended only stops following.
@Observable
@MainActor
final class RolloutJob {
    static let shared = RolloutJob()

    /// How long a restart is followed at most.
    private static let limit: Duration = .seconds(10 * 60)

    private(set) var workload: KubeWorkload?
    private(set) var status: KubeRolloutStatus?
    /// nil while following.
    private(set) var outcome: FollowOutcome?
    private var task: Task<Void, Never>?
    /// Bumped by each start and stop: a replaced follow's updates are dropped.
    private var generation = 0
    private let background = OperationBackgroundTask()

    var isRunning: Bool { task != nil }

    /// Follows the rollout of `workload`, just restarted.
    func start(client: TalosClient, workload: KubeWorkload) {
        task?.cancel()
        generation += 1
        let current = generation
        self.workload = workload
        status = nil
        outcome = nil
        OperationNotice.prepare()
        background.begin("Rollout restart")
        task = Task {
            let result = await follow(client: client, workload: workload, generation: current)
            guard current == generation else { return }
            finish(result)
        }
    }

    /// Stops following; the rollout goes on in the cluster.
    func stopFollowing() {
        guard isRunning else { return }
        task?.cancel()
        generation += 1
        finish(.unfollowed)
    }

    /// Forgets a finished restart.
    func clear() {
        guard !isRunning else { return }
        workload = nil
        status = nil
        outcome = nil
    }

    /// Watches until the rollout ends, the watch fails twice in a row, or the limit passes.
    private func follow(client: TalosClient, workload: KubeWorkload, generation current: Int) async -> FollowOutcome {
        let deadline = ContinuousClock.now + Self.limit
        var failures = 0
        while !Task.isCancelled, ContinuousClock.now < deadline {
            var lastError: String?
            for await event in client.rolloutWatch(workload) {
                switch event {
                case .update(let latest):
                    guard current == generation else { return .unfollowed }
                    status = latest
                    failures = 0
                    if latest.manual {
                        return .failed(String(localized: "Its pods are replaced only when deleted: the restart replaces none."))
                    }
                    if let ended = FollowOutcome.rollout(latest) { return ended }
                case .done(let error):
                    lastError = error
                }
                if Task.isCancelled || ContinuousClock.now >= deadline { break }
            }
            if Task.isCancelled { return .unfollowed }
            if let lastError {
                failures += 1
                if failures >= 2 { return .failed(lastError) }
            }
            // The watch ended (the network): followed again after 2 s, as the rollout sheet does.
            try? await Task.sleep(for: .seconds(2))
        }
        return Task.isCancelled ? .unfollowed : .stillRunning
    }

    private func finish(_ result: FollowOutcome) {
        guard outcome == nil else { return }
        outcome = result
        task = nil
        background.end()
        guard let workload, result != .unfollowed else { return }
        OperationNotice.post(.rollout, title: Self.title(workload), body: Self.result(self))
    }
}

/// How the followed restart is worded, in its Live Activity and its notification.
extension RolloutJob {
    static func title(_ workload: KubeWorkload) -> String {
        String(localized: "Restarting \(workload.name)")
    }

    /// "3/5 ready", or the start of the rollout.
    static func step(_ job: RolloutJob) -> String {
        guard let count = job.status?.restartCount else { return String(localized: "Rolling out…") }
        return String(localized: "\(count.done)/\(count.total) ready")
    }

    static func result(_ job: RolloutJob) -> String {
        switch job.outcome {
        case .succeeded?: String(localized: "Every pod was replaced and is ready.")
        case .failed(let message)?:
            message.nonEmpty ?? String(localized: "The rollout exceeded its progress deadline.")
        case .stillRunning?: String(localized: "Still rolling out after 10 minutes: check it again later.")
        case .unfollowed?, .terminated?, nil: String(localized: "Stopped following. The rollout goes on.")
        }
    }
}
