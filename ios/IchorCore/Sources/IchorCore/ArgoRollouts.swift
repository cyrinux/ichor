import Foundation

// Mirrors go/ichorgo/kube_argo_rollouts.go (KubeArgoRollouts) and kube_argo_rollouts_actions.go
// (KubeArgoRolloutAction): Argo Rollouts' canary and blue-green rollouts, read and piloted like
// the `kubectl argo rollouts` plugin.

/// The Rollouts of every namespace, worst first.
public struct ArgoRolloutsStatus: Decodable, Equatable, Sendable {
    /// False: the cluster does not serve Rollouts (no error).
    public let installed: Bool
    public let rollouts: [ArgoRollout]

    public init(installed: Bool = false, rollouts: [ArgoRollout] = []) {
        self.installed = installed
        self.rollouts = rollouts
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(installed: try c.field(.installed, false), rollouts: try c.field(.rollouts, []))
    }

    private enum CodingKeys: String, CodingKey { case installed, rollouts }

    /// Something is moving (a step, an analysis running): worth polling.
    public var needsPolling: Bool { rollouts.contains(where: \.needsPolling) }

    /// The rollouts by namespace, namespaces sorted, each keeping the core's order (worst first).
    public var byNamespace: [(namespace: String, rollouts: [ArgoRollout])] {
        Dictionary(grouping: rollouts, by: \.namespace)
            .sorted { $0.key < $1.key }
            .map { (namespace: $0.key, rollouts: $0.value) }
    }
}

public struct ArgoRollout: Decodable, Equatable, Identifiable, Hashable, Sendable {
    public static let canary = "canary"
    public static let blueGreen = "blueGreen"

    public let namespace: String
    public let name: String
    /// canary or blueGreen.
    public let strategy: String
    /// The controller's own word: Healthy, Progressing, Paused, Degraded… (free text).
    public let phase: String
    public let message: String
    /// By spec.paused or a pause condition (a pause step, a blue-green awaiting promotion).
    public let paused: Bool
    public let pauseReasons: [String]
    /// Unix ms the first pause condition started, 0 when none.
    public let pausedSince: Int64
    /// The canary's current step index, -1 when not started (or a blue-green).
    public let step: Int
    public let steps: [ArgoRolloutStep]
    /// The canary's share of the traffic, in percent.
    public let weight: Int
    public let aborted: Bool
    public let promoteFull: Bool
    public let stableHash: String
    public let currentHash: String
    public let replicas: Int
    public let updated: Int
    public let ready: Int
    public let available: Int
    /// The running or last analysis, nil when none.
    public let analysis: ArgoRolloutAnalysis?
    /// A blue-green promotes by itself.
    public let autoPromotion: Bool
    /// "Deployment/name" when the pods come from one, "" otherwise.
    public let workloadRef: String
    public let images: [String]
    public let level: ServiceHealth
    /// Unix ms of the last progress, 0 when unknown.
    public let updatedAt: Int64

    public var id: String { "\(namespace)/\(name)" }
    public var isBlueGreen: Bool { strategy == Self.blueGreen }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        strategy = try c.field(.strategy, Self.canary)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        paused = try c.field(.paused, false)
        pauseReasons = try c.field(.pauseReasons, [])
        pausedSince = try c.field(.pausedSince, 0)
        step = try c.field(.step, -1)
        steps = try c.field(.steps, [])
        weight = try c.field(.weight, 0)
        aborted = try c.field(.aborted, false)
        promoteFull = try c.field(.promoteFull, false)
        stableHash = try c.field(.stableHash, "")
        currentHash = try c.field(.currentHash, "")
        replicas = try c.field(.replicas, 0)
        updated = try c.field(.updated, 0)
        ready = try c.field(.ready, 0)
        available = try c.field(.available, 0)
        analysis = try c.decodeIfPresent(ArgoRolloutAnalysis.self, forKey: .analysis)
        autoPromotion = try c.field(.autoPromotion, false)
        workloadRef = try c.field(.workloadRef, "")
        images = try c.field(.images, [])
        level = try c.wire(.level)
        updatedAt = try c.field(.updatedAt, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, strategy, phase, message, paused, pauseReasons, pausedSince, step, steps, weight
        case aborted, promoteFull, stableHash, currentHash, replicas, updated, ready, available, analysis
        case autoPromotion, workloadRef, images, level, updatedAt
    }

    /// A new version is out next to the stable one: something to promote or abort.
    private var hasNewVersion: Bool { !currentHash.isEmpty && currentHash != stableHash }

    // What each action can do in this state; the Go core refuses the rest anyway.

    /// A paused rollout moves on; a blue-green with a new version switches the traffic.
    public var canPromote: Bool { paused && !aborted || (isBlueGreen && hasNewVersion && !aborted) }
    public var canPromoteFull: Bool { !aborted && hasNewVersion }
    public var canAbort: Bool { !aborted && hasNewVersion }
    public var canRetry: Bool { aborted }
    public var canPause: Bool { !paused && !aborted && currentHash != stableHash }

    public func allows(_ action: ArgoRolloutAction) -> Bool {
        switch action {
        case .promote: canPromote
        case .promoteFull: canPromoteFull
        case .abort: canAbort
        case .retry: canRetry
        case .pause: canPause
        }
    }

    /// The share of the canary steps done, 0…1; 0 without steps.
    public var progress: Double {
        guard !steps.isEmpty else { return 0 }
        return Double(min(max(step, 0), steps.count)) / Double(steps.count)
    }

    /// The step shown, 1-based: the one running, the last once all are done, 0 before the first.
    public var displayStep: Int { min(max(step + 1, 0), steps.count) }

    /// A blue-green holding its new version until someone promotes it.
    public var awaitsPromotion: Bool { isBlueGreen && paused && !aborted && hasNewVersion }

    /// A step on its way, or a paused rollout whose analysis still runs: poll it.
    public var needsPolling: Bool { phase == "Progressing" || (paused && analysis?.status == "Running") }

    /// The first characters of a pod-template hash ("" stays "").
    public static func shortHash(_ hash: String) -> String { String(hash.prefix(7)) }
}

public struct ArgoRolloutStep: Decodable, Equatable, Hashable, Sendable {
    /// setWeight, pause, analysis, experiment, setCanaryScale, setHeaderRoute, setMirrorRoute,
    /// plugin or other.
    public let kind: String
    /// "20 %", "1h", "∞", a template name, or "".
    public let label: String

    public init(kind: String, label: String = "") {
        self.kind = kind
        self.label = label
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(kind: try c.field(.kind, "other"), label: try c.field(.label, ""))
    }

    private enum CodingKeys: String, CodingKey { case kind, label }
}

public struct ArgoRolloutAnalysis: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    /// Running, Successful, Failed, Error, Inconclusive…
    public let status: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        status = try c.field(.status, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case name, status, message }

    /// The analysis did not pass.
    public var failed: Bool { ["Failed", "Error"].contains(status) }
}

/// The actions of KubeArgoRolloutAction, as its wire names.
public enum ArgoRolloutAction: String, CaseIterable, Identifiable, Sendable {
    case promote, promoteFull, abort, retry, pause

    public var id: String { rawValue }
}
