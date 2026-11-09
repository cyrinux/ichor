/// A pod named as "namespace/name", as an image scan report lists the pods running an image.
public struct PodRef: Hashable, Sendable {
    public let namespace: String
    public let name: String

    public init(namespace: String, name: String) {
        self.namespace = namespace
        self.name = name
    }

    /// Splits `path` on its first "/"; nil when a side is empty or the name holds another "/"
    /// (no pod is named so): such a value stays plain text rather than a link to nothing.
    public init?(path: String) {
        guard let slash = path.firstIndex(of: "/") else { return nil }
        let namespace = String(path[..<slash])
        let name = String(path[path.index(after: slash)...])
        guard !namespace.isEmpty, !name.isEmpty, !name.contains("/") else { return nil }
        self.init(namespace: namespace, name: name)
    }
}
