import Foundation

// Drill-down pod lists (plans/roadmap/large-clusters.md, Phase 5): the Kubernetes pods of one
// node (KubeNodePodsPage) or of one workload (KubeWorkloadPodsPage), page by page. Same rules
// as Android's model/SelectedPods.kt.

/// Rows a drill-down page asks for: the first page eagerly, the next ones on scroll.
public let selectedPodsPageSize = 200

/// The phase a drill-down list is narrowed to, as the Go core takes it (`query`).
public enum PodPhaseFilter: String, CaseIterable, Hashable, Sendable {
    case all, running
    /// Every pod but the finished ones (Jobs' Completed pods).
    case notCompleted

    public var query: String {
        switch self {
        case .all: ""
        case .running: "Running"
        case .notCompleted: "!Succeeded"
        }
    }
}

/// What a drill-down pod list shows.
public enum PodSelection: Hashable, Sendable {
    /// The pods scheduled on the Talos node at this address, in every namespace.
    case node(String)
    /// The pods a Deployment, StatefulSet or DaemonSet's selector matches.
    case workload(kind: String, namespace: String, name: String)

    /// Its rows come from several namespaces: each row says which.
    public var showsNamespace: Bool {
        if case .node = self { return true }
        return false
    }

    /// Its rows run on several nodes: each row says which.
    public var showsNode: Bool { !showsNamespace }
}

extension KubeWorkload {
    /// The selection of its pods; nil for a kind the Go core cannot list them of.
    public var podSelection: PodSelection? {
        workloadKinds.contains(kind) ? .workload(kind: kind, namespace: namespace, name: name) : nil
    }
}
