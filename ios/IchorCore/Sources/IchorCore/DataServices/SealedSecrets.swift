import Foundation

// Data services: Sealed Secrets, its controller and sealing keys (see DataServices.swift). Names
// and status only: the Go reader never decodes an encrypted value or a sealing key's data.

public struct SealedSecretsStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// The worst of the items, the controller and the sealing keys.
    public let health: ServiceHealth
    /// Section-level reasons: the controller and the keys. Known ones only.
    public let reasons: [SealedSecretsReason]
    /// nil when no Deployment runs the controller's image.
    public let controller: SealedSecretsController?
    /// Problems first.
    public let items: [SealedSecretItem]
    public let truncated: Bool
    /// All of them; `items` is capped at 500.
    public let secretCount: Int
    /// Newest first.
    public let keys: [SealingKey]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        controller = try c.decodeIfPresent(SealedSecretsController.self, forKey: .controller)
        items = try c.field(.items, [])
        truncated = try c.field(.truncated, false)
        secretCount = try c.field(.secretCount, 0)
        keys = try c.field(.keys, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, health, reasons, controller, items, truncated, secretCount, keys }
}

public struct SealedSecretsController: Decodable, Equatable, Sendable {
    public let namespace: String
    public let name: String
    public let ready: Bool
    public let replicas: Int
    public let readyReplicas: Int
    public let health: ServiceHealth
    public let reasons: [SealedSecretsReason]

    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        ready = try c.field(.ready, false)
        replicas = try c.field(.replicas, 0)
        readyReplicas = try c.field(.readyReplicas, 0)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, ready, replicas, readyReplicas, health, reasons }
}

public struct SealedSecretItem: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// The data keys of the Secret it unseals into, never their values.
    public let keys: [String]
    public let keyCount: Int
    public let type: String
    public let immutable: Bool
    /// strict, namespace-wide or cluster-wide.
    public let scope: String
    public let synced: Bool
    /// The Synced condition's reason and message.
    public let reason: String
    public let message: String
    public let lastTransitionTime: Int64
    public let observedGeneration: Int64
    public let stale: Bool
    /// Creation time, unix ms (the Go field is named "age").
    public let createdAt: Int64
    public let health: ServiceHealth
    public let reasons: [SealedSecretsReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        keys = try c.field(.keys, [])
        keyCount = try c.field(.keyCount, 0)
        type = try c.field(.type, "")
        immutable = try c.field(.immutable, false)
        scope = try c.field(.scope, "")
        synced = try c.field(.synced, false)
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        lastTransitionTime = try c.field(.lastTransitionTime, 0)
        observedGeneration = try c.field(.observedGeneration, 0)
        stale = try c.field(.stale, false)
        createdAt = try c.field(.age, 0)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, keys, keyCount, type, immutable, scope, synced, reason, message
        case lastTransitionTime, observedGeneration, stale, age, health, reasons
    }
}

public struct SealingKey: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// active or compromised, as the key's label says.
    public let state: String
    /// Unix ms, 0 when unknown.
    public let createdAt: Int64
    /// Elapsed ms.
    public let age: Int64

    public var id: String { "\(namespace)/\(name)" }
    public var active: Bool { state == "active" }
    public var compromised: Bool { state == "compromised" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        state = try c.field(.state, "")
        createdAt = try c.field(.createdAt, 0)
        age = try c.field(.age, 0)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, state, createdAt, age }
}

/// Why a SealedSecret, the controller or the sealing keys are not ok, as the Go core names it.
public enum SealedSecretsReason: String, Sendable {
    case notSynced, unmanaged, stale, controllerDown, noKeys, keyCompromised, keyStale

    /// Reasons that stop secrets from being delivered; the others are warnings.
    public var critical: Bool { self == .notSynced || self == .controllerDown || self == .noKeys }
}
