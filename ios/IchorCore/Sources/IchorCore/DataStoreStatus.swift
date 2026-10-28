import Foundation

/// Whether the core saves the action audit log and the node names across launches (Go's
/// DataStoreStatus): the last element of its directory, and why not when it does not.
public struct DataStoreStatus: Decodable, Equatable, Sendable {
    public let persisting: Bool
    public let dir: String
    public let reason: String

    public init(persisting: Bool, dir: String = "", reason: String = "") {
        self.persisting = persisting
        self.dir = dir
        self.reason = reason
    }

    private enum CodingKeys: String, CodingKey { case persisting, dir, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        persisting = try c.decodeIfPresent(Bool.self, forKey: .persisting) ?? false
        dir = try c.decodeIfPresent(String.self, forKey: .dir) ?? ""
        reason = try c.decodeIfPresent(String.self, forKey: .reason) ?? ""
    }

    /// The reason to show in Settings → Support; nil while the store is saving.
    public var problem: String? {
        guard !persisting else { return nil }
        return reason.isEmpty ? "no data directory" : reason
    }
}
