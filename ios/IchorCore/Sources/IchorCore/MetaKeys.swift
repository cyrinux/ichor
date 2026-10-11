import Foundation

/// A key of a node's META partition (Go NodeMetaKeys): boot-time values Talos reads before the
/// machine config.
public struct MetaKey: Decodable, Equatable, Identifiable, Sendable {
    public let key: Int
    public let hex: String
    /// Empty for a key Talos does not define.
    public let name: String
    /// The text; the bytes in hex when `binary`; empty when `secret`.
    public let value: String
    public let size: Int
    public let binary: Bool
    public let secret: Bool
    /// The API accepts a write or delete of this key.
    public let writable: Bool

    public var id: Int { key }

    /// Only a writable text value can be edited as text.
    public var editable: Bool { writable && !binary && !secret }

    private enum CodingKeys: String, CodingKey {
        case key, hex, name, value, size, binary, secret, writable
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, 0)
        hex = try c.field(.hex, "")
        name = try c.field(.name, "")
        value = try c.field(.value, "")
        size = try c.field(.size, 0)
        binary = try c.field(.binary, false)
        secret = try c.field(.secret, false)
        writable = try c.field(.writable, false)
    }
}

public struct MetaKeyList: Decodable, Equatable, Sendable {
    public let keys: [MetaKey]
    /// The node has read its META partition.
    public let loaded: Bool
    /// The keys Talos defines, for the "add a key" picker.
    public let known: [MetaKey]

    private enum CodingKeys: String, CodingKey {
        case keys, loaded, known
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        keys = try c.field(.keys, [])
        loaded = try c.field(.loaded, false)
        known = try c.field(.known, [])
    }
}

/// A key typed in hex ("0x0a", "0A", "a"), or nil when it is not one from 0x00 to 0xff.
public func parseMetaKey(_ typed: String) -> Int? {
    var text = typed.trimmingCharacters(in: .whitespacesAndNewlines)
    if text.lowercased().hasPrefix("0x") { text = String(text.dropFirst(2)) }
    guard !text.isEmpty, text.count <= 2 else { return nil }
    return Int(text, radix: 16)
}

/// The key as Talos names it: "0x0a".
public func metaHex(_ key: Int) -> String {
    String(format: "0x%02x", key)
}
