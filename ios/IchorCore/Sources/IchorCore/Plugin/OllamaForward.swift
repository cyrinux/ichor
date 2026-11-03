import Foundation

/// The Go core's AI provider that asks the cluster's Ollama through a port-forward
/// (KubeOllamaForward). Its stored server URL is the server it points at, `OllamaServerRef.text`.
public let ollamaClusterProvider = "ollama-cluster"

/// The provider's name in the settings and the AI screens (a brand: not translated).
public let ollamaBrand = "Ollama"

/// An Ollama server of the open cluster: the Deployment or StatefulSet running it.
public struct OllamaServerRef: Equatable, Hashable, Sendable {
    public let namespace: String
    public let server: String

    public init(namespace: String, server: String) {
        self.namespace = namespace
        self.server = server
    }

    /// The stored form back; nil for anything else (an empty setting, a URL).
    public init?(stored: String) {
        let parts = stored.trimmingCharacters(in: .whitespacesAndNewlines)
            .split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 2, parts.allSatisfy({ !$0.isEmpty && !$0.contains(":") }) else { return nil }
        self.init(namespace: String(parts[0]), server: String(parts[1]))
    }

    /// As stored in the AI settings and shown: "namespace/server".
    public var text: String { "\(namespace)/\(server)" }
}

/// A running forward, as KubeOllamaForward and OllamaForwardState give it.
public struct OllamaForward: Decodable, Equatable, Sendable {
    public var baseUrl: String
    public var namespace: String
    public var server: String
    public var pod: String
    public var port: Int

    public init(baseUrl: String, namespace: String, server: String, pod: String, port: Int) {
        self.baseUrl = baseUrl
        self.namespace = namespace
        self.server = server
        self.pod = pod
        self.port = port
    }

    /// "namespace/pod", what the settings say the forward goes to.
    public var target: String { "\(namespace)/\(pod)" }

    /// The forward goes to `ref`'s server.
    public func isFor(_ ref: OllamaServerRef) -> Bool { namespace == ref.namespace && server == ref.server }
}
