import Foundation

// The cluster's update candidates (go/ichorgo/updates_cluster.go, ClusterUpdates) and where one
// is edited (updates_edit.go, UpdateEditTarget). Same records as Android (model/ClusterUpdates.kt).

/// Who applied an image's workloads or a chart: tool flux, argocd or helm ("" for a bare workload).
public struct UpdateOwner: Codable, Hashable, Sendable {
    public let tool: String
    public let kind: String
    public let namespace: String
    public let name: String

    public init(tool: String, kind: String, namespace: String, name: String) {
        self.tool = tool
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }

    enum CodingKeys: String, CodingKey { case tool, kind, namespace, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tool = try c.field(.tool, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }
}

/// The quick edit hint of an item: op image (a workload's container) or path (a manifest field;
/// an empty path for an image a chart renders).
public struct UpdateEdit: Codable, Hashable, Sendable {
    public let op: String
    public let kind: String
    public let name: String
    public let container: String
    public let image: String
    public let path: String
    public let value: String
    public let hint: String
    public let valueFiles: [String]

    enum CodingKeys: String, CodingKey { case op, kind, name, container, image, path, value, hint, valueFiles }

    public init(
        op: String, kind: String = "", name: String = "", container: String = "", image: String = "",
        path: String = "", value: String = ""
    ) {
        self.op = op
        self.kind = kind
        self.name = name
        self.container = container
        self.image = image
        self.path = path
        self.value = value
        hint = ""
        valueFiles = []
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        op = try c.field(.op, "")
        kind = try c.field(.kind, "")
        name = try c.field(.name, "")
        container = try c.field(.container, "")
        image = try c.field(.image, "")
        path = try c.field(.path, "")
        value = try c.field(.value, "")
        hint = try c.field(.hint, "")
        valueFiles = try c.field(.valueFiles, [String]())
    }
}

public struct UpdateWorkloadRef: Codable, Hashable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let containers: [String]

    enum CodingKeys: String, CodingKey { case kind, namespace, name, containers }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        containers = try c.field(.containers, [String]())
    }
}

/// An image the cluster runs, its newer tags and who bumps it.
public struct UpdateImage: Codable, Hashable, Sendable {
    public let repo: String
    public let tag: String
    public let digest: String
    public let workloads: [UpdateWorkloadRef]
    public let more: Int
    public let owner: UpdateOwner?
    /// "not readable" when its pull secrets cannot be read.
    public let credentials: String
    public let current: String
    public let patch: String
    public let minor: String
    public let major: String
    public let prerelease: Bool
    public let shape: String
    public let reason: String
    public let source: String
    public let automatedBy: String
    public let edit: UpdateEdit?

    enum CodingKeys: String, CodingKey {
        case repo, tag, digest, workloads, more, owner, credentials, current, patch, minor, major, prerelease, shape
        case reason, source, automatedBy, edit
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repo = try c.field(.repo, "")
        tag = try c.field(.tag, "")
        digest = try c.field(.digest, "")
        workloads = try c.field(.workloads, [UpdateWorkloadRef]())
        more = try c.field(.more, 0)
        owner = try c.decodeIfPresent(UpdateOwner.self, forKey: .owner)
        credentials = try c.field(.credentials, "")
        current = try c.field(.current, "")
        patch = try c.field(.patch, "")
        minor = try c.field(.minor, "")
        major = try c.field(.major, "")
        prerelease = try c.field(.prerelease, false)
        shape = try c.field(.shape, "")
        reason = try c.field(.reason, "")
        source = try c.field(.source, "")
        automatedBy = try c.field(.automatedBy, "")
        edit = try c.decodeIfPresent(UpdateEdit.self, forKey: .edit)
    }
}

/// A chart a Flux, Argo CD or Helm release installs, its newer versions and who bumps it.
public struct UpdateChart: Codable, Hashable, Sendable {
    public let name: String
    public let version: String
    public let repoURL: String
    public let owner: UpdateOwner?
    public let constraint: String
    public let current: String
    public let patch: String
    public let minor: String
    public let major: String
    public let prerelease: Bool
    public let shape: String
    public let reason: String
    public let source: String
    public let automatedBy: String
    public let edit: UpdateEdit?

    enum CodingKeys: String, CodingKey {
        case name, version, repoURL, owner, constraint, current, patch, minor, major, prerelease, shape
        case reason, source, automatedBy, edit
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        version = try c.field(.version, "")
        repoURL = try c.field(.repoURL, "")
        owner = try c.decodeIfPresent(UpdateOwner.self, forKey: .owner)
        constraint = try c.field(.constraint, "")
        current = try c.field(.current, "")
        patch = try c.field(.patch, "")
        minor = try c.field(.minor, "")
        major = try c.field(.major, "")
        prerelease = try c.field(.prerelease, false)
        shape = try c.field(.shape, "")
        reason = try c.field(.reason, "")
        source = try c.field(.source, "")
        automatedBy = try c.field(.automatedBy, "")
        edit = try c.decodeIfPresent(UpdateEdit.self, forKey: .edit)
    }
}

public struct UpdateSkipped: Decodable, Hashable, Sendable {
    public let what: String
    public let reason: String

    enum CodingKeys: String, CodingKey { case what, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        what = try c.field(.what, "")
        reason = try c.field(.reason, "")
    }
}

public struct UpdateLookups: Decodable, Hashable, Sendable {
    public let queried: Int
    public let failed: Int

    public init(queried: Int = 0, failed: Int = 0) {
        self.queried = queried
        self.failed = failed
    }

    enum CodingKeys: String, CodingKey { case queried, failed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        queried = try c.field(.queried, 0)
        failed = try c.field(.failed, 0)
    }
}

/// ClusterUpdates' answer.
public struct ClusterUpdates: Decodable, Hashable, Sendable {
    /// When it was read (epoch milliseconds).
    public let at: Int64
    public let images: [UpdateImage]
    public let charts: [UpdateChart]
    public let skipped: [UpdateSkipped]
    public let lookups: UpdateLookups

    enum CodingKeys: String, CodingKey { case at, images, charts, skipped, lookups }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.field(.at, Int64(0))
        images = try c.field(.images, [UpdateImage]())
        charts = try c.field(.charts, [UpdateChart]())
        skipped = try c.field(.skipped, [UpdateSkipped]())
        lookups = try c.field(.lookups, UpdateLookups())
    }
}

/// The app whose source holds the file to edit.
public struct UpdateEditApp: Decodable, Hashable, Sendable {
    public let tool: String
    public let kind: String
    public let namespace: String
    public let name: String

    enum CodingKeys: String, CodingKey { case tool, kind, namespace, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tool = try c.field(.tool, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    public var ref: GitOpsAppRef { GitOpsAppRef(tool: tool, kind: kind, namespace: namespace, name: name) }
}

/// UpdateEditTarget's quick edit, GitOpsQuickEdit's editJSON.
public struct UpdateQuickEdit: Decodable, Hashable, Sendable {
    public let op: String
    public let documentIndex: Int
    public let kind: String
    public let name: String
    public let container: String
    public let kustomizeImage: String
    public let image: String
    public let path: String
    public let value: String

    public init(
        op: String, documentIndex: Int = 0, kind: String = "", name: String = "", container: String = "",
        kustomizeImage: String = "", image: String = "", path: String = "", value: String = ""
    ) {
        self.op = op
        self.documentIndex = documentIndex
        self.kind = kind
        self.name = name
        self.container = container
        self.kustomizeImage = kustomizeImage
        self.image = image
        self.path = path
        self.value = value
    }

    enum CodingKeys: String, CodingKey {
        case op, documentIndex, kind, name, container, kustomizeImage, image, path, value
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        op = try c.field(.op, "")
        documentIndex = try c.field(.documentIndex, 0)
        kind = try c.field(.kind, "")
        name = try c.field(.name, "")
        container = try c.field(.container, "")
        kustomizeImage = try c.field(.kustomizeImage, "")
        image = try c.field(.image, "")
        path = try c.field(.path, "")
        value = try c.field(.value, "")
    }
}

/// UpdateEditTarget's answer: the file (path of source sourceIndex of app) and the edit that
/// bumps the item; note when there is none.
public struct UpdateEditTarget: Decodable, Hashable, Sendable {
    public let app: UpdateEditApp?
    public let source: GitSource?
    public let sourceIndex: Int
    public let path: String
    public let line: Int
    public let documentIndex: Int
    public let edit: UpdateQuickEdit?
    public let note: String
    public let valueFiles: [String]

    enum CodingKeys: String, CodingKey {
        case app, source, sourceIndex, path, line, documentIndex, edit, note, valueFiles
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        app = try c.decodeIfPresent(UpdateEditApp.self, forKey: .app)
        source = try c.decodeIfPresent(GitSource.self, forKey: .source)
        sourceIndex = try c.field(.sourceIndex, 0)
        path = try c.field(.path, "")
        line = try c.field(.line, 0)
        documentIndex = try c.field(.documentIndex, 0)
        edit = try c.decodeIfPresent(UpdateQuickEdit.self, forKey: .edit)
        note = try c.field(.note, "")
        valueFiles = try c.field(.valueFiles, [String]())
    }
}
