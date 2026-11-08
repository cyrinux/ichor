import Foundation

/// What the model named in a request, as Go reports it: the placeholders it used.
public struct AIToolArgs: Decodable, Equatable, Sendable {
    public var namespace: String
    public var pod: String
    public var kind: String
    public var name: String
    public var node: String
    public var service: String

    public init(
        namespace: String = "", pod: String = "", kind: String = "", name: String = "", node: String = "",
        service: String = ""
    ) {
        self.namespace = namespace
        self.pod = pod
        self.kind = kind
        self.name = name
        self.node = node
        self.service = service
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.decodeIfPresent(String.self, forKey: .namespace) ?? ""
        pod = try c.decodeIfPresent(String.self, forKey: .pod) ?? ""
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        node = try c.decodeIfPresent(String.self, forKey: .node) ?? ""
        service = try c.decodeIfPresent(String.self, forKey: .service) ?? ""
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, pod, kind, name, node, service
    }

    /// The arguments of Go's OnTool; empty when they cannot be read.
    public init(json: String) {
        self = (try? JSONDecoder().decode(Self.self, from: Data(json.utf8))) ?? Self()
    }
}

/// One request of the model and, once `done`, what went back: `result` is the exact text sent.
public struct AIToolCall: Equatable, Identifiable, Sendable {
    public let id: Int
    public let tool: String
    public let args: AIToolArgs
    public var done = false
    public var bytes = 0
    public var result = ""
    /// Why nothing was read.
    public var error: String?

    public init(id: Int, tool: String, args: AIToolArgs) {
        self.id = id
        self.tool = tool
        self.args = args
    }
}

extension Array where Element == AIToolCall {
    /// A new list with the request appended.
    public func started(tool: String, argsJSON: String) -> [AIToolCall] {
        self + [AIToolCall(id: count, tool: tool, args: AIToolArgs(json: argsJSON))]
    }

    /// A new list with the oldest request of that tool still open closed.
    public func finished(tool: String, bytes: Int, result: String, error: String?) -> [AIToolCall] {
        guard let index = firstIndex(where: { !$0.done && $0.tool == tool }) else { return self }
        var copy = self
        copy[index].done = true
        copy[index].bytes = bytes
        copy[index].result = result
        copy[index].error = error
        return copy
    }
}
