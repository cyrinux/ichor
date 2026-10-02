import Foundation

/// A container image in the node's CRI namespace (NodeImages).
public struct ContainerImage: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let digest: String
    /// Bytes.
    public let size: Int64
    /// Unix ms, 0 when unknown.
    public let created: Int64

    // The same image can be listed under several tags: the name tells them apart.
    public var id: String { "\(name)@\(digest)" }

    public init(name: String, digest: String = "", size: Int64 = 0, created: Int64 = 0) {
        self.name = name
        self.digest = digest
        self.size = size
        self.created = created
    }
}

public enum ImageSort: String, CaseIterable, Sendable {
    case name, size, created
}

/// Case-insensitive match on the name or the digest; an empty query keeps everything.
public func filterImages(_ images: [ContainerImage], query: String) -> [ContainerImage] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return images }
    return images.filter {
        $0.name.range(of: needle, options: .caseInsensitive) != nil || $0.digest.range(of: needle, options: .caseInsensitive) != nil
    }
}

/// By name (A→Z), size (largest first) or creation (newest first, unknown last); ties by name.
public func sortImages(_ images: [ContainerImage], by sort: ImageSort) -> [ContainerImage] {
    images.sorted { a, b in
        switch sort {
        case .name: break
        case .size: if a.size != b.size { return a.size > b.size }
        case .created: if a.created != b.created { return a.created > b.created }
        }
        if a.name != b.name { return a.name < b.name }
        return a.digest < b.digest
    }
}

/// Total size of the listed images. Images sharing layers are counted once each, like
/// `talosctl image list`, so this is an upper bound of the disk use.
public func totalImageSize(_ images: [ContainerImage]) -> Int64 {
    images.reduce(Int64(0)) { $0 &+ max($1.size, 0) }
}
