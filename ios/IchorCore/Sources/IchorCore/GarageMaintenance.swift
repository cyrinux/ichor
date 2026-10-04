import Foundation

private extension KeyedDecodingContainer {
    /// A field the Go core may leave out (older cores, empty values): its default then.
    func field<T: Decodable>(_ key: Key, _ fallback: T) throws -> T {
        try decodeIfPresent(T.self, forKey: key) ?? fallback
    }
}

/// Resync tranquility of a Garage node: 0 resyncs at full speed, 2 is Garage's default.
public enum GarageTranquility {
    public static let fullSpeed: Int64 = 0
    public static let standard: Int64 = 2
}

/// What the blocks failing to resync in a Garage cluster are (KubeGarageBlockErrors): the
/// objects that reference them, live or deleted, and the repairs they need. Only a sample of
/// the errored blocks is looked up, so detailed <= errored.
public struct GarageBlockReport: Decodable, Equatable, Sendable {
    public let errored: Int
    public let detailed: Int
    public let live: Int
    public let cleanupOnly: Int
    public let staleRefs: Int
    public let refcountMismatches: Int
    public let retryable: Int
    /// A block-refs or block-rc repair is busy.
    public let repairsRunning: Bool
    public let nodes: [GarageBlockNode]

    public enum Verdict: Equatable, Sendable {
        /// No block is failing to resync.
        case clean
        /// Failing blocks back live objects: check them through S3 before declaring data loss.
        case liveAffected
        /// Only deleted data is held by the failing blocks.
        case deletedOnly
    }

    public var verdict: Verdict {
        if errored == 0 { return .clean }
        return live > 0 ? .liveAffected : .deletedOnly
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        errored = try c.field(.errored, 0)
        detailed = try c.field(.detailed, 0)
        live = try c.field(.live, 0)
        cleanupOnly = try c.field(.cleanupOnly, 0)
        staleRefs = try c.field(.staleRefs, 0)
        refcountMismatches = try c.field(.refcountMismatches, 0)
        retryable = try c.field(.retryable, 0)
        repairsRunning = try c.field(.repairsRunning, false)
        nodes = try c.field(.nodes, [])
    }

    private enum CodingKeys: String, CodingKey {
        case errored, detailed, live, cleanupOnly, staleRefs, refcountMismatches, retryable, repairsRunning, nodes
    }
}

public struct GarageBlockNode: Decodable, Equatable, Identifiable, Sendable {
    public let nodeID: String
    public let hostname: String
    public let errored: Int
    /// Why the node could not be read, "" when it answered.
    public let error: String
    public let blocks: [GarageBlock]

    public var id: String { nodeID.isEmpty ? hostname : nodeID }
    public var label: String { hostname.isEmpty ? String(nodeID.prefix(16)) : hostname }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodeID = try c.field(.id, "")
        hostname = try c.field(.hostname, "")
        errored = try c.field(.errored, 0)
        error = try c.field(.error, "")
        blocks = try c.field(.blocks, [])
    }

    private enum CodingKeys: String, CodingKey { case id, hostname, errored, error, blocks }
}

public enum GarageBlockImpact: String, Sendable {
    /// A live object or upload references the block.
    case live
    /// Only a reference to deleted metadata keeps it.
    case staleRef = "stale-ref"
    /// Deleted data awaiting garbage collection.
    case cleanup
    /// Not looked up, or the lookup failed.
    case unknown
}

public struct GarageBlock: Decodable, Equatable, Identifiable, Sendable {
    public let hash: String
    public let refcount: Int64
    /// Failed resync attempts.
    public let errors: Int64
    public let lastTrySecs: Int64
    public let nextTrySecs: Int64
    public let impact: GarageBlockImpact
    public let staleRef: Bool
    public let refcountMismatch: Bool
    /// Why the lookup failed, "" when it worked.
    public let error: String
    public let refs: [GarageBlockRef]

    public var id: String { hash }
    public var shortHash: String { String(hash.prefix(12)) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hash = try c.field(.hash, "")
        refcount = try c.field(.refcount, 0)
        errors = try c.field(.errors, 0)
        lastTrySecs = try c.field(.lastTrySecs, -1)
        nextTrySecs = try c.field(.nextTrySecs, -1)
        impact = GarageBlockImpact(rawValue: try c.field(.impact, "")) ?? .unknown
        staleRef = try c.field(.staleRef, false)
        refcountMismatch = try c.field(.refcountMismatch, false)
        error = try c.field(.error, "")
        refs = try c.field(.refs, [])
    }

    private enum CodingKeys: String, CodingKey {
        case hash, refcount, errors, lastTrySecs, nextTrySecs, impact, staleRef, refcountMismatch, error, refs
    }
}

public struct GarageBlockRef: Decodable, Equatable, Sendable {
    /// object, upload or version.
    public let kind: String
    public let bucket: String
    public let key: String
    public let uploadID: String
    public let version: String
    public let live: Bool

    /// bucket/key, or what is known of it.
    public var label: String {
        if !bucket.isEmpty && !key.isEmpty { return "\(bucket)/\(key)" }
        if !key.isEmpty { return key }
        if !version.isEmpty { return String(version.prefix(16)) }
        return String(uploadID.prefix(16))
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        bucket = try c.field(.bucket, "")
        key = try c.field(.key, "")
        uploadID = try c.field(.uploadId, "")
        version = try c.field(.version, "")
        live = try c.field(.live, false)
    }

    private enum CodingKeys: String, CodingKey { case kind, bucket, key, uploadId, version, live }
}

/// What KubeGarageRepairBlocks did; the metadata repairs then run asynchronously.
public struct GarageRepairResult: Decodable, Equatable, Sendable {
    /// block-refs repair launched on every node.
    public let blockRefs: Bool
    /// block-rc repair launched on every node.
    public let blockRc: Bool
    /// One was already running: not launched again.
    public let repairsRunning: Bool
    /// A node did not answer: no repair launched.
    public let unreachable: Bool
    /// Resyncs rescheduled.
    public let retried: Int64
    public let errors: [String]

    public enum Outcome: Equatable, Sendable {
        /// The metadata repairs launched, by name (block-refs, block-rc).
        case launched([String])
        case alreadyRunning
        case unreachable
        case retried(Int64)
        case error(String)
        case nothingToDo
    }

    /// Each thing the repair did or could not do, in the order the summary shows them.
    public var outcomes: [Outcome] {
        var out: [Outcome] = []
        let launched = [blockRefs ? "block-refs" : nil, blockRc ? "block-rc" : nil].compactMap { $0 }
        if !launched.isEmpty { out.append(.launched(launched)) }
        if repairsRunning { out.append(.alreadyRunning) }
        if unreachable { out.append(.unreachable) }
        if retried > 0 { out.append(.retried(retried)) }
        out.append(contentsOf: errors.filter { !$0.isEmpty }.map { .error($0) })
        return out.isEmpty ? [.nothingToDo] : out
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        blockRefs = try c.field(.blockRefs, false)
        blockRc = try c.field(.blockRc, false)
        repairsRunning = try c.field(.repairsRunning, false)
        unreachable = try c.field(.unreachable, false)
        retried = try c.field(.retried, 0)
        errors = try c.field(.errors, [])
    }

    private enum CodingKeys: String, CodingKey { case blockRefs, blockRc, repairsRunning, unreachable, retried, errors }
}
