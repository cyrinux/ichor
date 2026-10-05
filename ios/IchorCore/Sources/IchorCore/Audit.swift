import Foundation

// Mirrors go/ichorgo/kube_audit*.go: who loads the Kubernetes API server and what they do
// wrong, from the control planes' audit logs, and the pure logic the screen words it with.

/// The kinds of AuditFinding, as Go names them.
public enum AuditKind: String, Sendable {
    case throttled, hotClient, listLoop, watchChurn, forbidden, missingAPI, missingObject, conflicts,
         alreadyExists, hotObject, eventSpam, slow, serverErrors
    case widespreadErrors, widespreadSlow, widespreadWatchChurn, staleLog, unauthorized
}

public enum AuditSeverity: String, Sendable {
    case critical, warning, info
}

/// What the audit logs of the control planes show over `seconds` (from..to, unix ms).
public struct AuditReport: Decodable, Equatable, Sendable {
    public let nodes: [AuditNodeRead]
    public let from: Int64
    public let to: Int64
    public let seconds: Double
    public let requests: Int
    public let findings: [AuditFinding]
    public let actors: [AuditActorRow]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        from = try c.field(.from, 0)
        to = try c.field(.to, 0)
        seconds = try c.field(.seconds, 0)
        requests = try c.field(.requests, 0)
        findings = try c.field(.findings, [])
        actors = try c.field(.actors, [])
    }

    private enum CodingKeys: String, CodingKey { case nodes, from, to, seconds, requests, findings, actors }

    /// Compressed bytes read from every control plane.
    public var bytesRead: Int64 { nodes.reduce(0) { $0 + $1.bytes } }
}

/// How reading one control plane's log went.
public struct AuditNodeRead: Decodable, Equatable, Sendable, Identifiable {
    public let node: String
    public let bytes: Int64
    public let events: Int
    public let last: Int64
    public let error: String

    public var id: String { node }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        bytes = try c.field(.bytes, 0)
        events = try c.field(.events, 0)
        last = try c.field(.last, 0)
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey { case node, bytes, events, last, error }
}

/// Who sent requests: a service account, a node, a control-plane component or a person.
public struct AuditActor: Decodable, Equatable, Sendable {
    public let user: String
    public let agent: String
    public let kind: String
    public let namespace: String
    public let name: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        user = try c.field(.user, "")
        agent = try c.field(.agent, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    private enum CodingKeys: String, CodingKey { case user, agent, kind, namespace, name }

    /// "monitoring/pod-exporter" for a service account, else the node, component or user.
    public var label: String {
        kind == "serviceAccount" ? "\(namespace)/\(name)" : (name.isEmpty ? user : name)
    }

    /// The program, when it says more than the name.
    public var agentDetail: String? { agent.isEmpty || agent == name ? nil : agent }
}

/// One problem with its evidence; `value` depends on the kind (see the Go auditFinding).
public struct AuditFinding: Decodable, Equatable, Sendable {
    public let kindName: String
    public let severity: AuditSeverity
    public let actor: AuditActor?
    public let count: Int
    public let rate: Double
    public let verb: String
    public let resource: String
    public let namespace: String
    public let name: String
    public let value: Double
    public let code: Int
    public let actors: Int
    public let objects: Int
    public let examples: [String]

    public var kind: AuditKind? { AuditKind(rawValue: kindName) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kindName = try c.field(.kind, "")
        severity = AuditSeverity(rawValue: try c.field(.severity, "")) ?? .info
        actor = try c.field(.actor, nil)
        count = try c.field(.count, 0)
        rate = try c.field(.rate, 0)
        verb = try c.field(.verb, "")
        resource = try c.field(.resource, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        value = try c.field(.value, 0)
        code = try c.field(.code, 0)
        actors = try c.field(.actors, 0)
        objects = try c.field(.objects, 0)
        examples = try c.field(.examples, [])
    }

    private enum CodingKeys: String, CodingKey {
        case kind, severity, actor, count, rate, verb, resource, namespace, name, value, code, actors, objects, examples
    }

    /// The finding is about the API server itself, not one client.
    public var aboutServer: Bool {
        switch kind {
        case .widespreadErrors, .widespreadSlow, .widespreadWatchChurn, .staleLog: true
        default: false
        }
    }

    /// "secrets tools/restic", "configmaps (media)" or the resource cluster-wide.
    public var target: String {
        switch (namespace.isEmpty, name.isEmpty) {
        case (false, false): "\(resource) \(namespace)/\(name)"
        case (true, false): "\(resource) \(name)"
        case (false, true): "\(resource) (\(namespace))"
        case (true, true): resource
        }
    }
}

public struct AuditActorRow: Decodable, Equatable, Sendable, Identifiable {
    public let actor: AuditActor
    public let requests: Int
    public let rate: Double
    public let share: Double
    public let errors: Int
    public let throttled: Int
    public let latencyMs: Double
    public let topVerb: String
    public let topResource: String
    public let topCount: Int

    public var id: String { "\(actor.user)/\(actor.agent)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        actor = try c.decode(AuditActor.self, forKey: .actor)
        requests = try c.field(.requests, 0)
        rate = try c.field(.rate, 0)
        share = try c.field(.share, 0)
        errors = try c.field(.errors, 0)
        throttled = try c.field(.throttled, 0)
        latencyMs = try c.field(.latencyMs, 0)
        topVerb = try c.field(.topVerb, "")
        topResource = try c.field(.topResource, "")
        topCount = try c.field(.topCount, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case actor, requests, rate, share, errors, throttled, latencyMs, topVerb, topResource, topCount
    }
}

/// An interval or age in seconds as "0.5 s", "26 s", "4 min" or "3.2 h".
public func formatSeconds(_ seconds: Double) -> String {
    switch seconds {
    case ..<10: String(format: "%.1f s", seconds)
    case ..<120: String(format: "%.0f s", seconds)
    case ..<7200: String(format: "%.0f min", seconds / 60)
    default: String(format: "%.1f h", seconds / 3600)
    }
}

/// Compressed bytes read, as "6.4 MB".
public func formatMegabytes(_ bytes: Int64) -> String {
    String(format: "%.1f MB", Double(bytes) / 1_000_000)
}
