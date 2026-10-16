import Foundation

// Mirrors go/ichorgo/kube_policies.go: Kyverno's admission policies with their PolicyReport
// findings, Gatekeeper's constraints with their audit violations, and the pure logic the
// Policies screen filters them with.

/// What a policy does to a request that breaks a rule. Mixed is Ichor's: the rules' own
/// failure actions disagree with the policy's (or a Gatekeeper constraint is "scoped"). Warn is
/// Gatekeeper's: the request is admitted with a warning.
public enum PolicyAction: String, Sendable, CaseIterable, WireEnum {
    case enforce = "Enforce"
    case audit = "Audit"
    case mixed = "Mixed"
    case warn = "Warn"
    case unknown = ""

    public static let wireFallback: Self = .unknown
}

/// The admission-policy engine a policy or an engine card belongs to.
public enum PolicyEngineName: String, Sendable, WireEnum {
    case kyverno
    case gatekeeper
    case unknown = ""

    public static let wireFallback: Self = .unknown

    /// The project's own name, never translated.
    public var title: String {
        switch self {
        case .kyverno: "Kyverno"
        case .gatekeeper: "Gatekeeper"
        case .unknown: ""
        }
    }
}

/// Kyverno's policies and Gatekeeper's constraints (os:admin); `installed` false and nothing else
/// on a cluster with neither.
public struct PolicyReport: Decodable, Equatable, Sendable {
    public let installed: Bool
    public let kyverno: Bool
    public let gatekeeper: Bool
    /// The served kyverno.io version.
    public let version: String
    /// A Kyverno policy or engine read that failed; the rest still shows.
    public let error: String
    /// A Gatekeeper template, constraint or engine read that failed.
    public let gatekeeperError: String
    /// The policy reports could not be read (often a 403): the counts stay at 0.
    public let reportsError: String
    /// One per engine that runs: Kyverno first.
    public let engines: [PolicyEngine]
    /// Not ready first, then Enforce, Mixed, Warn, Audit, the most failing first (the Go order).
    public let policies: [KyvernoPolicy]
    public let namespaces: [PolicyNamespaceTotals]
    public let reportsRead: Int
    /// The read stopped taking reports: the counts cover the first `reportsRead`.
    public let reportsTruncated: Bool

    /// Both engines run: each row says which one it belongs to.
    public var bothEngines: Bool { kyverno && gatekeeper }

    public init(installed: Bool = false, kyverno: Bool = false, gatekeeper: Bool = false, version: String = "", error: String = "",
                gatekeeperError: String = "", reportsError: String = "", engines: [PolicyEngine] = [], policies: [KyvernoPolicy] = [],
                namespaces: [PolicyNamespaceTotals] = [], reportsRead: Int = 0, reportsTruncated: Bool = false) {
        self.installed = installed
        self.kyverno = kyverno
        self.gatekeeper = gatekeeper
        self.version = version
        self.error = error
        self.gatekeeperError = gatekeeperError
        self.reportsError = reportsError
        self.engines = engines
        self.policies = policies
        self.namespaces = namespaces
        self.reportsRead = reportsRead
        self.reportsTruncated = reportsTruncated
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        kyverno = try c.field(.kyverno, false)
        gatekeeper = try c.field(.gatekeeper, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        gatekeeperError = try c.field(.gatekeeperError, "")
        reportsError = try c.field(.reportsError, "")
        engines = try c.field(.engines, [])
        policies = try c.field(.policies, [])
        namespaces = try c.field(.namespaces, [])
        reportsRead = try c.field(.reportsRead, 0)
        reportsTruncated = try c.field(.reportsTruncated, false)
    }

    private enum CodingKeys: String, CodingKey {
        case installed, kyverno, gatekeeper, version, error, gatekeeperError, reportsError, engines, policies, namespaces
        case reportsRead, reportsTruncated
    }

    /// The policies a `namespace` (nil: every one) and a `query` keep, in the report's order. A
    /// namespace keeps its own policies, and the cluster-wide ones that checked something there
    /// or are not ready (a broken policy matters everywhere).
    public func filtered(namespace: String?, query: String) -> [KyvernoPolicy] {
        let q = query.trimmingCharacters(in: .whitespaces)
        return policies
            .filter { policy in
                guard let namespace else { return true }
                return policy.namespace == namespace || (policy.clusterWide && (!policy.ready || policy.checks(namespace)))
            }
            .filter { policy in
                q.isEmpty || ([policy.name, policy.namespace, policy.kind, policy.category, policy.severity, policy.description, policy.action.rawValue]
                    + policy.ruleNames)
                    .contains { $0.localizedCaseInsensitiveContains(q) }
            }
    }

    /// The namespaces with findings or policies, for the namespace picker.
    public var namespaceNames: [String] {
        Array(Set((namespaces.map(\.namespace) + policies.map(\.namespace)).filter { !$0.isEmpty })).sorted()
    }
}

/// An engine: its controllers, and what its webhook does while it is down.
public struct PolicyEngine: Decodable, Equatable, Identifiable, Sendable {
    public let name: PolicyEngineName
    public let namespace: String
    public let controllers: [PolicyController]
    public let webhook: PolicyWebhook

    public var id: String { name.rawValue }

    public init(name: PolicyEngineName = .kyverno, namespace: String = "", controllers: [PolicyController] = [],
                webhook: PolicyWebhook = PolicyWebhook()) {
        self.name = name
        self.namespace = namespace
        self.controllers = controllers
        self.webhook = webhook
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.wire(.name)
        namespace = try c.field(.namespace, "")
        controllers = try c.field(.controllers, [])
        webhook = try c.field(.webhook, PolicyWebhook())
    }

    private enum CodingKeys: String, CodingKey { case name, namespace, controllers, webhook }
}

public struct PolicyController: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let ready: Int
    public let desired: Int

    public var id: String { name }
    public var healthy: Bool { desired > 0 && ready >= desired }

    public init(name: String, ready: Int = 0, desired: Int = 0) {
        self.name = name
        self.ready = ready
        self.desired = desired
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        ready = try c.field(.ready, 0)
        desired = try c.field(.desired, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, ready, desired }
}

/// The admission webhook, and what it does to requests while Kyverno is down.
public struct PolicyWebhook: Decodable, Equatable, Sendable {
    public enum FailurePolicy: String, Sendable, WireEnum {
        /// Requests are refused while Kyverno is down.
        case fail = "Fail"
        /// Policies are skipped while Kyverno is down.
        case ignore = "Ignore"
        case unknown = ""

        public static let wireFallback: Self = .unknown
    }

    public let present: Bool
    public let failurePolicy: FailurePolicy

    public init(present: Bool = false, failurePolicy: FailurePolicy = .unknown) {
        self.present = present
        self.failurePolicy = failurePolicy
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        present = try c.field(.present, false)
        failurePolicy = try c.wire(.failurePolicy)
    }

    private enum CodingKeys: String, CodingKey { case present, failurePolicy }
}

/// A Kyverno policy or a Gatekeeper constraint (`engine` says which).
public struct KyvernoPolicy: Decodable, Equatable, Identifiable, Sendable {
    /// "" for a ClusterPolicy or a constraint.
    public let namespace: String
    public let name: String
    public let engine: PolicyEngineName
    /// A constraint's kind (its template's): K8sRequiredLabels.
    public let kind: String
    public let action: PolicyAction
    /// It also checks the resources already there, not only new requests.
    public let background: Bool
    public let ready: Bool
    public let readyReason: String
    public let readyMessage: String
    public let category: String
    public let severity: String
    public let description: String
    public let rules: PolicyRules
    public let ruleNames: [String]
    public let pass: Int
    public let fail: Int
    public let warn: Int
    public let errors: Int
    public let skip: Int
    /// The newest failing resources, at most 20; `fail` counts them all.
    public let failing: [PolicyFailure]
    /// `failing` is the first violations Gatekeeper kept (its own limit), not all of `fail`.
    public let failingCapped: Bool
    /// When Gatekeeper's audit last wrote the violations, unix seconds; 0 for Kyverno.
    public let auditAt: Int64
    /// "argocd" or "flux" when a GitOps tool applies it: a change from the phone is reverted on its next sync.
    public let managedBy: String
    /// The same counts per namespace: a ClusterPolicy's results spread over every namespace.
    public let byNamespace: [PolicyNamespaceTotals]

    /// Unique in a report: a Gatekeeper constraint's name is unique per kind only.
    public var id: String { "\(engine.rawValue)/\(kind)/\(namespace)/\(name)" }
    public var clusterWide: Bool { namespace.isEmpty }

    /// The modes a Kyverno policy can be switched to from here: the other one, or both when Mixed.
    public var switchTargets: [PolicyAction] {
        guard engine == .kyverno else { return [] }
        switch action {
        case .enforce: return [.audit]
        case .audit: return [.enforce]
        case .mixed: return [.audit, .enforce]
        case .warn, .unknown: return []
        }
    }

    /// The resource the switch patches, as kubeCan takes it.
    public var kyvernoResource: String { clusterWide ? "clusterpolicies" : "policies" }

    /// The GitOps tool that would revert a switch, by its own name; nil when none applies the policy.
    public var gitopsTool: String? {
        switch managedBy {
        case "argocd": "Argo CD"
        case "flux": "Flux"
        default: nil
        }
    }

    /// Its counts in `namespace`, or across the cluster for nil.
    public func counts(namespace: String?) -> PolicyResultCounts {
        guard let namespace else { return PolicyResultCounts(fail: fail, warn: warn, pass: pass) }
        guard let row = byNamespace.first(where: { $0.namespace == namespace }) else { return PolicyResultCounts() }
        return PolicyResultCounts(fail: row.fail, warn: row.warn, pass: row.pass)
    }

    /// It has results in `namespace`: it checked something there.
    public func checks(_ namespace: String) -> Bool {
        byNamespace.contains { $0.namespace == namespace && $0.pass + $0.fail + $0.warn + $0.errors > 0 }
    }

    public init(namespace: String = "", name: String, engine: PolicyEngineName = .kyverno, kind: String = "", action: PolicyAction = .unknown,
                background: Bool = false, ready: Bool = true, readyReason: String = "", readyMessage: String = "", category: String = "",
                severity: String = "", description: String = "", rules: PolicyRules = PolicyRules(), ruleNames: [String] = [],
                pass: Int = 0, fail: Int = 0, warn: Int = 0, errors: Int = 0, skip: Int = 0, failing: [PolicyFailure] = [],
                failingCapped: Bool = false, auditAt: Int64 = 0, managedBy: String = "", byNamespace: [PolicyNamespaceTotals] = []) {
        self.namespace = namespace
        self.name = name
        self.engine = engine
        self.kind = kind
        self.action = action
        self.background = background
        self.ready = ready
        self.readyReason = readyReason
        self.readyMessage = readyMessage
        self.category = category
        self.severity = severity
        self.description = description
        self.rules = rules
        self.ruleNames = ruleNames
        self.pass = pass
        self.fail = fail
        self.warn = warn
        self.errors = errors
        self.skip = skip
        self.failing = failing
        self.failingCapped = failingCapped
        self.auditAt = auditAt
        self.managedBy = managedBy
        self.byNamespace = byNamespace
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        engine = PolicyEngineName(wire: try c.field(.engine, PolicyEngineName.kyverno.rawValue))
        kind = try c.field(.kind, "")
        action = try c.wire(.action)
        background = try c.field(.background, false)
        ready = try c.field(.ready, false)
        readyReason = try c.field(.readyReason, "")
        readyMessage = try c.field(.readyMessage, "")
        category = try c.field(.category, "")
        severity = try c.field(.severity, "")
        description = try c.field(.description, "")
        rules = try c.field(.rules, PolicyRules())
        ruleNames = try c.field(.ruleNames, [])
        pass = try c.field(.pass, 0)
        fail = try c.field(.fail, 0)
        warn = try c.field(.warn, 0)
        errors = try c.field(.errors, 0)
        skip = try c.field(.skip, 0)
        failing = try c.field(.failing, [])
        failingCapped = try c.field(.failingCapped, false)
        auditAt = try c.field(.auditAt, 0)
        managedBy = try c.field(.managedBy, "")
        byNamespace = try c.field(.byNamespace, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, engine, kind, action, background, ready, readyReason, readyMessage, category, severity, description
        case rules, ruleNames, pass, fail, warn, skip, failing, failingCapped, auditAt, managedBy, byNamespace
        case errors = "error"
    }
}

/// What switching a Kyverno policy's mode answers: the policy as read after the patch.
public struct PolicyActionResult: Decodable, Equatable, Sendable {
    public let namespace: String
    public let name: String
    public let action: PolicyAction
    public let ready: Bool
    public let rulesPatched: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        action = try c.wire(.action)
        ready = try c.field(.ready, false)
        rulesPatched = try c.field(.rulesPatched, 0)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, action, ready, rulesPatched }
}

/// A policy's failing, warning and passing results, over the cluster or in one namespace.
public struct PolicyResultCounts: Equatable, Sendable {
    public let fail: Int
    public let warn: Int
    public let pass: Int

    public init(fail: Int = 0, warn: Int = 0, pass: Int = 0) {
        self.fail = fail
        self.warn = warn
        self.pass = pass
    }
}

/// A policy's rules by kind.
public struct PolicyRules: Decodable, Equatable, Sendable {
    public let validate: Int
    public let mutate: Int
    public let generate: Int
    public let verifyImages: Int

    public init(validate: Int = 0, mutate: Int = 0, generate: Int = 0, verifyImages: Int = 0) {
        self.validate = validate
        self.mutate = mutate
        self.generate = generate
        self.verifyImages = verifyImages
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        validate = try c.field(.validate, 0)
        mutate = try c.field(.mutate, 0)
        generate = try c.field(.generate, 0)
        verifyImages = try c.field(.verifyImages, 0)
    }

    private enum CodingKeys: String, CodingKey { case validate, mutate, generate, verifyImages }

    /// "validate 2  ·  verifyImages 1": the kinds set, as Kyverno spells them.
    public var text: String {
        [("validate", validate), ("mutate", mutate), ("generate", generate), ("verifyImages", verifyImages)]
            .filter { $0.1 > 0 }
            .map { "\($0.0) \($0.1)" }
            .joined(separator: "  ·  ")
    }
}

/// A resource a policy's rule fails.
public struct PolicyFailure: Decodable, Equatable, Identifiable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let rule: String
    public let message: String
    public let severity: String
    /// Unix seconds, 0 when the result had none.
    public let at: Int64

    public var id: String { "\(kind)/\(namespace)/\(name)/\(rule)" }

    public init(kind: String = "", namespace: String = "", name: String = "", rule: String = "", message: String = "", severity: String = "", at: Int64 = 0) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
        self.rule = rule
        self.message = message
        self.severity = severity
        self.at = at
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        rule = try c.field(.rule, "")
        message = try c.field(.message, "")
        severity = try c.field(.severity, "")
        at = try c.field(.at, 0)
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, rule, message, severity, at }

    /// The resource browser's type of a well-known kind, to open the object; nil for another
    /// (shown as text) or a result without a name.
    public var resource: KubeAPIResource? {
        guard !name.isEmpty else { return nil }
        switch kind {
        case "Pod": return KubeAPIResource(resource: "pods", kind: kind)
        case "Service": return KubeAPIResource(resource: "services", kind: kind)
        case "ConfigMap": return KubeAPIResource(resource: "configmaps", kind: kind)
        case "ServiceAccount": return KubeAPIResource(resource: "serviceaccounts", kind: kind)
        case "Namespace": return KubeAPIResource(resource: "namespaces", kind: kind, namespaced: false)
        case "Deployment": return KubeAPIResource(group: "apps", resource: "deployments", kind: kind)
        case "StatefulSet": return KubeAPIResource(group: "apps", resource: "statefulsets", kind: kind)
        case "DaemonSet": return KubeAPIResource(group: "apps", resource: "daemonsets", kind: kind)
        case "ReplicaSet": return KubeAPIResource(group: "apps", resource: "replicasets", kind: kind)
        case "Job": return KubeAPIResource(group: "batch", resource: "jobs", kind: kind)
        case "CronJob": return KubeAPIResource(group: "batch", resource: "cronjobs", kind: kind)
        case "Ingress": return KubeAPIResource(group: "networking.k8s.io", resource: "ingresses", kind: kind)
        default: return nil
        }
    }

    /// The namespace the object screen takes: "" for a cluster-scoped kind.
    public var objectNamespace: String { resource?.namespaced == false ? "" : namespace }
}

/// One namespace's report results, for the namespace filter.
public struct PolicyNamespaceTotals: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let pass: Int
    public let fail: Int
    public let warn: Int
    public let errors: Int

    public var id: String { namespace }

    public init(namespace: String, pass: Int = 0, fail: Int = 0, warn: Int = 0, errors: Int = 0) {
        self.namespace = namespace
        self.pass = pass
        self.fail = fail
        self.warn = warn
        self.errors = errors
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        pass = try c.field(.pass, 0)
        fail = try c.field(.fail, 0)
        warn = try c.field(.warn, 0)
        errors = try c.field(.errors, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, pass, fail, warn
        case errors = "error"
    }
}
