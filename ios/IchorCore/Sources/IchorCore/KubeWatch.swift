import Foundation

// A list the Go core keeps live (StartKubeWatch, KubeWatchListener): the whole list at the
// start and whenever the watch started over, else one item to merge into it. Same rules as
// Android's model/KubeWatch.kt.

/// How long a screen waits before following again a watch that ended (a refusal, the network).
public let kubeWatchRetrySeconds = 30

/// One change of a watched list.
public enum KubeWatchEvent<T: Sendable>: Sendable {
    case sync([T])
    case added(T)
    case modified(T)
    case deleted(T)
}

extension KubeWatchEvent where T: Decodable {
    /// What the listener got as `eventType` and `json`: an item decoded as `T`, a SYNC list
    /// with `list`; nil for an unknown type or JSON the models cannot read.
    public static func decode(_ eventType: String, json: String, list: (String) throws -> [T]) -> KubeWatchEvent<T>? {
        switch eventType {
        case "SYNC": return (try? list(json)).map { .sync($0) }
        case "ADDED": return (try? TalosJSON.decode(T.self, from: json)).map { .added($0) }
        case "MODIFIED": return (try? TalosJSON.decode(T.self, from: json)).map { .modified($0) }
        case "DELETED": return (try? TalosJSON.decode(T.self, from: json)).map { .deleted($0) }
        default: return nil
        }
    }
}

extension KubeWatchEvent: Equatable where T: Equatable {}

extension Array where Element: Sendable {
    /// This list with `event` applied, rows told apart by `key`: a changed row keeps its place,
    /// a new one goes last (the API server's order ends there), a deleted one leaves.
    public func applying(_ event: KubeWatchEvent<Element>, key: (Element) -> String) -> [Element] {
        switch event {
        case .sync(let items):
            return items
        case .added(let item), .modified(let item):
            return upserting(item, key: key)
        case .deleted(let item):
            let gone = key(item)
            return filter { key($0) != gone }
        }
    }

    private func upserting(_ item: Element, key: (Element) -> String) -> [Element] {
        guard let at = firstIndex(where: { key($0) == key(item) }) else { return self + [item] }
        var rows = self
        rows[at] = item
        return rows
    }
}

extension PagedLoad where T: Sendable {
    /// This load with `event` applied: a SYNC is the whole list, complete and as full objects
    /// (nothing left to page); a single change keeps the pages as they are.
    public func applying(_ event: KubeWatchEvent<T>, key: (T) -> String) -> PagedLoad<T> {
        if case .sync(let items) = event { return .complete(items) }
        return replacing(items: items.applying(event, key: key))
    }
}
