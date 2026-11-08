import Foundation

/// The images report of an inventory, from the Go core (ImageReport): images not pinned,
/// repositories running several tags, the registries pulled from, and (Talos) the images a node
/// keeps that none of its containers runs.
public struct ImageReport: Decodable, Equatable, Sendable {
    public let unpinned: [ReportUnpinned]
    public let multiTag: [ReportRepo]
    /// Most images first.
    public let registries: [ReportRegistry]
    public let unusedOnNode: [ReportUnused]
    public let summary: ReportSummary

    public init(
        unpinned: [ReportUnpinned] = [], multiTag: [ReportRepo] = [], registries: [ReportRegistry] = [],
        unusedOnNode: [ReportUnused] = [], summary: ReportSummary = ReportSummary()
    ) {
        self.unpinned = unpinned
        self.multiTag = multiTag
        self.registries = registries
        self.unusedOnNode = unusedOnNode
        self.summary = summary
    }

    private enum CodingKeys: String, CodingKey { case unpinned, multiTag, registries, unusedOnNode, summary }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        unpinned = try c.field(.unpinned, [])
        multiTag = try c.field(.multiTag, [])
        registries = try c.field(.registries, [])
        unusedOnNode = try c.field(.unusedOnNode, [])
        summary = try c.field(.summary, ReportSummary())
    }

    /// Bytes the unused images take, all nodes together.
    public var unusedBytes: Int64 { unusedOnNode.reduce(Int64(0)) { $0 &+ $1.sizeBytes } }
}

/// An app of the inventory (`id` opens its sheet), with a namespace it runs in.
public struct ReportAppRef: Decodable, Equatable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let namespace: String

    public init(id: String, name: String = "", namespace: String = "") {
        self.id = id
        self.name = name
        self.namespace = namespace
    }

    private enum CodingKeys: String, CodingKey { case id, name, namespace }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        name = try c.field(.name, "")
        namespace = try c.field(.namespace, "")
    }
}

/// An image run as :latest or untagged, without a digest.
public struct ReportUnpinned: Decodable, Equatable, Identifiable, Sendable {
    public let image: String
    public let apps: [ReportAppRef]
    /// Node addresses.
    public let nodes: [String]

    public var id: String { image }

    public init(image: String, apps: [ReportAppRef] = [], nodes: [String] = []) {
        self.image = image
        self.apps = apps
        self.nodes = nodes
    }

    private enum CodingKeys: String, CodingKey { case image, apps, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        image = try c.field(.image, "")
        apps = try c.field(.apps, [])
        nodes = try c.field(.nodes, [])
    }
}

/// A repository running with several tags (or digests) at once.
public struct ReportRepo: Decodable, Equatable, Identifiable, Sendable {
    public let repository: String
    public let tags: [ReportTag]

    public var id: String { repository }

    public init(repository: String, tags: [ReportTag] = []) {
        self.repository = repository
        self.tags = tags
    }

    private enum CodingKeys: String, CodingKey { case repository, tags }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repository = try c.field(.repository, "")
        tags = try c.field(.tags, [])
    }
}

public struct ReportTag: Decodable, Equatable, Identifiable, Sendable {
    /// The tag, "@sha256:…" for a digest alone, "latest" for neither.
    public let tag: String
    public let nodes: [String]
    public let apps: [ReportAppRef]

    public var id: String { tag }

    public init(tag: String, nodes: [String] = [], apps: [ReportAppRef] = []) {
        self.tag = tag
        self.nodes = nodes
        self.apps = apps
    }

    private enum CodingKeys: String, CodingKey { case tag, nodes, apps }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tag = try c.field(.tag, "")
        nodes = try c.field(.nodes, [])
        apps = try c.field(.apps, [])
    }
}

public struct ReportRegistry: Decodable, Equatable, Identifiable, Sendable {
    public let host: String
    /// Distinct images, and the apps running them.
    public let images: Int
    public let apps: Int
    /// Docker Hub: anonymous pulls are rate limited.
    public let dockerHub: Bool

    public var id: String { host }

    public init(host: String, images: Int = 0, apps: Int = 0, dockerHub: Bool = false) {
        self.host = host
        self.images = images
        self.apps = apps
        self.dockerHub = dockerHub
    }

    private enum CodingKeys: String, CodingKey { case host, images, apps, dockerHub }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.field(.host, "")
        images = try c.field(.images, 0)
        apps = try c.field(.apps, 0)
        dockerHub = try c.field(.dockerHub, false)
    }
}

/// An image in `node`'s CRI store that none of its containers runs.
public struct ReportUnused: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let image: String
    public let sizeBytes: Int64

    public var id: String { "\(node)|\(image)" }

    public init(node: String, image: String, sizeBytes: Int64 = 0) {
        self.node = node
        self.image = image
        self.sizeBytes = sizeBytes
    }

    private enum CodingKeys: String, CodingKey { case node, image, sizeBytes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        image = try c.field(.image, "")
        sizeBytes = try c.field(.sizeBytes, 0)
    }
}

public struct ReportSummary: Decodable, Equatable, Sendable {
    public let images: Int
    public let unpinned: Int
    public let multiTag: Int
    /// Distinct images pulled from Docker Hub.
    public let dockerHub: Int
    public let unused: Int

    public init(images: Int = 0, unpinned: Int = 0, multiTag: Int = 0, dockerHub: Int = 0, unused: Int = 0) {
        self.images = images
        self.unpinned = unpinned
        self.multiTag = multiTag
        self.dockerHub = dockerHub
        self.unused = unused
    }

    private enum CodingKeys: String, CodingKey { case images, unpinned, multiTag, dockerHub, unused }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        images = try c.field(.images, 0)
        unpinned = try c.field(.unpinned, 0)
        multiTag = try c.field(.multiTag, 0)
        dockerHub = try c.field(.dockerHub, 0)
        unused = try c.field(.unused, 0)
    }
}

extension ClusterInventory {
    /// The nodes whose CRI images the report compares with the containers: every node running a pod.
    public var reportNodes: [String] {
        Array(Set(apps.flatMap(\.nodes).filter { !$0.isEmpty })).sorted()
    }

    /// What ImageReport reads of the inventory, as JSON: the apps with their namespaces, images
    /// and pods (the model keeps no raw JSON, and Go needs nothing else).
    public func imageReportInput() throws -> String {
        let wire = WireInventory(apps: apps.map(WireApp.init))
        return String(decoding: try JSONEncoder().encode(wire), as: UTF8.self)
    }
}

/// [node: NodeImages] as ImageReportWithNodeImages reads it.
public func imageReportNodeImages(_ byNode: [String: [ContainerImage]]) throws -> String {
    let wire = byNode.mapValues { $0.map { WireNodeImage(name: $0.name, digest: $0.digest, size: $0.size) } }
    return String(decoding: try JSONEncoder().encode(wire), as: UTF8.self)
}

private struct WireInventory: Encodable {
    let apps: [WireApp]
}

private struct WireApp: Encodable {
    let id: String
    let name: String
    let namespaces: [String]
    let nodes: [String]
    let images: [WireImage]
    let pods: [WirePod]

    init(_ app: InventoryApp) {
        id = app.id
        name = app.name
        namespaces = app.namespaces
        nodes = app.nodes
        images = app.images.map { WireImage(repo: $0.repo, tag: $0.tag, digest: $0.digest) }
        pods = app.pods.map { pod in
            WirePod(
                namespace: pod.namespace, pod: pod.pod, node: pod.node,
                containers: pod.containers.map { WireContainer(name: $0.name, image: $0.image) })
        }
    }
}

private struct WireImage: Encodable {
    let repo: String
    let tag: String
    let digest: String
}

private struct WirePod: Encodable {
    let namespace: String
    let pod: String
    let node: String
    let containers: [WireContainer]
}

private struct WireContainer: Encodable {
    let name: String
    let image: String
}

private struct WireNodeImage: Encodable {
    let name: String
    let digest: String
    let size: Int64
}
