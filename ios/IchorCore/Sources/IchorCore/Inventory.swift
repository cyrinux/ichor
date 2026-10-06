import Foundation

/// The applications running in the cluster, from the Go core (ClusterInventory): every node's
/// Kubernetes containers grouped into apps and identified against the bundled catalog.
public struct ClusterInventory: Decodable, Equatable, Sendable {
    /// Unix ms.
    public let at: Int64
    /// Nodes of the context, and those whose containers are counted.
    public let nodes: Int
    public let answered: Int
    /// Sorted by name.
    public let apps: [InventoryApp]

    public init(at: Int64 = 0, nodes: Int = 0, answered: Int = 0, apps: [InventoryApp] = []) {
        self.at = at
        self.nodes = nodes
        self.answered = answered
        self.apps = apps
    }

    private enum CodingKeys: String, CodingKey { case at, nodes, answered, apps }

    // Go encodes an empty (nil) slice as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.field(.at, 0)
        nodes = try c.field(.nodes, 0)
        answered = try c.field(.answered, 0)
        apps = try c.field(.apps, [])
    }

    /// Containers of every app.
    public var containers: Int { apps.reduce(0) { $0 + $1.containers } }
}

/// The catalog categories; one the app does not know is `other`.
public enum AppCategory: String, CaseIterable, Sendable {
    case system, networking, storage, observability, security, database, messaging, devops,
         media, home, productivity, ai, web, other
}

public struct InventoryApp: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    /// A proper noun, never translated.
    public let name: String
    public let category: AppCategory
    /// Bundled icon: appicons/<icon>.webp.
    public let icon: String?
    /// Dashboard Icons slug, fetched only when the user allowed it.
    public let remoteIcon: String?
    /// A resource's own icon (https URL or data: URI), first choice when valid; see customIconSource.
    public let iconURL: String?
    /// False: not recognised by the catalog.
    public let known: Bool
    /// Kubernetes and Talos plumbing.
    public let system: Bool
    /// Empty when unknown (e.g. pinned by digest).
    public let version: String
    /// The same repository runs with several tags.
    public let drift: Bool
    /// :latest or untagged.
    public let unpinned: Bool
    public let namespaces: [String]
    /// Node addresses.
    public let nodes: [String]
    public let containers: Int
    public let running: Int
    /// Bytes.
    public let memory: UInt64
    public let images: [InventoryImage]
    public let pods: [InventoryPod]

    public init(
        id: String, name: String, category: AppCategory = .other, icon: String? = nil, remoteIcon: String? = nil,
        iconURL: String? = nil, known: Bool = true, system: Bool = false, version: String = "", drift: Bool = false, unpinned: Bool = false,
        namespaces: [String] = [], nodes: [String] = [], containers: Int = 0, running: Int = 0, memory: UInt64 = 0,
        images: [InventoryImage] = [], pods: [InventoryPod] = []
    ) {
        self.id = id
        self.name = name
        self.category = category
        self.icon = icon
        self.remoteIcon = remoteIcon
        self.iconURL = iconURL
        self.known = known
        self.system = system
        self.version = version
        self.drift = drift
        self.unpinned = unpinned
        self.namespaces = namespaces
        self.nodes = nodes
        self.containers = containers
        self.running = running
        self.memory = memory
        self.images = images
        self.pods = pods
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, category, icon, remoteIcon, iconURL = "iconUrl", known, system, version, drift, unpinned
        case namespaces, nodes, containers, running, memory, images, pods
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.field(.name, id)
        category = AppCategory(rawValue: try c.decodeIfPresent(String.self, forKey: .category) ?? "") ?? .other
        icon = try c.decodeIfPresent(String.self, forKey: .icon).nonEmpty
        remoteIcon = try c.decodeIfPresent(String.self, forKey: .remoteIcon).nonEmpty
        iconURL = try c.decodeIfPresent(String.self, forKey: .iconURL).nonEmpty
        known = try c.field(.known, false)
        system = try c.field(.system, false)
        version = try c.field(.version, "")
        drift = try c.field(.drift, false)
        unpinned = try c.field(.unpinned, false)
        namespaces = try c.field(.namespaces, [])
        nodes = try c.field(.nodes, [])
        containers = try c.field(.containers, 0)
        running = try c.field(.running, 0)
        memory = try c.field(.memory, 0)
        images = try c.field(.images, [])
        pods = try c.field(.pods, [])
    }

    /// Worth a look: versions drift, or the image is not pinned.
    public var needsAttention: Bool { drift || unpinned }

    /// Repositories running with more than one tag.
    public var driftingRepos: Set<String> {
        let tags = Dictionary(grouping: images.filter { !$0.repo.isEmpty }, by: \.repo)
        return Set(tags.filter { Set($0.value.map(\.tag)).count > 1 }.keys)
    }

    /// The most tags one repository runs with (1 without drift).
    public var versionCount: Int {
        let tags = Dictionary(grouping: images.filter { !$0.repo.isEmpty }, by: \.repo)
        return max(tags.values.map { Set($0.map(\.tag)).count }.max() ?? 1, 1)
    }
}

public struct InventoryImage: Decodable, Equatable, Identifiable, Sendable {
    /// Empty for an image known only by its digest.
    public let repo: String
    public let tag: String
    public let digest: String
    public let containers: Int

    public var id: String { "\(repo):\(tag)@\(digest)" }

    public init(repo: String, tag: String = "", digest: String = "", containers: Int = 1) {
        self.repo = repo
        self.tag = tag
        self.digest = digest
        self.containers = containers
    }

    private enum CodingKeys: String, CodingKey { case repo, tag, digest, containers }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repo = try c.field(.repo, "")
        tag = try c.field(.tag, "")
        digest = try c.field(.digest, "")
        containers = try c.field(.containers, 0)
    }
}

public struct InventoryPod: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let pod: String
    /// Node address.
    public let node: String
    public let containers: [InventoryContainer]

    public var id: String { "\(node)/\(namespace)/\(pod)" }

    public init(namespace: String, pod: String, node: String, containers: [InventoryContainer] = []) {
        self.namespace = namespace
        self.pod = pod
        self.node = node
        self.containers = containers
    }

    private enum CodingKeys: String, CodingKey { case namespace, pod, node, containers }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        pod = try c.field(.pod, "")
        node = try c.field(.node, "")
        containers = try c.field(.containers, [])
    }

    public var allRunning: Bool { containers.allSatisfy(\.isRunning) }
    public var memory: UInt64 { containers.reduce(UInt64(0)) { $0 &+ $1.memory } }
}

public struct InventoryContainer: Decodable, Equatable, Sendable {
    public let name: String
    public let image: String
    public let status: String
    /// Bytes.
    public let memory: UInt64

    public init(name: String, image: String = "", status: String = "CONTAINER_RUNNING", memory: UInt64 = 0) {
        self.name = name
        self.image = image
        self.status = status
        self.memory = memory
    }

    /// "CONTAINER_RUNNING" (CRI) or "RUNNING".
    public var isRunning: Bool { status.uppercased().hasSuffix("RUNNING") }
}

/// "sha256:0123456789ab" (the first 12 hex digits, like docker).
public func shortDigest(_ digest: String) -> String {
    guard let colon = digest.firstIndex(of: ":") else { return String(digest.prefix(12)) }
    let hex = digest[digest.index(after: colon)...]
    return "\(digest[..<colon]):\(hex.prefix(12))"
}
