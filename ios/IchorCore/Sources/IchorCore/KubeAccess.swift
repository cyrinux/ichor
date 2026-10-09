import Foundation

// "Can I?" (Go kube_access.go): which of the app's Kubernetes actions the cluster's credentials
// may run, asked before the tap. Only a definite refusal disables an action: an unknown answer,
// a load still running or one that failed leaves it offered, and the API server decides.

/// An app action KubeActionAccess answers for (its key in `actions`).
public enum KubeAction: String, CaseIterable, Sendable {
    case restartWorkload, restartStatefulSet, restartDaemonSet, scale, scaleStatefulSet
    case deletePod, execPod, debugPod, suspendCronJob, triggerCronJob, helmRollback, argoSync
    case fluxReconcile, fluxReconcileHelmRelease, fluxReconcileGitRepository, fluxReconcileOCIRepository
    case fluxReconcileHelmRepository, fluxReconcileBucket, cordonNode, drainNode
    /// create and delete on services/proxy: an Alertmanager reached through the service proxy.
    case alertmanagerSilence, alertmanagerExpire

    /// Asked in a namespace; the node actions are cluster-wide whatever the namespace.
    public var isNamespaced: Bool {
        switch self {
        case .cordonNode, .drainNode: false
        default: true
        }
    }

    // The action asked for an object of a kind, on that kind's own resource; nil for a kind the
    // core does not ask about (offered).

    /// A rollout restart (and a Deployment's rollback) of a `kind` workload.
    public static func restart(kind: String) -> KubeAction? {
        switch kind {
        case "Deployment": .restartWorkload
        case "StatefulSet": .restartStatefulSet
        case "DaemonSet": .restartDaemonSet
        default: nil
        }
    }

    /// Setting the replicas of a `kind` workload.
    public static func scale(kind: String) -> KubeAction? {
        switch kind {
        case "Deployment": .scale
        case "StatefulSet": .scaleStatefulSet
        default: nil
        }
    }

    /// Reconciling, suspending or resuming a Flux object of `kind`.
    public static func fluxReconcile(kind: String) -> KubeAction? {
        switch kind {
        case "Kustomization": .fluxReconcile
        case "HelmRelease": .fluxReconcileHelmRelease
        case "GitRepository": .fluxReconcileGitRepository
        case "OCIRepository": .fluxReconcileOCIRepository
        case "HelmRepository": .fluxReconcileHelmRepository
        case "Bucket": .fluxReconcileBucket
        default: nil
        }
    }
}

/// The distinct actions of `actions` in order, nil ones left out: the actions a list names.
public func kubeDistinctActions(_ actions: [KubeAction?]) -> [KubeAction] {
    var seen = Set<KubeAction>()
    return actions.compactMap { $0 }.filter { seen.insert($0).inserted }
}

/// The refusal of `action` over objects of `namespaces` (a bulk action), given the access of
/// each namespace loaded (`access`): the first distinct namespace that refuses it, nil when each
/// allows it or is not known yet. A bulk action runs only where each of its objects may.
public func kubeBulkDenial(for action: KubeAction?, across namespaces: [String], in access: [String: KubeActionAccess]) -> KubeAccess? {
    guard let action else { return nil }
    var seen = Set<String>()
    for namespace in namespaces where seen.insert(namespace).inserted {
        if let denial = access[namespace]?.denial(for: action, in: namespace) { return denial }
    }
    return nil
}

/// The answer for one action or permission: when refused, what was (verb, resource as
/// "resource/subresource", namespace "" for every namespace or cluster-wide) and the API
/// server's reason. Unknown: the review could not be asked.
public struct KubeAccess: Decodable, Equatable, Sendable {
    public let allowed: Bool
    public let unknown: Bool
    public let verb: String
    public let group: String
    public let resource: String
    public let namespace: String
    public let reason: String

    public init(allowed: Bool, unknown: Bool = false, verb: String = "", group: String = "", resource: String = "",
                namespace: String = "", reason: String = "") {
        self.allowed = allowed
        self.unknown = unknown
        self.verb = verb
        self.group = group
        self.resource = resource
        self.namespace = namespace
        self.reason = reason
    }

    private enum CodingKeys: String, CodingKey { case allowed, unknown, verb, group, resource, namespace, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        // A missing answer offers the action: only an explicit refusal disables it.
        allowed = try c.field(.allowed, true)
        unknown = try c.field(.unknown, false)
        verb = try c.field(.verb, "")
        group = try c.field(.group, "")
        resource = try c.field(.resource, "")
        namespace = try c.field(.namespace, "")
        reason = try c.field(.reason, "")
    }

    /// A definite refusal: not allowed, and the review could be asked.
    public var isDenied: Bool { !allowed && !unknown }

    /// Refused for every namespace (or a cluster-scoped resource) rather than one.
    public var isClusterWide: Bool { namespace.isEmpty }

    /// The resource as kubectl names it: "deployments.apps/scale", "pods/exec", "nodes".
    public var deniedResource: String {
        let (base, sub) = splitResource
        let qualified = group.isEmpty ? base : "\(base).\(group)"
        return sub.isEmpty ? qualified : "\(qualified)/\(sub)"
    }

    private var splitResource: (String, String) {
        guard let slash = resource.firstIndex(of: "/") else { return (resource, "") }
        return (String(resource[..<slash]), String(resource[resource.index(after: slash)...]))
    }
}

/// The access to every action of KubeAction in `namespace` ("" for cluster-wide).
public struct KubeActionAccess: Decodable, Equatable, Sendable {
    public let namespace: String
    public let actions: [String: KubeAccess]

    public init(namespace: String, actions: [String: KubeAccess] = [:]) {
        self.namespace = namespace
        self.actions = actions
    }

    private enum CodingKeys: String, CodingKey { case namespace, actions }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        actions = try c.field(.actions, [:])
    }

    /// The refusal of `action` on an object of `objectNamespace` (nil: the namespace asked
    /// about), nil when it may run, is unknown or was not asked. A namespaced action asked for
    /// another namespace (every namespace listed, the object in one) says nothing about this one.
    public func denial(for action: KubeAction, in objectNamespace: String? = nil) -> KubeAccess? {
        guard let access = actions[action.rawValue], access.isDenied else { return nil }
        if action.isNamespaced, let objectNamespace, objectNamespace != namespace { return nil }
        return access
    }
}

/// The namespace every object of a screen is in, "" when none or several: the namespace to ask
/// KubeActionAccess about for them (asked cluster-wide, it then blocks none of them).
public func kubeSharedNamespace(_ namespaces: [String]) -> String {
    guard let first = namespaces.first, namespaces.allSatisfy({ $0 == first }) else { return "" }
    return first
}

public extension FluxRef {
    /// The action whose access its reconcile and suspend follow: a patch of its own kind's
    /// resource (kustomizations, helmreleases, gitrepositories...).
    var accessAction: KubeAction? { KubeAction.fluxReconcile(kind: kind) }
}

/// Who the API server takes the credentials for (SelfSubjectReview, Kubernetes 1.28+).
public struct KubeWhoAmI: Decodable, Equatable, Sendable {
    public let user: String
    public let groups: [String]
    /// The API server cannot say.
    public let unknown: Bool

    public init(user: String, groups: [String] = [], unknown: Bool = false) {
        self.user = user
        self.groups = groups
        self.unknown = unknown
    }

    private enum CodingKeys: String, CodingKey { case user, groups, unknown }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        user = try c.field(.user, "")
        groups = try c.field(.groups, [])
        unknown = try c.field(.unknown, false)
    }

    /// Worth a row: known, with a user name.
    public var isKnown: Bool { !unknown && !user.isEmpty }

    /// The groups in one line, the ones every authenticated user has left out.
    public var groupsLine: String {
        groups.filter { $0 != "system:authenticated" }.joined(separator: ", ")
    }
}
