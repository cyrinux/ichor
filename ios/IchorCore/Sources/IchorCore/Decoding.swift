import Foundation

extension KeyedDecodingContainer {
    /// A field the Go core may leave out (older cores, empty values): its default then.
    func field<T: Decodable>(_ key: Key, _ fallback: T) throws -> T {
        try decodeIfPresent(T.self, forKey: key) ?? fallback
    }
}
