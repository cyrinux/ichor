import Foundation

// `kubectl describe` for any kind, CRDs included. Mirrors go/ichorgo/kube_summary.go; same
// rules as Android's model/KubeObjectSummary.kt.

/// The tone Go names ("ok", "warn", "bad"), neutral for anything else.
public func summaryTone(_ tone: String) -> KubeTone {
    switch tone {
    case "ok": .good
    case "warn": .warn
    case "bad": .bad
    default: .neutral
    }
}

/// One object summed up (KubeObjectSummary). Times are Unix milliseconds.
public struct KubeObjectSummary: Decodable, Equatable, Sendable {
    public let kind: String
    public let apiVersion: String
    public let namespace: String
    public let name: String
    /// "ok", "warn", "bad" or "none": the worst condition's tone, or the phase's.
    public let health: String
    /// What the worst condition says ("Ready: ContainersNotReady").
    public let healthReason: String
    public let phase: String
    public let conditions: [SummaryCondition]
    public let owners: [ObjectOwner]
    public let labels: [String: String]
    public let annotations: [String: String]
    public let created: Int64
    /// When its deletion was asked, 0 when none is pending.
    public let deleting: Int64
    public let finalizers: [String]
    public let highlights: [SpecHighlight]
    public let events: [KubeEvent]
    /// Why the events could not be read, "" when they were.
    public let eventsError: String

    public var healthTone: KubeTone { summaryTone(health) }

    private enum CodingKeys: String, CodingKey {
        case kind, apiVersion, namespace, name, health, healthReason, phase, conditions, owners, labels, annotations
        case created, deleting, finalizers, highlights, events, eventsError
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        apiVersion = try c.field(.apiVersion, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        health = try c.field(.health, "none")
        healthReason = try c.field(.healthReason, "")
        phase = try c.field(.phase, "")
        conditions = try c.field(.conditions, [])
        owners = try c.field(.owners, [])
        labels = try c.field(.labels, [:])
        annotations = try c.field(.annotations, [:])
        created = try c.field(.created, 0)
        deleting = try c.field(.deleting, 0)
        finalizers = try c.field(.finalizers, [])
        highlights = try c.field(.highlights, [])
        events = try c.field(.events, [])
        eventsError = try c.field(.eventsError, "")
    }
}

/// An entry of status.conditions, with the tone it reads as.
public struct SummaryCondition: Decodable, Equatable, Sendable {
    public let type: String
    public let status: String
    public let reason: String
    public let message: String
    public let lastTransition: Int64
    public let tone: String

    public var toneValue: KubeTone { summaryTone(tone) }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message, lastTransition, tone }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        lastTransition = try c.field(.lastTransition, 0)
        tone = try c.field(.tone, "none")
    }
}

/// An object up the chain: an ownerReference (`via` "owner"), or the Flux object, Argo CD
/// Application or Helm release that manages it ("flux", "argocd", "helm").
public struct ObjectOwner: Decodable, Equatable, Hashable, Sendable {
    public static let viaOwner = "owner"
    public static let viaFlux = "flux"
    public static let viaArgo = "argocd"
    public static let viaHelm = "helm"

    public let via: String
    public let group: String
    public let version: String
    public let resource: String
    public let kind: String
    public let namespace: String
    public let name: String
    public let namespaced: Bool
    public let verbs: [String]
    public let controller: Bool

    public var isHelmRelease: Bool { via == Self.viaHelm }

    /// The browser's resource for it, nil when discovery did not know its kind (or for a
    /// Helm release, which has a screen of its own).
    public var apiResource: KubeAPIResource? {
        guard !resource.isEmpty, !version.isEmpty, !name.isEmpty else { return nil }
        return KubeAPIResource(group: group, version: version, resource: resource, kind: kind, namespaced: namespaced, verbs: verbs)
    }

    private enum CodingKeys: String, CodingKey { case via, group, version, resource, kind, namespace, name, namespaced, verbs, controller }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        via = try c.field(.via, "")
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        namespaced = try c.field(.namespaced, false)
        verbs = try c.field(.verbs, [])
        controller = try c.field(.controller, false)
    }
}

/// A spec field shown first; `key` names it (replicas, selector, image, node, suspended, schedule).
public struct SpecHighlight: Decodable, Equatable, Sendable {
    public let key: String
    public let value: String

    private enum CodingKeys: String, CodingKey { case key, value }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        value = try c.field(.value, "")
    }
}
