import Foundation

// Mirrors go/ichorgo/kube_revisions.go: the rollout history of a Deployment, StatefulSet or
// DaemonSet, the pod template diff before a rollback, and a Deployment's rollout pause.

extension KubeWorkload {
    /// Deployments (ReplicaSets), StatefulSets and DaemonSets (ControllerRevisions) keep
    /// revisions to roll back to.
    public var hasHistory: Bool { ["Deployment", "StatefulSet", "DaemonSet"].contains(kind) }

    /// Only a Deployment's rollout can be paused.
    public var canPauseRollout: Bool { kind == "Deployment" }

    /// A paused Deployment: Resume rather than Pause, and no rollback until it resumes.
    public var rolloutPaused: Bool { workloadState == .paused }
}

/// A workload's revisions, newest first (KubeWorkloadRevisions).
public struct WorkloadRevisionList: Decodable, Equatable, Sendable {
    public let kind: String
    /// The pods only take a rolled back template when deleted (OnDelete, StatefulSet partition).
    public let manual: Bool
    /// A paused Deployment: its rollback is refused until the rollout resumes.
    public let paused: Bool
    public let revisions: [DeploymentRevision]

    public init(kind: String = "", manual: Bool = false, paused: Bool = false, revisions: [DeploymentRevision] = []) {
        self.kind = kind
        self.manual = manual
        self.paused = paused
        self.revisions = revisions
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        manual = try c.field(.manual, false)
        paused = try c.field(.paused, false)
        revisions = try c.field(.revisions, [])
    }

    private enum CodingKeys: String, CodingKey { case kind, manual, paused, revisions }
}

/// The images of the current pod template and of a revision's, in container order.
public struct RevisionImages: Decodable, Equatable, Sendable {
    public let from: [String]
    public let to: [String]

    public init(from: [String] = [], to: [String] = []) {
        self.from = from
        self.to = to
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        from = try c.field(.from, [])
        to = try c.field(.to, [])
    }

    private enum CodingKeys: String, CodingKey { case from, to }

    /// The images a rollback changes, container by container: only those that differ; a
    /// container only one side has comes with "" on the other.
    public var changes: [ImageChange] {
        (0..<max(from.count, to.count)).compactMap { i in
            let change = ImageChange(
                from: i < from.count ? from[i] : "", to: i < to.count ? to[i] : "")
            return change.from == change.to ? nil : change
        }
    }
}

/// One image a rollback changes: `from` what runs now, `to` what the revision ran ("" for none).
public struct ImageChange: Equatable, Hashable, Sendable {
    public let from: String
    public let to: String

    public init(from: String, to: String) {
        self.from = from
        self.to = to
    }
}

/// The pod template diff from the current revision to an older one (KubeRevisionDiff).
public struct RevisionDiff: Decodable, Equatable, Sendable {
    /// Unified, "" when the templates are equal.
    public let diff: String
    public let from: String
    public let to: String
    public let fromRevision: Int
    public let toRevision: Int
    public let images: RevisionImages

    public init(
        diff: String = "", from: String = "", to: String = "", fromRevision: Int = 0, toRevision: Int = 0,
        images: RevisionImages = RevisionImages()
    ) {
        self.diff = diff
        self.from = from
        self.to = to
        self.fromRevision = fromRevision
        self.toRevision = toRevision
        self.images = images
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        diff = try c.field(.diff, "")
        from = try c.field(.from, "")
        to = try c.field(.to, "")
        fromRevision = try c.field(.fromRevision, 0)
        toRevision = try c.field(.toRevision, 0)
        images = try c.field(.images, RevisionImages())
    }

    private enum CodingKeys: String, CodingKey { case diff, from, to, fromRevision, toRevision, images }
}
