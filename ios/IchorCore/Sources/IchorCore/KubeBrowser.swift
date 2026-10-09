import Foundation

// The Kubernetes resource browser (the Linear plan document "Study: kubeconfig-only clusters (no Talos)" §7, §7b): any kind the API
// server serves, CRDs included, listed with the server's own Table columns. Mirrors
// go/ichorgo/kube_browser.go and kube_edit.go; same rules as Android's model/KubeBrowser.kt.

/// A listable resource of the cluster (KubeAPIResources), at its group's preferred version.
public struct KubeAPIResource: Decodable, Equatable, Hashable, Identifiable, Sendable {
    /// "" for the core group.
    public let group: String
    public let version: String
    /// The plural name in API paths: "deployments".
    public let resource: String
    public let kind: String
    public let namespaced: Bool
    public let verbs: [String]
    public let shortNames: [String]
    public let categories: [String]
    /// It serves /scale (any CRD declaring it), or is a Job (its parallelism).
    public let scalable: Bool

    public var id: String { "\(group)/\(resource)" }

    public init(group: String = "", version: String = "v1", resource: String, kind: String, namespaced: Bool = true,
                verbs: [String] = ["get", "list"], shortNames: [String] = [], categories: [String] = [], scalable: Bool = false) {
        self.group = group
        self.version = version
        self.resource = resource
        self.kind = kind
        self.namespaced = namespaced
        self.verbs = verbs
        self.shortNames = shortNames
        self.categories = categories
        self.scalable = scalable
    }

    private enum CodingKeys: String, CodingKey { case group, version, resource, kind, namespaced, verbs, shortNames, categories, scalable }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        kind = try c.field(.kind, "")
        namespaced = try c.field(.namespaced, false)
        verbs = try c.field(.verbs, [])
        shortNames = try c.field(.shortNames, [])
        categories = try c.field(.categories, [])
        scalable = try c.field(.scalable, false)
    }

    /// "apps/v1", "v1" for the core group.
    public var groupVersion: String { group.isEmpty ? version : "\(group)/\(version)" }

    /// The browser offers Edit: the credentials' role may still refuse it.
    public var canUpdate: Bool { verbs.contains("update") && verbs.contains("get") }

    /// Secrets: their values are hidden until the user asks for them.
    public var isSecret: Bool { group.isEmpty && resource == "secrets" }

    public var isPod: Bool { group.isEmpty && resource == "pods" }

    public var isJob: Bool { group == "batch" && resource == "jobs" }

    /// The resource a scale patches, as KubeCan takes it: a Job itself, the /scale subresource otherwise.
    public var scaleResource: String { isJob ? resource : "\(resource)/scale" }
}

/// How many pods an object wants (`replicas`) and runs (`current`) (KubeObjectScale). A Job's
/// count is its parallelism (`field` "parallelism"): how many of its pods run at once.
public struct KubeObjectScale: Decodable, Equatable, Sendable {
    public let replicas: Int
    public let current: Int
    public let field: String

    public var isParallelism: Bool { field == "parallelism" }

    public init(replicas: Int = 0, current: Int = 0, field: String = "replicas") {
        self.replicas = replicas
        self.current = current
        self.field = field
    }

    private enum CodingKeys: String, CodingKey { case replicas, current, field }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        replicas = try c.field(.replicas, 0)
        current = try c.field(.current, 0)
        field = try c.field(.field, "replicas")
    }
}

/// What KubeAPIResources answers: the resources, and the group versions that could not be read.
public struct KubeAPIResourceList: Decodable, Equatable, Sendable {
    public let resources: [KubeAPIResource]
    /// "metrics.k8s.io/v1beta1": an aggregated API down, or one the credentials may not read.
    public let failed: [String]

    public init(resources: [KubeAPIResource] = [], failed: [String] = []) {
        self.resources = resources
        self.failed = failed
    }

    private enum CodingKeys: String, CodingKey { case resources, failed }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        resources = try c.field(.resources, [])
        failed = try c.field(.failed, [])
    }
}

/// The resources of one API group, as the browser lists them.
public struct KubeAPIGroupSection: Equatable, Identifiable, Sendable {
    /// "" for the core group.
    public let group: String
    public let resources: [KubeAPIResource]

    public var id: String { group }

    public init(group: String, resources: [KubeAPIResource]) {
        self.group = group
        self.resources = resources
    }
}

/// Where a group comes in the browser: core, apps, the other groups Kubernetes ships, then
/// the rest (CRDs and aggregated APIs).
private func groupRank(_ group: String) -> Int {
    if group.isEmpty { return 0 }
    if group == "apps" { return 1 }
    if !group.contains(".") || group.hasSuffix(".k8s.io") { return 2 }
    return 3
}

/// Whether `resource` matches what is typed: its kind, plural, a short name or its group
/// (case-insensitive; every resource when blank).
public func apiResourceMatches(_ resource: KubeAPIResource, query: String) -> Bool {
    let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
    guard !needle.isEmpty else { return true }
    return resource.kind.lowercased().contains(needle)
        || resource.resource.lowercased().contains(needle)
        || resource.shortNames.contains { $0.lowercased().contains(needle) }
        || resource.group.lowercased().contains(needle)
}

/// The resources matching `query`, by group (core and apps first, then the groups Kubernetes
/// ships, then the others, by name), each sorted by kind.
public func groupAPIResources(_ resources: [KubeAPIResource], query: String = "") -> [KubeAPIGroupSection] {
    let shown = resources.filter { apiResourceMatches($0, query: query) }
    return Dictionary(grouping: shown, by: \.group)
        .map { group, items in
            KubeAPIGroupSection(group: group, resources: items.sorted {
                ($0.kind.lowercased(), $0.resource) < ($1.kind.lowercased(), $1.resource)
            })
        }
        .sorted { (groupRank($0.group), $0.group) < (groupRank($1.group), $1.group) }
}

// MARK: - Pages of any resource

/// A column of the server's Table for a resource.
public struct KubeResourceColumn: Codable, Equatable, Hashable, Sendable {
    public let name: String
    /// 0: what `kubectl get` prints; higher: what `-o wide` adds.
    public let priority: Int
    /// "string", "integer", "date"...
    public let type: String

    public init(name: String, priority: Int = 0, type: String = "string") {
        self.name = name
        self.priority = priority
        self.type = type
    }

    private enum CodingKeys: String, CodingKey { case name, priority, type }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        priority = try c.field(.priority, 0)
        type = try c.field(.type, "")
    }
}

/// An object as a Table row: its cells in the columns' order.
public struct KubeResourceRow: Codable, Equatable, Hashable, Identifiable, Sendable {
    public let name: String
    /// "" for a cluster-scoped object.
    public let namespace: String
    public let cells: [String]
    /// Unix seconds, 0 when unknown.
    public let created: Int64
    /// A deletion is pending (finalizers).
    public let deleting: Bool

    public var id: String { "\(namespace)/\(name)" }

    public init(name: String, namespace: String = "", cells: [String] = [], created: Int64 = 0, deleting: Bool = false) {
        self.name = name
        self.namespace = namespace
        self.cells = cells
        self.created = created
        self.deleting = deleting
    }

    private enum CodingKeys: String, CodingKey { case name, namespace, cells, created, deleting }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        namespace = try c.field(.namespace, "")
        cells = try c.field(.cells, [])
        created = try c.field(.created, 0)
        deleting = try c.field(.deleting, false)
    }
}

/// The namespaces of listed objects, sorted, cluster-scoped ones ("") left out: the
/// namespace menu of a resource list.
public func resourceRowNamespaces(_ rows: [KubeResourceRow]) -> [String] {
    Array(Set(rows.map(\.namespace).filter { !$0.isEmpty })).sorted()
}

/// One page of KubeResourcePage.
public struct KubeResourcePage: Decodable, Equatable, Sendable {
    public let columns: [KubeResourceColumn]
    public let rows: [KubeResourceRow]
    /// Asks for the next page, "" on the last one.
    public let continueToken: String
    /// Items left after this page, -1 when the API server does not say.
    public let remaining: Int64

    public init(columns: [KubeResourceColumn] = [], rows: [KubeResourceRow] = [], continueToken: String = "", remaining: Int64 = 0) {
        self.columns = columns
        self.rows = rows
        self.continueToken = continueToken
        self.remaining = remaining
    }

    private enum CodingKeys: String, CodingKey {
        case columns, rows, remaining
        case continueToken = "continue"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        columns = try c.field(.columns, [])
        rows = try c.field(.rows, [])
        continueToken = try c.field(.continueToken, "")
        remaining = try c.field(.remaining, -1)
    }

    /// As the paged lists take it: complete when there is no next page.
    public var page: KubePage<KubeResourceRow> {
        KubePage(items: rows, continueToken: continueToken, remaining: continueToken.isEmpty ? 0 : remaining,
                 complete: continueToken.isEmpty)
    }
}

/// Columns a row shows under its name: the Name, Namespace and Age ones are said otherwise
/// (the row's title, its subtitle and its age); `wide` adds the `-o wide` ones.
public func browserColumnIndices(_ columns: [KubeResourceColumn], wide: Bool) -> [Int] {
    let said: Set<String> = ["name", "namespace", "age"]
    return columns.indices.filter { index in
        let column = columns[index]
        return (wide || column.priority == 0) && !said.contains(column.name.lowercased())
    }
}

/// Whether any column is left out unless wide: the "wide" toggle is shown then.
public func hasWideColumns(_ columns: [KubeResourceColumn]) -> Bool {
    browserColumnIndices(columns, wide: true).count > browserColumnIndices(columns, wide: false).count
}

/// A row's cells worth a line under its name: (column, value) for `indices`, blank and "<none>"
/// cells left out.
public func browserCells(_ row: KubeResourceRow, columns: [KubeResourceColumn], indices: [Int]) -> [(column: KubeResourceColumn, value: String)] {
    indices.compactMap { index in
        guard index < columns.count, index < row.cells.count else { return nil }
        let value = row.cells[index].trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value != "<none>" else { return nil }
        return (columns[index], value)
    }
}

/// The seconds since a "date" column's timestamp (CRD printer columns are RFC 3339), nil
/// when the cell is not one (built-in kinds already say "5d").
public func browserCellAge(_ value: String, type: String, now: Date = Date()) -> Int64? {
    guard type == "date" else { return nil }
    let formatter = ISO8601DateFormatter()
    guard let date = formatter.date(from: value.trimmingCharacters(in: .whitespaces)) else { return nil }
    return max(0, Int64(now.timeIntervalSince(date)))
}

/// Rows whose name, namespace or a cell contains `query` (case-insensitive; all when blank).
public func filterResourceRows(_ rows: [KubeResourceRow], query: String) -> [KubeResourceRow] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return rows }
    let matches = { (text: String) in text.range(of: needle, options: .caseInsensitive) != nil }
    return rows.filter { matches($0.name) || matches($0.namespace) || $0.cells.contains(where: matches) }
}

/// How a status cell reads: good, in progress, bad, or nothing to say.
public enum KubeTone: Sendable, Equatable {
    case good, warn, bad, neutral
}

private let goodStates: Set<String> = [
    "running", "ready", "bound", "active", "succeeded", "completed", "complete", "true", "available",
    "healthy", "synced", "deployed", "established", "approved,issued",
]
private let warnStates: Set<String> = [
    "pending", "containercreating", "podinitializing", "terminating", "progressing", "unknown",
    "released", "suspended", "outofsync", "pending-install", "pending-upgrade", "pending-rollback",
    "uninstalling", "degraded",
]
private let badStates: Set<String> = [
    "failed", "error", "crashloopbackoff", "imagepullbackoff", "errimagepull", "evicted", "false",
    "notready", "oomkilled", "lost", "missing", "createcontainerconfigerror", "invalidimagename",
]

/// The tone of a status word ("Running", "CrashLoopBackOff", "deployed", "True").
public func kubeStatusTone(_ value: String) -> KubeTone {
    let word = value.trimmingCharacters(in: .whitespaces).lowercased()
    if goodStates.contains(word) { return .good }
    if badStates.contains(word) || word.hasPrefix("init:error") || word.hasPrefix("init:crashloop") { return .bad }
    if warnStates.contains(word) || word.hasPrefix("init:") { return .warn }
    return .neutral
}

/// The tone of a cell, from its column: Status/Phase/Health/Sync words, and "1/2" readiness
/// (all ready, some, none); other columns say nothing.
public func kubeCellTone(column: String, value: String) -> KubeTone {
    switch column.lowercased() {
    case "status", "phase", "health", "sync", "sync status", "health status", "state":
        return kubeStatusTone(value)
    case "ready":
        let parts = value.split(separator: "/")
        if parts.count == 2, let ready = Int(parts[0]), let total = Int(parts[1]) {
            if total == 0 { return .neutral }
            return ready == total ? .good : .warn
        }
        return kubeStatusTone(value)
    default:
        return .neutral
    }
}

// MARK: - Edit

/// What saving an edited object would change (KubeObjectUpdatePreview): a unified diff of the
/// stored object and the one the API server would store.
public struct KubeEditPreview: Decodable, Equatable, Sendable {
    public let changed: Bool
    public let diff: String

    public init(changed: Bool = false, diff: String = "") {
        self.changed = changed
        self.diff = diff
    }

    private enum CodingKeys: String, CodingKey { case changed, diff }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        changed = try c.field(.changed, false)
        diff = try c.field(.diff, "")
    }

    /// The diff's lines without its header.
    public var lines: [DiffLine] { parseDiffLines(diff) }

    /// Lines added and removed.
    public var counts: (added: Int, removed: Int) {
        lines.reduce(into: (0, 0)) { counts, line in
            if line.kind == .added { counts.0 += 1 }
            if line.kind == .removed { counts.1 += 1 }
        }
    }
}

/// How KubeObjectYAML hides a Secret's values ("<hidden, 12 characters>"): such YAML cannot
/// be edited, it would save the placeholders.
public let kubeHiddenSecretMarker = "<hidden, "

/// Whether `yaml` still holds hidden Secret values.
public func hasHiddenSecretValues(_ yaml: String) -> Bool {
    yaml.contains(kubeHiddenSecretMarker)
}

/// Whether a save failed because someone changed the object since it was read (409 Conflict,
/// kube_edit.go editError): reload and edit again, never overwrite.
public func isKubeEditConflict(_ message: String) -> Bool {
    message.contains("the object changed since it was opened")
}

// MARK: - Delete

/// What a deletion does with what the object owns (DeleteOptions.propagationPolicy).
public enum KubeDeletePropagation: String, CaseIterable, Identifiable, Sendable {
    /// The object goes now; the garbage collector deletes what it owned afterwards.
    case background = "Background"
    /// What the object owns is deleted first; the object waits for it.
    case foreground = "Foreground"
    /// What the object owns is kept, without an owner.
    case orphan = "Orphan"

    public var id: String { rawValue }
}

/// An object the deleted one owns: the propagation decides about it.
public struct KubeDeleteDependent: Decodable, Equatable, Hashable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String

    public init(kind: String, namespace: String = "", name: String) {
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }
}

/// What deleting an object would do (KubeObjectDeletePreview).
public struct KubeDeletePreview: Decodable, Equatable, Sendable {
    /// Deleting it breaks the cluster or more than the object: only "Delete anyway" (force).
    public let isProtected: Bool
    /// Why it is protected.
    public let reason: String
    public let clusterScoped: Bool
    public let finalizers: [String]
    public let dependents: [KubeDeleteDependent]
    /// Dependents beyond those listed.
    public let moreDependents: Int
    /// The version read: the delete is refused if the object changed since.
    public let resourceVersion: String
    /// A deletion is already pending, held by the finalizers.
    public let deleting: Bool

    public init(isProtected: Bool = false, reason: String = "", clusterScoped: Bool = false, finalizers: [String] = [],
                dependents: [KubeDeleteDependent] = [], moreDependents: Int = 0, resourceVersion: String = "", deleting: Bool = false) {
        self.isProtected = isProtected
        self.reason = reason
        self.clusterScoped = clusterScoped
        self.finalizers = finalizers
        self.dependents = dependents
        self.moreDependents = moreDependents
        self.resourceVersion = resourceVersion
        self.deleting = deleting
    }

    private enum CodingKeys: String, CodingKey {
        case isProtected = "protected", reason, clusterScoped, finalizers, dependents, moreDependents, resourceVersion, deleting
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        isProtected = try c.field(.isProtected, false)
        reason = try c.field(.reason, "")
        clusterScoped = try c.field(.clusterScoped, false)
        finalizers = try c.field(.finalizers, [])
        dependents = try c.field(.dependents, [])
        moreDependents = try c.field(.moreDependents, 0)
        resourceVersion = try c.field(.resourceVersion, "")
        deleting = try c.field(.deleting, false)
    }

    /// A cluster-scoped or protected object is deleted only once its name is typed.
    public var needsTypedName: Bool { isProtected || clusterScoped }
}

/// Whether a delete failed because the object changed since its preview (409 Conflict,
/// kube_delete.go deleteError): look again before deleting.
public func isKubeDeleteConflict(_ message: String) -> Bool {
    message.contains("the object changed since you looked at it")
}
