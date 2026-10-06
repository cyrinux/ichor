import Foundation

/// The two calls every Go listener makes, decoded: a JSON item per update, then the end with
/// an error message ("" when the run went through). An item that does not decode is dropped.
public struct JSONSink<Item: Decodable>: Sendable {
    private let item: @Sendable (Item) -> Void
    private let done: @Sendable (String?) -> Void

    public init(item: @escaping @Sendable (Item) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.item = item
        self.done = done
    }

    public func emit(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(Item.self, from: json) else { return }
        item(decoded)
    }

    /// Ends the run: nil when `errMessage` is empty.
    public func finish(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
