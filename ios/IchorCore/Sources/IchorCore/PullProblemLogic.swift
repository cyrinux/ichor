import Foundation

// Mirrors go/ichorgo/kube_image_pull.go: why a pod's image cannot be pulled. The core only
// classifies; the app words each case below and shows the kubelet's message as is.

/// The kind of pull problem; any kind a newer core sends reads as `unknown`.
public enum PullKind: String, Codable, CaseIterable, Sendable, WireEnum {
    case notFound, unauthorized, rateLimited, tls, network, platform, invalidName, unknown

    public static let wireFallback = PullKind.unknown
}

/// Why a pod's image cannot be pulled, with what its fix needs.
public struct PullProblem: Codable, Equatable, Hashable, Sendable {
    public let kind: PullKind
    public let image: String
    /// The image's registry host, docker.io when the reference names none.
    public let registry: String
    /// The pod's imagePullSecrets.
    public let secrets: [String]

    public init(kind: PullKind, image: String = "", registry: String = "", secrets: [String] = []) {
        self.kind = kind
        self.image = image
        self.registry = registry
        self.secrets = secrets
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.wire(.kind)
        image = try c.field(.image, "")
        registry = try c.field(.registry, "")
        secrets = try c.field(.secrets, [])
    }

    private enum CodingKeys: String, CodingKey { case kind, image, registry, secrets }

    /// The registry to name: the image itself when the core sent no host.
    private var host: String { registry.isEmpty ? image : registry }

    /// The one line the pod row shows.
    public var title: PullTitle {
        switch kind {
        case .notFound: .notFound(registry: host)
        case .unauthorized: .unauthorized(registry: host)
        case .rateLimited: .rateLimited(registry: host)
        case .tls: .tls(registry: host)
        case .network: .network(registry: host)
        case .platform: .platform
        case .invalidName: .invalidName
        case .unknown: .unknown(registry: host)
        }
    }

    /// What to check, under the kubelet's message.
    public var hint: PullHint {
        switch kind {
        case .notFound: .notFound
        case .unauthorized: secrets.isEmpty ? .noSecret : .secrets(secrets.joined(separator: ", "))
        case .rateLimited: .rateLimited
        case .tls: .tls
        case .network: .network
        case .platform: .platform
        case .invalidName: .invalidName
        case .unknown: .unknown
        }
    }

    /// The screen that helps: the registry mirror snippet, or the node's network tools.
    public var action: PullAction? {
        switch kind {
        case .rateLimited: .registryMirror(host: registry)
        case .network: .netTools
        default: nil
        }
    }
}

/// The pod row's line about a pull problem; the app maps each case to its sentence.
public enum PullTitle: Equatable, Sendable {
    case notFound(registry: String)
    case unauthorized(registry: String)
    case rateLimited(registry: String)
    case tls(registry: String)
    case network(registry: String)
    case platform
    case invalidName
    case unknown(registry: String)
}

/// The hint under a pull problem's message.
public enum PullHint: Equatable, Sendable {
    case notFound
    /// The pod's pull secrets, joined with ", ".
    case secrets(String)
    case noSecret
    case rateLimited
    case tls
    case network
    case platform
    case invalidName
    case unknown
}

/// A screen a pull problem links to.
public enum PullAction: Equatable, Sendable {
    /// The registry_mirror snippet, its host prefilled.
    case registryMirror(host: String)
    /// The network tools of the pod's node.
    case netTools
}

/// The pull problem kind of a checkup podImagePull finding, from its `reason`: "" or any
/// other text reads as unknown.
public func checkupPullKind(reason: String) -> PullKind {
    PullKind(wire: reason)
}
