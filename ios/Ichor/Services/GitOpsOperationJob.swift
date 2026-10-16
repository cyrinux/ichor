import Foundation
import IchorCore

/// The Argo CD sync and the Flux reconcile the phone started, followed app-wide until they end
/// (one of each at a time; a new one replaces the last): their Live Activities show the wave
/// and the resources, or the reconcile, after the user left the screen. Go follows them
/// (StartArgoSyncFollow, StartFluxReconcileFollow); the sync and the reconcile themselves run
/// in the cluster, so the app being suspended only stops following.
@Observable
@MainActor
final class GitOpsOperationJob {
    static let shared = GitOpsOperationJob()

    struct ArgoRun: Equatable {
        let namespace: String
        let name: String
        var progress: ArgoSyncProgress?
        /// nil while following.
        var outcome: FollowOutcome?
        /// Terminate was asked: the follow goes on until the operation ends.
        var terminating = false

        var running: Bool { outcome == nil }
    }

    struct FluxRun: Equatable {
        let target: FluxRef
        var progress: FluxReconcileProgress?
        var outcome: FollowOutcome?

        var running: Bool { outcome == nil }
    }

    private(set) var argo: ArgoRun?
    private(set) var flux: FluxRun?
    /// Why the last Terminate was refused, until the next sync.
    private(set) var terminateError: String?
    private var argoHandle: ArgoSyncHandle?
    private var fluxHandle: FluxReconcileHandle?
    /// Bumped by each start: the events of a replaced follow are dropped.
    private var argoGeneration = 0
    private var fluxGeneration = 0
    private let argoBackground = OperationBackgroundTask()
    private let fluxBackground = OperationBackgroundTask()

    // MARK: Argo CD

    /// Follows the sync of the Application namespace/name the screen just requested.
    func followArgoSync(client: TalosClient, namespace: String, name: String) {
        argoHandle?.cancel()
        argoGeneration += 1
        let generation = argoGeneration
        argo = ArgoRun(namespace: namespace, name: name)
        terminateError = nil
        OperationNotice.prepare()
        argoBackground.begin("Argo CD sync")
        let handle = client.startArgoSyncFollow(namespace: namespace, name: name)
        argoHandle = handle
        Task {
            var outcome = FollowOutcome.unfollowed
            for await event in handle.events where generation == argoGeneration {
                switch event {
                case .progress(let p): argo?.progress = p
                case .done(let result, let error): outcome = .argo(result: result, error: error)
                }
            }
            guard generation == argoGeneration else { return }
            finishArgo(outcome)
        }
    }

    /// Stops the sync itself; the follow reports it Terminated.
    func terminate() {
        guard let handle = argoHandle, let run = argo, run.running, !run.terminating else { return }
        let generation = argoGeneration
        argo?.terminating = true
        terminateError = nil
        Task {
            do {
                try await handle.terminate()
            } catch {
                guard generation == argoGeneration else { return }
                argo?.terminating = false
                terminateError = error.localizedDescription
            }
        }
    }

    /// Stops following; the sync goes on in the cluster.
    func stopFollowingArgo() {
        guard argo?.running == true else { return }
        argoHandle?.cancel()
    }

    /// Forgets a finished sync.
    func clearArgo() {
        guard argo?.running != true else { return }
        argo = nil
        terminateError = nil
    }

    private func finishArgo(_ outcome: FollowOutcome) {
        argo?.outcome = outcome
        argoHandle = nil
        argoBackground.end()
        guard let run = argo, outcome != .unfollowed else { return }
        OperationNotice.post(.argoSync, title: Self.argoTitle(run), body: Self.argoResult(run))
    }

    // MARK: Flux

    /// Follows the reconcile of the Flux object `target` the screen just requested.
    func followFluxReconcile(client: TalosClient, target: FluxRef) {
        fluxHandle?.cancel()
        fluxGeneration += 1
        let generation = fluxGeneration
        flux = FluxRun(target: target)
        OperationNotice.prepare()
        fluxBackground.begin("Flux reconcile")
        let handle = client.startFluxReconcileFollow(kind: target.kind, namespace: target.namespace, name: target.name)
        fluxHandle = handle
        Task {
            var outcome = FollowOutcome.unfollowed
            for await event in handle.events where generation == fluxGeneration {
                switch event {
                case .progress(let p): flux?.progress = p
                case .done(let result, let error): outcome = .flux(result: result, error: error)
                }
            }
            guard generation == fluxGeneration else { return }
            finishFlux(outcome)
        }
    }

    /// Stops following; the reconcile goes on in the cluster.
    func stopFollowingFlux() {
        guard flux?.running == true else { return }
        fluxHandle?.cancel()
    }

    /// Forgets a finished reconcile.
    func clearFlux() {
        guard flux?.running != true else { return }
        flux = nil
    }

    private func finishFlux(_ outcome: FollowOutcome) {
        flux?.outcome = outcome
        fluxHandle = nil
        fluxBackground.end()
        guard let run = flux, outcome != .unfollowed else { return }
        OperationNotice.post(.fluxReconcile, title: Self.fluxTitle(run), body: Self.fluxResult(run))
    }
}

/// How the followed runs are worded, in their Live Activity and their notification.
extension GitOpsOperationJob {
    static func argoTitle(_ run: ArgoRun) -> String {
        String(localized: "Syncing \(run.name)")
    }

    /// "Wave 2/4 · 12/30 resources", or what the sync does before it counts.
    static func argoStep(_ run: ArgoRun) -> String {
        if run.terminating { return String(localized: "Terminating the sync…") }
        guard let p = run.progress, !p.pending else { return String(localized: "Waiting for Argo CD to start the sync…") }
        if p.hasWaves, p.total > 0 {
            return String(localized: "Wave \(p.shownWave)/\(p.waves) · \(p.done)/\(p.total) resources")
        }
        if p.total > 0 { return String(localized: "\(p.done)/\(p.total) resources") }
        return p.message.isEmpty ? String(localized: "Syncing…") : p.message
    }

    static func argoResult(_ run: ArgoRun) -> String {
        switch run.outcome {
        case .succeeded?: String(localized: "Sync succeeded")
        case .terminated?: String(localized: "Sync terminated")
        case .failed(let message)?:
            message.nonEmpty ?? run.progress?.message.nonEmpty ?? String(localized: "Sync failed")
        case .unfollowed?, .stillRunning?, nil:
            String(localized: "Stopped following. The sync goes on in Argo CD.")
        }
    }

    static func fluxTitle(_ run: FluxRun) -> String {
        String(localized: "Reconciling \(run.target.name)")
    }

    static func fluxStep(_ run: FluxRun) -> String {
        run.progress?.message.nonEmpty ?? String(localized: "Reconciling…")
    }

    static func fluxResult(_ run: FluxRun) -> String {
        switch run.outcome {
        case .succeeded?: String(localized: "Ready")
        case .failed(let message)?:
            message.nonEmpty ?? run.progress?.message.nonEmpty ?? String(localized: "Reconcile failed")
        case .stillRunning?: String(localized: "Still reconciling after 10 minutes: check it again later.")
        case .unfollowed?, .terminated?, nil:
            String(localized: "Stopped following. The reconcile goes on in Flux.")
        }
    }
}
