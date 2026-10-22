import Foundation
import IchorCore

/// The Kubernetes-side runs' Live Activities: an Argo CD sync ("wave 2/4 · 12/30 resources",
/// Terminate), a Flux reconcile and a rollout restart (Stop following), a network test (Stop).
extension OperationActivities {
    static func argoSync() -> OperationContent? {
        guard let run = GitOpsOperationJob.shared.argo else { return nil }
        let title = GitOpsOperationJob.argoTitle(run)
        if let outcome = run.outcome {
            return OperationContent(title: title, step: GitOpsOperationJob.argoResult(run), phase: outcome.phase)
        }
        let p = run.progress
        let count = p.flatMap { operationCount(index: $0.done, total: $0.total) }
        return OperationContent(title: title, step: GitOpsOperationJob.argoStep(run), done: count?.done, total: count?.total,
                                buttons: OperationButton.offered(for: .argoSync, stopping: run.terminating))
    }

    static func fluxReconcile() -> OperationContent? {
        guard let run = GitOpsOperationJob.shared.flux else { return nil }
        let title = GitOpsOperationJob.fluxTitle(run)
        if let outcome = run.outcome {
            return OperationContent(title: title, step: GitOpsOperationJob.fluxResult(run), phase: outcome.phase)
        }
        return OperationContent(title: title, step: GitOpsOperationJob.fluxStep(run), buttons: OperationButton.offered(for: .fluxReconcile))
    }

    static func rollout() -> OperationContent? {
        let job = RolloutJob.shared
        guard let workload = job.workload else { return nil }
        let title = RolloutJob.title(workload)
        if let outcome = job.outcome {
            return OperationContent(title: title, step: RolloutJob.result(job), phase: outcome.phase)
        }
        let count = job.status?.restartCount
        return OperationContent(title: title, step: RolloutJob.step(job), done: count?.done, total: count?.total,
                                buttons: OperationButton.offered(for: .rollout))
    }

    static func netPerf() -> OperationContent? {
        guard let run = NetPerfJob.shared.run else { return nil }
        let title = String(localized: "Network test")
        if !run.running {
            let phase: OperationPhase = run.stopped ? .stopped : run.error == nil ? .succeeded : .failed
            return OperationContent(title: title, step: NetPerfJob.result(run), phase: phase)
        }
        // The measurements done, of those the setup runs.
        let count = operationCount(index: run.results.count, total: run.setup.steps)
        return OperationContent(title: title, step: NetPerfJob.step(run), done: count?.done, total: count?.total,
                                buttons: OperationButton.offered(for: .netPerf, stopping: run.stopping))
    }

    /// A runbook: "Step 2 of 3 · scale-down", "Waiting for you" while a destructive step waits
    /// for its confirmation (not a failure), Stop.
    static func runbook() -> OperationContent? {
        guard let run = RunbookJob.shared.run else { return nil }
        let title = run.plan.name
        if !run.running {
            let phase: OperationPhase = run.stopped ? .stopped : run.succeeded ? .succeeded : .failed
            return OperationContent(title: title, step: RunbookJob.result(run), phase: phase)
        }
        let count = operationCount(index: run.progress.ended, total: run.progress.steps.count)
        return OperationContent(title: title, step: RunbookJob.step(run), done: count?.done, total: count?.total,
                                phase: run.progress.waiting == nil ? .running : .paused,
                                buttons: OperationButton.offered(for: .runbook, stopping: run.stopping))
    }
}
