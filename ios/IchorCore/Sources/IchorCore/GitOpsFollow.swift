import Foundation

// Mirrors go/ichorgo/kube_argocd_follow.go and kube_flux_follow.go: an Argo CD sync and a
// Flux reconcile started from the phone, followed until they end (and a rollout restart,
// followed through the rollout watch).

/// Phases of a followed Argo CD sync: Pending until the controller starts it, Running, then
/// Terminating after a Terminate, and the final one.
public enum ArgoSyncPhase {
    public static let pending = "Pending"
    public static let running = "Running"
    public static let terminating = "Terminating"
    public static let succeeded = "Succeeded"
    public static let failed = "Failed"
    public static let error = "Error"
    public static let terminated = "Terminated"
}

/// Where a followed Argo CD sync stands (ArgoSyncListener.OnProgress).
public struct ArgoSyncProgress: Codable, Equatable, Sendable {
    public let phase: String
    public let message: String
    /// The current sync wave's position among the app's waves, from 1; 0 when unknown.
    public let wave: Int
    public let waves: Int
    /// Resources synced so far, of `total`.
    public let done: Int
    public let total: Int
    /// Unix ms.
    public let at: Int64

    public init(phase: String, message: String = "", wave: Int = 0, waves: Int = 0, done: Int = 0, total: Int = 0, at: Int64 = 0) {
        self.phase = phase
        self.message = message
        self.wave = wave
        self.waves = waves
        self.done = done
        self.total = total
        self.at = at
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        wave = try c.field(.wave, 0)
        waves = try c.field(.waves, 0)
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        at = try c.field(.at, 0)
    }

    /// The controller has not picked the sync up yet.
    public var pending: Bool { phase == ArgoSyncPhase.pending }

    /// The waves are known: "wave 2/4" can be shown.
    public var hasWaves: Bool { waves > 0 && wave > 0 }

    /// The wave shown, kept inside 1…waves.
    public var shownWave: Int { min(max(wave, 1), max(waves, 1)) }

    private enum CodingKeys: String, CodingKey { case phase, message, wave, waves, done, total, at }
}

/// Phases of a followed Flux reconcile (FluxReconcileListener.OnProgress), and the result of
/// one that outlived the 10-minute follow.
public enum FluxReconcilePhase {
    public static let reconciling = "reconciling"
    public static let ready = "ready"
    public static let failed = "failed"
    public static let stillReconciling = "still reconciling"
}

/// Where a followed Flux reconcile stands: the phase and the Ready condition's message.
public struct FluxReconcileProgress: Codable, Equatable, Sendable {
    public let phase: String
    public let message: String
    /// Unix ms.
    public let at: Int64

    public init(phase: String, message: String = "", at: Int64 = 0) {
        self.phase = phase
        self.message = message
        self.at = at
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at }
}

/// How a followed GitOps operation or rollout ended.
public enum FollowOutcome: Equatable, Sendable {
    case succeeded
    /// The error's message, "" when the run only reported its phase (the last step says why).
    case failed(String)
    /// Terminate from the phone stopped the sync.
    case terminated
    /// The follow ended before the operation (Flux's 10 minutes): it may still finish.
    case stillRunning
    /// Stop following: the operation goes on.
    case unfollowed

    /// The Argo CD follow's end (ArgoSyncListener.OnDone): `result` empty and no error after Cancel.
    public static func argo(result: String, error: String?) -> FollowOutcome {
        if let error, !error.isEmpty { return .failed(error) }
        switch result {
        case "": return .unfollowed
        case ArgoSyncPhase.succeeded: return .succeeded
        case ArgoSyncPhase.terminated: return .terminated
        default: return .failed("")
        }
    }

    /// The Flux follow's end (FluxReconcileListener.OnDone), as `argo`.
    public static func flux(result: String, error: String?) -> FollowOutcome {
        if let error, !error.isEmpty { return .failed(error) }
        switch result {
        case "": return .unfollowed
        case FluxReconcilePhase.ready: return .succeeded
        case FluxReconcilePhase.stillReconciling: return .stillRunning
        default: return .failed("")
        }
    }

    /// The rollout watch's latest status: nil while it rolls out.
    public static func rollout(_ status: KubeRolloutStatus) -> FollowOutcome? {
        if status.done { return .succeeded }
        if status.failed { return .failed("") }
        return nil
    }

    /// Where the run's Live Activity says it stands.
    public var phase: OperationPhase {
        switch self {
        case .succeeded: .succeeded
        case .failed: .failed
        case .terminated, .stillRunning, .unfollowed: .stopped
        }
    }
}

extension KubeRolloutStatus {
    /// The bar of a restart: new pods ready of the replicas wanted; nil without replicas.
    public var restartCount: (done: Int, total: Int)? {
        operationCount(index: newReady, total: workload.desired)
    }
}
