import Foundation

// Mirrors go/ichorgo/kube_actions.go: scale, CronJob suspend, Deployment rollback, pod logs.

/// KubeScale's upper bound.
public let kubeMaxScaleReplicas = 1000

/// The log lines KubePodLogs is asked for (Go caps them at 5000).
public let kubePodLogTail = 1000

extension KubeWorkload {
    /// Deployments and StatefulSets scale; a DaemonSet runs one pod per node.
    public var canScale: Bool { kind == "Deployment" || kind == "StatefulSet" }

    /// Only Deployments keep revisions (their ReplicaSets) to roll back to.
    public var hasHistory: Bool { kind == "Deployment" }
}

/// Scaling to zero stops every pod: the user types the workload's name to confirm it.
public func scaleNeedsTypedName(_ replicas: Int) -> Bool { replicas == 0 }

/// One revision of a Deployment, one of its ReplicaSets (KubeDeploymentRevisions).
public struct DeploymentRevision: Decodable, Equatable, Identifiable, Sendable {
    public let revision: Int
    public let replicaSet: String
    /// Unix ms.
    public let created: Int64
    public let images: [String]
    /// The kubernetes.io/change-cause annotation, "" when none.
    public let changeCause: String
    public let replicas: Int
    /// The revision the Deployment runs now (rolling back to it is refused).
    public let current: Bool

    public var id: Int { revision }

    public init(revision: Int, replicaSet: String = "", created: Int64 = 0, images: [String] = [], changeCause: String = "",
                replicas: Int = 0, current: Bool = false) {
        self.revision = revision
        self.replicaSet = replicaSet
        self.created = created
        self.images = images
        self.changeCause = changeCause
        self.replicas = replicas
        self.current = current
    }

    private enum CodingKeys: String, CodingKey { case revision, replicaSet, created, images, changeCause, replicas, current }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        revision = try c.decodeIfPresent(Int.self, forKey: .revision) ?? 0
        replicaSet = try c.decodeIfPresent(String.self, forKey: .replicaSet) ?? ""
        created = try c.decodeIfPresent(Int64.self, forKey: .created) ?? 0
        images = try c.decodeIfPresent([String].self, forKey: .images) ?? []
        changeCause = try c.decodeIfPresent(String.self, forKey: .changeCause) ?? ""
        replicas = try c.decodeIfPresent(Int.self, forKey: .replicas) ?? 0
        current = try c.decodeIfPresent(Bool.self, forKey: .current) ?? false
    }
}

/// Newest first.
public struct DeploymentRevisionList: Decodable, Equatable, Sendable {
    public let revisions: [DeploymentRevision]

    public init(revisions: [DeploymentRevision] = []) { self.revisions = revisions }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        revisions = try c.decodeIfPresent([DeploymentRevision].self, forKey: .revisions) ?? []
    }

    private enum CodingKeys: String, CodingKey { case revisions }
}

/// The containers the Kubernetes API offers when a pod's log is asked without a container
/// ("a container name must be specified for pod web-1, choose one of: [app sidecar]"); empty
/// for any other error. A fallback for a core whose pods list lacks containerNames.
public func podLogContainerChoices(fromError message: String) -> [String] {
    guard let start = message.range(of: "choose one of: [") else { return [] }
    let rest = message[start.upperBound...]
    guard let end = rest.firstIndex(of: "]") else { return [] }
    return rest[..<end].split(separator: " ").map(String.init)
}

/// A log's lines, without the empty one after the final newline.
public func podLogLines(_ text: String) -> [String] {
    var lines = text.components(separatedBy: "\n")
    if lines.last?.isEmpty == true { lines.removeLast() }
    return lines
}
