import Foundation

// Mirrors go/ichorgo/etcdrecover.go (StartEtcdRecover) and snapshotdecrypt.go (SnapshotInspect).

/// What the user gives to open a snapshot.
public enum SnapshotOpener: Sendable {
    /// A clear file.
    case clear
    /// An age secret key (AGE-SECRET-KEY-1…).
    case secretKey
    case passphrase
    /// A YubiKey or SSH key: decrypt it on a laptop first.
    case unsupported
}

/// What a snapshot file needs before a recovery (Go SnapshotInspect).
public struct SnapshotInfo: Decodable, Equatable, Sendable {
    public let encrypted: Bool
    /// "x25519" (an age secret key), "scrypt" (a passphrase), "unknown", "" in clear.
    public let recipientsHint: String
    public let size: Int64
    public let sha256: String

    public var opener: SnapshotOpener {
        guard encrypted else { return .clear }
        switch recipientsHint {
        case "x25519": return .secretKey
        case "scrypt": return .passphrase
        default: return .unsupported
        }
    }

    public init(encrypted: Bool, recipientsHint: String = "", size: Int64 = 0, sha256: String = "") {
        self.encrypted = encrypted
        self.recipientsHint = recipientsHint
        self.size = size
        self.sha256 = sha256
    }

    private enum CodingKeys: String, CodingKey { case encrypted, recipientsHint, size, sha256 }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        encrypted = try c.field(.encrypted, false)
        recipientsHint = try c.field(.recipientsHint, "")
        size = try c.field(.size, 0)
        sha256 = try c.field(.sha256, "")
    }
}

/// One OnProgress event of a recovery: `bytes` of the file uploaded out of `total`.
public struct EtcdRecoverProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let message: String
    public let at: Int64
    public let bytes: Int64
    public let total: Int64

    public init(phase: String, message: String = "", at: Int64 = 0, bytes: Int64 = 0, total: Int64 = 0) {
        self.phase = phase
        self.message = message
        self.at = at
        self.bytes = bytes
        self.total = total
    }

    private enum CodingKeys: String, CodingKey { case phase, message, at, bytes, total }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
        bytes = try c.field(.bytes, 0)
        total = try c.field(.total, 0)
    }
}

/// A recovery's steps, in order.
public enum EtcdRecoverPhase: String, CaseIterable, Comparable, Sendable {
    case decrypting, uploading, bootstrapping, waiting

    public static func < (a: Self, b: Self) -> Bool {
        allCases.firstIndex(of: a)! < allCases.firstIndex(of: b)!
    }
}

/// Timeline of a recovery from the events so far; a clear file skips decrypting.
public func etcdRecoverTimeline(_ events: [EtcdRecoverProgress], encrypted: Bool, finished: Bool = false,
                                failure: String? = nil) -> [TimelineStep<EtcdRecoverPhase>] {
    let phases = EtcdRecoverPhase.allCases.filter { encrypted || $0 != .decrypting }
    return foldTimeline(events, phases: phases, finished: finished, failure: failure,
                        phase: { EtcdRecoverPhase(rawValue: $0.phase) }, at: \.at, message: \.message)
}

public extension EtcdOverview {
    /// etcd is lost: no member answered its status. Only then is a recovery offered (Go refuses
    /// it otherwise: it would split a live cluster).
    var etcdLost: Bool { !statuses.isEmpty && statuses.allSatisfy { $0.error != nil } }

    /// The control planes a recovery can run on: every node etcd was asked on.
    var recoverCandidates: [String] { statuses.map(\.node) }
}
