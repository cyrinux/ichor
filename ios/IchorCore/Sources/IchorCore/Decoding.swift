import Foundation

/// A value the Go core sends as a string; what this version does not know maps to a fallback.
public protocol WireDecodable {
    init(wire: String)
}

/// A string enum of the Go core: unknown strings (a newer core) read as `wireFallback`.
public protocol WireEnum: WireDecodable, RawRepresentable where RawValue == String {
    static var wireFallback: Self { get }
}

public extension WireEnum {
    init(wire: String) { self = Self(rawValue: wire) ?? Self.wireFallback }
}

extension KeyedDecodingContainer {
    /// A field the Go core may leave out (older cores, empty values): its default then.
    func field<T: Decodable>(_ key: Key, _ fallback: T) throws -> T {
        try decodeIfPresent(T.self, forKey: key) ?? fallback
    }

    /// A wire string as its enum, the fallback when absent or unknown.
    func wire<T: WireDecodable>(_ key: Key) throws -> T {
        T(wire: try field(key, ""))
    }

    /// Wire strings as enums, dropping the unknown ones (reasons a newer core names).
    func wireList<T: RawRepresentable>(_ key: Key) throws -> [T] where T.RawValue == String {
        (try field(key, [String]())).compactMap(T.init(rawValue:))
    }
}
