import Foundation

// Quick edits of a source file: an image tag or the replicas, rewritten by the Go core with
// every other byte kept (go/ichorgo/gitops_quickedit.go), then the usual review and pull
// request. Same records as Android (model/GitOpsQuickEdit.kt).

/// One container of a workload document and its image as the file has it.
public struct QuickEditContainer: Decodable, Hashable, Sendable {
    public let name: String
    public let image: String
    /// The kustomize `images:` entry of the same file that replaces this image at build time,
    /// "" for none: that entry is what is edited.
    public let overriddenBy: String

    public init(name: String, image: String, overriddenBy: String = "") {
        self.name = name
        self.image = image
        self.overriddenBy = overriddenBy
    }

    enum CodingKeys: String, CodingKey { case name, image, overriddenBy }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        image = try c.field(.image, "")
        overriddenBy = try c.field(.overriddenBy, "")
    }
}

/// A workload document of the file (Deployment, StatefulSet, DaemonSet, Job, CronJob…).
public struct QuickEditDocument: Decodable, Hashable, Identifiable, Sendable {
    /// Its position in the file (0 for the first document).
    public let index: Int
    public let kind: String
    public let name: String
    /// spec.replicas as the file sets it; nil when it does not (Kubernetes then runs 1).
    public let replicas: Int?
    /// The replicas op applies (the Go core's word; a guess from the kind for an older core).
    public let scalable: Bool
    public let containers: [QuickEditContainer]

    public var id: Int { index }

    public init(index: Int, kind: String, name: String, replicas: Int? = nil, scalable: Bool? = nil,
                containers: [QuickEditContainer] = []) {
        self.index = index
        self.kind = kind
        self.name = name
        self.replicas = replicas
        self.scalable = scalable ?? guessScalable(kind: kind, replicas: replicas)
        self.containers = containers
    }

    enum CodingKeys: String, CodingKey { case index, kind, name, replicas, scalable, containers }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        index = try c.field(.index, 0)
        kind = try c.field(.kind, "")
        name = try c.field(.name, "")
        replicas = try c.decodeIfPresent(Int.self, forKey: .replicas)
        scalable = try c.decodeIfPresent(Bool.self, forKey: .scalable) ?? guessScalable(kind: kind, replicas: replicas)
        containers = try c.field(.containers, [])
    }

    /// The replicas Kubernetes runs: the file's, else the default of 1.
    public var currentReplicas: Int { replicas ?? 1 }
}

/// What has spec.replicas, as the Go core lists it, for a core that does not say.
private func guessScalable(kind: String, replicas: Int?) -> Bool {
    replicas != nil || ["Deployment", "StatefulSet", "ReplicaSet", "ReplicationController", "Rollout"].contains(kind)
}

/// An entry of a kustomization.yaml `images:` list: it overrides the manifests' image.
public struct QuickEditKustomizeImage: Decodable, Hashable, Sendable {
    /// The kustomization's document in the file.
    public let documentIndex: Int
    public let name: String
    public let newName: String
    public let newTag: String

    public init(documentIndex: Int = 0, name: String, newName: String = "", newTag: String = "") {
        self.documentIndex = documentIndex
        self.name = name
        self.newName = newName
        self.newTag = newTag
    }

    enum CodingKeys: String, CodingKey { case documentIndex, name, newName, newTag }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        documentIndex = try c.field(.documentIndex, 0)
        name = try c.field(.name, "")
        newName = try c.field(.newName, "")
        newTag = try c.field(.newTag, "")
    }

    /// The image the entry makes the manifests run.
    public var image: String {
        let repository = newName.isEmpty ? name : newName
        return newTag.isEmpty ? repository : "\(repository):\(newTag)"
    }
}

/// An entry of a kustomization.yaml `replicas:` list: it overrides the manifests' count.
public struct QuickEditKustomizeReplica: Decodable, Hashable, Sendable {
    public let documentIndex: Int
    /// The workload it scales.
    public let name: String
    /// nil when the entry has no count yet.
    public let count: Int?

    public init(documentIndex: Int = 0, name: String, count: Int? = nil) {
        self.documentIndex = documentIndex
        self.name = name
        self.count = count
    }

    enum CodingKeys: String, CodingKey { case documentIndex, name, count }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        documentIndex = try c.field(.documentIndex, 0)
        name = try c.field(.name, "")
        count = try c.decodeIfPresent(Int.self, forKey: .count)
    }
}

/// What a file offers to quick edits (GitOpsQuickEditTargets).
public struct QuickEditTargets: Decodable, Hashable, Sendable {
    public let documents: [QuickEditDocument]
    public let kustomizeImages: [QuickEditKustomizeImage]
    public let kustomizeReplicas: [QuickEditKustomizeReplica]

    public init(documents: [QuickEditDocument] = [], kustomizeImages: [QuickEditKustomizeImage] = [],
                kustomizeReplicas: [QuickEditKustomizeReplica] = []) {
        self.documents = documents
        self.kustomizeImages = kustomizeImages
        self.kustomizeReplicas = kustomizeReplicas
    }

    enum CodingKeys: String, CodingKey { case documents, kustomizeImages, kustomizeReplicas }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        documents = try c.field(.documents, [])
        kustomizeImages = try c.field(.kustomizeImages, [])
        kustomizeReplicas = try c.field(.kustomizeReplicas, [])
    }

    /// Every image the file sets: the kustomization's entries first (they win over the
    /// manifests), then each container whose image no entry of the file replaces.
    public var images: [QuickEditImageTarget] {
        let several = documents.filter { !$0.containers.isEmpty }.count > 1
        let kustomize = kustomizeImages.map { entry in
            QuickEditImageTarget(place: .kustomize(documentIndex: entry.documentIndex, name: entry.name),
                                 label: shortImageName(entry.name), image: entry.image)
        }
        let containers = documents.flatMap { document in
            document.containers.filter { $0.overriddenBy.isEmpty }.map { container in
                QuickEditImageTarget(place: .container(documentIndex: document.index, name: container.name),
                                     label: several ? "\(container.name) (\(document.kind) \(document.name))" : container.name,
                                     image: container.image)
            }
        }
        return kustomize + containers
    }

    /// Every count the file sets: the kustomization's `replicas:` entries, then the workloads.
    public var replicas: [QuickEditReplicaTarget] {
        let kustomize = kustomizeReplicas.map { entry in
            QuickEditReplicaTarget(place: .kustomize(documentIndex: entry.documentIndex, name: entry.name),
                                   label: entry.name, current: entry.count ?? 1)
        }
        let workloads = documents.filter(\.scalable).map { document in
            QuickEditReplicaTarget(place: .document(index: document.index), label: "\(document.kind) \(document.name)",
                                   current: document.currentReplicas, name: document.name)
        }
        return kustomize + workloads
    }

    /// Nothing to quick edit: the menu is hidden.
    public var isEmpty: Bool { images.isEmpty && replicas.isEmpty }
}

/// Where an image is set in the file.
public enum QuickEditImagePlace: Hashable, Sendable {
    case container(documentIndex: Int, name: String)
    /// The kustomization's `images:` entry of that name.
    case kustomize(documentIndex: Int, name: String)
}

/// One image of the file the image sheet offers.
public struct QuickEditImageTarget: Hashable, Identifiable, Sendable {
    public let place: QuickEditImagePlace
    /// The container's name, or the kustomize entry's short image name.
    public let label: String
    /// The image as the file sets it now.
    public let image: String

    public var id: QuickEditImagePlace { place }

    public init(place: QuickEditImagePlace, label: String, image: String) {
        self.place = place
        self.label = label
        self.image = image
    }

    public var isKustomize: Bool {
        if case .kustomize = place { return true }
        return false
    }

    /// The name the pull request's title gives it.
    public var titleName: String {
        switch place {
        case .container(_, let name): return name
        case .kustomize(_, let name): return shortImageName(name)
        }
    }

    /// The edit setting this image to tag: the whole reference for a container (its digest
    /// dropped), the tag alone for a kustomize entry (its newTag; newName stays).
    public func edit(tag: String) -> QuickEdit {
        let tag = tag.trimmingCharacters(in: .whitespacesAndNewlines)
        switch place {
        case .container(let index, let name):
            return QuickEdit(op: .image, documentIndex: index, container: name, image: imageWithTag(image, tag: tag),
                             label: titleName)
        case .kustomize(let index, let name):
            return QuickEdit(op: .image, documentIndex: index, image: tag, kustomizeImage: name, label: titleName)
        }
    }
}

/// Where a count is set in the file.
public enum QuickEditReplicaPlace: Hashable, Sendable {
    case document(index: Int)
    /// The kustomization's `replicas:` entry of that workload name.
    case kustomize(documentIndex: Int, name: String)
}

/// One count of the file the replicas sheet offers.
public struct QuickEditReplicaTarget: Hashable, Identifiable, Sendable {
    public let place: QuickEditReplicaPlace
    /// "Deployment podinfo", or the kustomize entry's workload name.
    public let label: String
    /// The count the file sets now (1, Kubernetes' default, when it sets none).
    public let current: Int
    /// The workload's name, for the pull request's title.
    public let name: String

    public var id: QuickEditReplicaPlace { place }

    public init(place: QuickEditReplicaPlace, label: String, current: Int, name: String? = nil) {
        self.place = place
        self.label = label
        self.current = current
        self.name = name ?? label
    }

    public var isKustomize: Bool {
        if case .kustomize = place { return true }
        return false
    }

    /// The edit setting this count to replicas.
    public func edit(replicas: Int) -> QuickEdit {
        switch place {
        case .document(let index):
            return QuickEdit(op: .replicas, documentIndex: index, replicas: replicas, label: name)
        case .kustomize(let index, let entry):
            return QuickEdit(op: .replicas, documentIndex: index, replicas: replicas, kustomizeReplica: entry, label: name)
        }
    }
}

/// One edit as GitOpsQuickEdit takes it.
public struct QuickEdit: Encodable, Hashable, Sendable {
    public enum Op: String, Encodable, Sendable {
        case image, replicas
    }

    public let op: Op
    public let documentIndex: Int
    public let container: String?
    /// The whole reference for a container, the tag alone for a kustomize entry.
    public let image: String?
    public let replicas: Int?
    /// The kustomization `images:` entry edited instead of a container.
    public let kustomizeImage: String?
    /// The kustomization `replicas:` entry edited instead of a workload.
    public let kustomizeReplica: String?
    /// The container, image or workload the title names; not sent.
    public let label: String

    public init(op: Op, documentIndex: Int, container: String? = nil, image: String? = nil, replicas: Int? = nil,
                kustomizeImage: String? = nil, kustomizeReplica: String? = nil, label: String) {
        self.op = op
        self.documentIndex = documentIndex
        self.container = container
        self.image = image
        self.replicas = replicas
        self.kustomizeImage = kustomizeImage
        self.kustomizeReplica = kustomizeReplica
        self.label = label
    }

    enum CodingKeys: String, CodingKey { case op, documentIndex, container, image, replicas, kustomizeImage, kustomizeReplica }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(op, forKey: .op)
        try c.encode(documentIndex, forKey: .documentIndex)
        try c.encodeIfPresent(container, forKey: .container)
        try c.encodeIfPresent(image, forKey: .image)
        try c.encodeIfPresent(replicas, forKey: .replicas)
        try c.encodeIfPresent(kustomizeImage, forKey: .kustomizeImage)
        try c.encodeIfPresent(kustomizeReplica, forKey: .kustomizeReplica)
    }

    /// The JSON GitOpsQuickEdit takes.
    public func json() throws -> String { try TalosJSON.encode(self) }
}

/// The rewritten file (GitOpsQuickEdit's answer).
public struct QuickEditResult: Decodable, Hashable, Sendable {
    public let content: String
    /// false: the file already had this value.
    public let changed: Bool
    /// The line edited (1-based), 0 when unknown.
    public let line: Int

    public init(content: String, changed: Bool, line: Int = 0) {
        self.content = content
        self.changed = changed
        self.line = line
    }

    enum CodingKeys: String, CodingKey { case content, changed, line }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        content = try c.field(.content, "")
        changed = try c.field(.changed, false)
        line = try c.field(.line, 0)
    }
}

/// The pull request's default title (English, like the Go core's): "Bump podinfo to 6.7.0",
/// "Scale podinfo to 3".
public func quickEditTitle(_ edit: QuickEdit) -> String {
    switch edit.op {
    case .image:
        let image = edit.image ?? ""
        // A kustomize entry's edit is the tag alone.
        let tag = edit.kustomizeImage != nil ? image : imageTag(image)
        return "Bump \(edit.label) to \(tag.isEmpty ? image : tag)"
    case .replicas:
        return "Scale \(edit.label) to \(edit.replicas ?? 0)"
    }
}

/// The image without its tag and digest: "ghcr.io/example-org/app:1.2" → "ghcr.io/example-org/app".
public func imageRepository(_ image: String) -> String {
    let undigested = image.split(separator: "@", maxSplits: 1).first.map(String.init) ?? image
    guard let colon = undigested.lastIndex(of: ":") else { return undigested }
    // A colon before the last slash is a registry port, not a tag.
    if let slash = undigested.lastIndex(of: "/"), slash > colon { return undigested }
    return String(undigested[..<colon])
}

/// The image's tag, "" without one (latest, or a digest only).
public func imageTag(_ image: String) -> String {
    let undigested = image.split(separator: "@", maxSplits: 1).first.map(String.init) ?? image
    let repository = imageRepository(undigested)
    guard undigested.count > repository.count + 1 else { return "" }
    return String(undigested.dropFirst(repository.count + 1))
}

/// The image at tag, its digest dropped (a digest would keep the old one running).
public func imageWithTag(_ image: String, tag: String) -> String {
    let tag = tag.trimmingCharacters(in: .whitespacesAndNewlines)
    let repository = imageRepository(image)
    return tag.isEmpty ? repository : "\(repository):\(tag)"
}

/// "ghcr.io/example-org/app" → "app".
public func shortImageName(_ image: String) -> String {
    let repository = imageRepository(image)
    return repository.split(separator: "/").last.map(String.init) ?? repository
}

/// The tags the image sheet suggests: the previous one from the app's history and the one
/// running now, each only when it is another tag of the same repository.
public struct QuickEditTagSuggestions: Equatable, Sendable {
    public let previous: String?
    public let running: String?

    public init(previous: String? = nil, running: String? = nil) {
        self.previous = previous
        self.running = running
    }

    public var isEmpty: Bool { previous == nil && running == nil }
}

/// history: the images the app ran before, latest first; live: the ones it runs now.
public func quickEditTagSuggestions(for image: String, history: [String], live: [String]) -> QuickEditTagSuggestions {
    let repository = imageRepository(image)
    let current = imageTag(image)
    func other(_ images: [String]) -> String? {
        images.first { imageRepository($0) == repository && !imageTag($0).isEmpty && imageTag($0) != current }
            .map(imageTag)
    }
    let running = other(live)
    let previous = other(history)
    return QuickEditTagSuggestions(previous: previous, running: running == previous ? nil : running)
}
