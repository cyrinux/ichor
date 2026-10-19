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
    /// Pinned by digest ("" for none): left to the editor.
    public let digest: String

    public init(documentIndex: Int = 0, name: String, newName: String = "", newTag: String = "", digest: String = "") {
        self.documentIndex = documentIndex
        self.name = name
        self.newName = newName
        self.newTag = newTag
        self.digest = digest
    }

    enum CodingKeys: String, CodingKey { case documentIndex, name, newName, newTag, digest }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        documentIndex = try c.field(.documentIndex, 0)
        name = try c.field(.name, "")
        newName = try c.field(.newName, "")
        newTag = try c.field(.newTag, "")
        digest = try c.field(.digest, "")
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
    /// manifests; one pinned by digest is left to the editor), then each named container whose
    /// image no entry of the file replaces.
    public var images: [QuickEditImageTarget] {
        let several = documents.filter { !$0.containers.isEmpty }.count > 1
        let kustomize = kustomizeImages.filter { !$0.name.isEmpty && $0.digest.isEmpty }.map { entry in
            QuickEditImageTarget(place: .kustomize(documentIndex: entry.documentIndex, name: entry.name),
                                 label: shortImageName(entry.name), image: entry.image)
        }
        let containers = documents.flatMap { document in
            document.containers.filter { !$0.name.isEmpty && $0.overriddenBy.isEmpty }.map { container in
                QuickEditImageTarget(place: .container(documentIndex: document.index, name: container.name),
                                     label: several ? "\(container.name) (\(document.kind) \(document.name))" : container.name,
                                     image: container.image, workloadKind: document.kind, workloadName: document.name)
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
                                   current: document.currentReplicas, name: document.name, workloadKind: document.kind)
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
    /// A container's workload document: its kind and metadata.name ("" for a kustomize entry).
    public let workloadKind: String
    public let workloadName: String

    public var id: QuickEditImagePlace { place }

    public init(place: QuickEditImagePlace, label: String, image: String, workloadKind: String = "", workloadName: String = "") {
        self.place = place
        self.label = label
        self.image = image
        self.workloadKind = workloadKind
        self.workloadName = workloadName
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
            return QuickEdit(op: .image, documentIndex: index, kind: nonEmpty(workloadKind), name: nonEmpty(workloadName),
                             container: name, image: imageWithTag(image, tag: tag), label: titleName)
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
    /// The workload's kind ("" for a kustomize entry): with name, what the Go core checks.
    public let workloadKind: String

    public var id: QuickEditReplicaPlace { place }

    public init(place: QuickEditReplicaPlace, label: String, current: Int, name: String? = nil, workloadKind: String = "") {
        self.place = place
        self.label = label
        self.current = current
        self.name = name ?? label
        self.workloadKind = workloadKind
    }

    public var isKustomize: Bool {
        if case .kustomize = place { return true }
        return false
    }

    /// The edit setting this count to replicas.
    public func edit(replicas: Int) -> QuickEdit {
        switch place {
        case .document(let index):
            let kind = nonEmpty(workloadKind)
            return QuickEdit(op: .replicas, documentIndex: index, kind: kind, name: kind == nil ? nil : nonEmpty(name),
                             replicas: replicas, label: name)
        case .kustomize(let index, let entry):
            return QuickEdit(op: .replicas, documentIndex: index, replicas: replicas, kustomizeReplica: entry, label: name)
        }
    }
}

/// One edit as GitOpsQuickEdit takes it.
public struct QuickEdit: Encodable, Hashable, Sendable {
    public enum Op: String, Encodable, Sendable {
        /// keep: the live values of a diff written where the file holds the wanted ones;
        /// talos: a machine config patch written into a node's file of the cluster repository.
        case image, replicas, keep, talos
    }

    public let op: Op
    public let documentIndex: Int
    /// The kind and metadata.name of the workload document a container or workload replicas
    /// edit targets: the Go core checks documentIndex still holds it (else finds it by them).
    public let kind: String?
    public let name: String?
    public let container: String?
    /// The whole reference for a container, the tag alone for a kustomize entry.
    public let image: String?
    public let replicas: Int?
    /// The kustomization `images:` entry edited instead of a container.
    public let kustomizeImage: String?
    /// The kustomization `replicas:` entry edited instead of a workload.
    public let kustomizeReplica: String?
    /// The live values a keep edit writes.
    public let changes: [DiffLiveChange]?
    /// The container, image or workload the title names (talos: the first changed path); not sent.
    public let label: String
    /// talos: the patch, the node, whether the file is new, and the values left to shared patches.
    public let patch: String?
    public let hostname: String?
    public let create: Bool?
    public let skipped: [String]?

    public init(op: Op, documentIndex: Int, kind: String? = nil, name: String? = nil, container: String? = nil,
                image: String? = nil, replicas: Int? = nil, kustomizeImage: String? = nil, kustomizeReplica: String? = nil,
                changes: [DiffLiveChange]? = nil, label: String, patch: String? = nil, hostname: String? = nil,
                create: Bool? = nil, skipped: [String]? = nil) {
        self.op = op
        self.documentIndex = documentIndex
        self.kind = kind
        self.name = name
        self.container = container
        self.image = image
        self.replicas = replicas
        self.kustomizeImage = kustomizeImage
        self.kustomizeReplica = kustomizeReplica
        self.changes = changes
        self.label = label
        self.patch = patch
        self.hostname = hostname
        self.create = create
        self.skipped = skipped
    }

    /// A kept machine config change of hostname written into its file: plan says where and what.
    public static func talos(hostname: String, plan: TalosWriteBackPlan, firstPath: String) -> QuickEdit {
        QuickEdit(op: .talos, documentIndex: 0, label: firstPath, patch: plan.patch, hostname: hostname, create: plan.create,
                  skipped: plan.skipped.map { "\($0.path): \($0.why)" })
    }

    /// The live values of resource (document documentIndex of its manifest) kept in Git.
    public static func keep(documentIndex: Int, resource: KubeDiffResource) -> QuickEdit {
        QuickEdit(op: .keep, documentIndex: documentIndex, kind: resource.kind, name: resource.name,
                  changes: resource.liveChanges, label: "\(resource.kind) \(resource.name)")
    }

    enum CodingKeys: String, CodingKey {
        case op, documentIndex, kind, name, container, image, replicas, kustomizeImage, kustomizeReplica, changes
        case patch, hostname, create, skipped
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(op, forKey: .op)
        try c.encode(documentIndex, forKey: .documentIndex)
        try c.encodeIfPresent(kind, forKey: .kind)
        try c.encodeIfPresent(name, forKey: .name)
        try c.encodeIfPresent(container, forKey: .container)
        try c.encodeIfPresent(image, forKey: .image)
        try c.encodeIfPresent(replicas, forKey: .replicas)
        try c.encodeIfPresent(kustomizeImage, forKey: .kustomizeImage)
        try c.encodeIfPresent(kustomizeReplica, forKey: .kustomizeReplica)
        try c.encodeIfPresent(changes, forKey: .changes)
        try c.encodeIfPresent(patch, forKey: .patch)
        try c.encodeIfPresent(hostname, forKey: .hostname)
        try c.encodeIfPresent(create, forKey: .create)
        try c.encodeIfPresent(skipped, forKey: .skipped)
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
    /// For a keep edit: the live values not written (the file does not set them) and why.
    public let skipped: [String]

    public init(content: String, changed: Bool, line: Int = 0, skipped: [String] = []) {
        self.content = content
        self.changed = changed
        self.line = line
        self.skipped = skipped
    }

    enum CodingKeys: String, CodingKey { case content, changed, line, skipped }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        content = try c.field(.content, "")
        changed = try c.field(.changed, false)
        line = try c.field(.line, 0)
        skipped = try c.field(.skipped, [String]())
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
    case .keep:
        return "Keep live changes of \(edit.label)"
    case .talos:
        let node = edit.hostname ?? ""
        return edit.label.isEmpty ? "Keep \(node)'s config change in Git" : "Keep \(node)'s config change in Git: \(edit.label)"
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

/// Whether tag is an image tag (a digest is changed in the editor): no spaces, slashes or
/// colons, at most 128 characters, not starting with "." or "-". Same rule as Android.
public func isValidImageTag(_ tag: String) -> Bool {
    let tag = tag.trimmingCharacters(in: .whitespacesAndNewlines)
    guard let first = tag.unicodeScalars.first, tag.unicodeScalars.count <= 128 else { return false }
    func allowed(_ c: Unicode.Scalar) -> Bool {
        c.isASCII && (("a"..."z").contains(c) || ("A"..."Z").contains(c) || ("0"..."9").contains(c) || c == "_")
    }
    return allowed(first) && tag.unicodeScalars.dropFirst().allSatisfy { allowed($0) || $0 == "." || $0 == "-" }
}

/// nil for "", for the optional keys of the edit JSON.
private func nonEmpty(_ s: String) -> String? { s.isEmpty ? nil : s }

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
