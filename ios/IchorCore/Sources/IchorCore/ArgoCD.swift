import Foundation

// Mirrors go/ichorgo/kube_argocd.go and kube_argocd_actions.go (the wire format and the UX are
// described in plans/argocd/README.md). The logic on top lives in ArgoCDLogic.swift.

/// The catalog id of Argo CD in the inventory: only clusters running it are asked (KubeArgoCD).
public let argoCDCatalogID = "argo-cd"

/// The Argo CD Applications of every namespace, read through their custom resources (os:admin).
public struct ArgoStatus: Decodable, Equatable, Sendable {
    /// False when the cluster serves no argoproj.io Applications.
    public let installed: Bool
    /// The application controller's image tag, "" when unknown.
    public let version: String
    /// Listing the ApplicationSets failed; the apps still show.
    public let appSetsError: String
    /// Worst level first, then by name.
    public let apps: [ArgoApp]
    public let appSets: [ArgoAppSet]
    public let projects: [ArgoProject]

    public init(installed: Bool = true, version: String = "", appSetsError: String = "", apps: [ArgoApp] = [],
                appSets: [ArgoAppSet] = [], projects: [ArgoProject] = []) {
        self.installed = installed
        self.version = version
        self.appSetsError = appSetsError
        self.apps = apps
        self.appSets = appSets
        self.projects = projects
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        appSetsError = try c.field(.appSetsError, "")
        apps = try c.field(.apps, [])
        appSets = try c.field(.appSets, [])
        projects = try c.field(.projects, [])
    }

    private enum CodingKeys: String, CodingKey { case installed, version, appSetsError, apps, appSets, projects }
}

/// Argo CD's health of an app or a resource.
public enum ArgoHealth: String, Sendable, CaseIterable {
    case healthy = "Healthy"
    case progressing = "Progressing"
    case degraded = "Degraded"
    case suspended = "Suspended"
    case missing = "Missing"
    case unknown = "Unknown"

    public init(wire: String) { self = ArgoHealth(rawValue: wire) ?? .unknown }

    /// Broken: degraded, or a resource that should exist and does not.
    public var broken: Bool { self == .degraded || self == .missing }
}

/// Whether the live objects match Git.
public enum ArgoSyncState: String, Sendable, CaseIterable {
    case synced = "Synced"
    case outOfSync = "OutOfSync"
    case unknown = "Unknown"

    public init(wire: String) { self = ArgoSyncState(rawValue: wire) ?? .unknown }
}

/// Phase of a sync operation.
public enum ArgoPhase: String, Sendable {
    case running = "Running"
    case terminating = "Terminating"
    case succeeded = "Succeeded"
    case failed = "Failed"
    case error = "Error"
    case unknown = ""

    public init(wire: String) { self = ArgoPhase(rawValue: wire) ?? .unknown }

    /// Still going: the controller works on it (or is stopping it).
    public var active: Bool { self == .running || self == .terminating }
    public var failed: Bool { self == .failed || self == .error }
}

public struct ArgoApp: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let project: String
    /// The ApplicationSet or parent app that writes its spec: spec changes would be reverted.
    public let owner: ArgoOwner?
    /// Health, sync and the last operation summed up: critical, warning, ok or idle (suspended).
    public let level: ServiceHealth
    /// Bundled catalog icon, "" when none.
    public let icon: String
    /// Dashboard Icons slug, "" when none.
    public let remoteIcon: String
    /// Its own icon from its ichor.levis.name/icon annotation: an https URL or a data: URI, "" when none.
    public let iconURL: String
    public let health: ArgoHealth
    public let healthMessage: String
    public let sync: ArgoSyncState
    /// The revision synced: a commit, or a chart version.
    public let revision: String
    /// The pending refresh annotation: normal, hard or "".
    public let refreshing: String
    public let sources: [ArgoSource]
    public let destination: ArgoDestination
    public let autoSync: ArgoAutoSync
    public let syncOptions: [String]
    /// The running or last sync; nil when none ran since the app was created.
    public let operation: ArgoOperation?
    public let conditions: [ArgoCondition]
    /// By sync wave.
    public let resources: [ArgoResource]
    /// Newest first.
    public let history: [ArgoHistory]
    public let images: [String]
    public let externalURLs: [String]
    /// Pods of its destination namespace that are not ready (sent for unhealthy apps only).
    public let unhealthyPods: [KubePod]
    /// Unix ms.
    public let reconciledAt: Int64

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.decode(String.self, forKey: .name)
        project = try c.field(.project, "")
        owner = try c.decodeIfPresent(ArgoOwner.self, forKey: .owner)
        level = ServiceHealth(wire: try c.field(.level, ""))
        icon = try c.field(.icon, "")
        remoteIcon = try c.field(.remoteIcon, "")
        iconURL = try c.field(.iconURL, "")
        health = ArgoHealth(wire: try c.field(.health, ""))
        healthMessage = try c.field(.healthMessage, "")
        sync = ArgoSyncState(wire: try c.field(.sync, ""))
        revision = try c.field(.revision, "")
        refreshing = try c.field(.refreshing, "")
        sources = try c.field(.sources, [])
        destination = try c.field(.destination, ArgoDestination())
        autoSync = try c.field(.autoSync, ArgoAutoSync())
        syncOptions = try c.field(.syncOptions, [])
        operation = try c.decodeIfPresent(ArgoOperation.self, forKey: .operation)
        conditions = try c.field(.conditions, [])
        resources = try c.field(.resources, [])
        history = try c.field(.history, [])
        images = try c.field(.images, [])
        externalURLs = try c.field(.externalURLs, [])
        unhealthyPods = try c.field(.unhealthyPods, [])
        reconciledAt = try c.field(.reconciledAt, 0)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, project, owner, level, icon, remoteIcon, iconURL = "iconUrl", health, healthMessage, sync, revision, refreshing
        case sources, destination, autoSync, syncOptions, operation, conditions, resources, history, images, externalURLs
        case unhealthyPods, reconciledAt
    }
}

public struct ArgoOwner: Decodable, Equatable, Sendable {
    /// ApplicationSet or Application.
    public let kind: String
    public let name: String

    public init(kind: String, name: String) {
        self.kind = kind
        self.name = name
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        name = try c.field(.name, "")
    }

    private enum CodingKeys: String, CodingKey { case kind, name }
}

public struct ArgoSource: Decodable, Equatable, Sendable {
    public let repo: String
    /// "" for a Helm chart.
    public let path: String
    /// "" for a Git directory.
    public let chart: String
    public let targetRevision: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repo = try c.field(.repo, "")
        path = try c.field(.path, "")
        chart = try c.field(.chart, "")
        targetRevision = try c.field(.targetRevision, "")
    }

    private enum CodingKeys: String, CodingKey { case repo, path, chart, targetRevision }
}

public struct ArgoDestination: Decodable, Equatable, Sendable {
    public let server: String
    public let name: String
    public let namespace: String

    public init(server: String = "", name: String = "", namespace: String = "") {
        self.server = server
        self.name = name
        self.namespace = namespace
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        server = try c.field(.server, "")
        name = try c.field(.name, "")
        namespace = try c.field(.namespace, "")
    }

    private enum CodingKeys: String, CodingKey { case server, name, namespace }
}

public struct ArgoAutoSync: Decodable, Equatable, Sendable {
    public let enabled: Bool
    public let prune: Bool
    public let selfHeal: Bool

    public init(enabled: Bool = false, prune: Bool = false, selfHeal: Bool = false) {
        self.enabled = enabled
        self.prune = prune
        self.selfHeal = selfHeal
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        enabled = try c.field(.enabled, false)
        prune = try c.field(.prune, false)
        selfHeal = try c.field(.selfHeal, false)
    }

    private enum CodingKeys: String, CodingKey { case enabled, prune, selfHeal }
}

public struct ArgoOperation: Decodable, Equatable, Sendable {
    public let phase: ArgoPhase
    public let message: String
    /// Unix ms; finishedAt is 0 while running.
    public let startedAt: Int64
    public let finishedAt: Int64
    /// A user name, or "automated".
    public let initiatedBy: String
    public let revision: String
    public let retryCount: Int
    public let dryRun: Bool
    /// Resources synced so far, of total (hooks included once run).
    public let done: Int
    public let total: Int
    /// The lowest sync wave with resources not synced yet.
    public let wave: Int
    /// Every wave of the app, sorted.
    public let waves: [Int]
    public let failed: [ArgoResult]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = ArgoPhase(wire: try c.field(.phase, ""))
        message = try c.field(.message, "")
        startedAt = try c.field(.startedAt, 0)
        finishedAt = try c.field(.finishedAt, 0)
        initiatedBy = try c.field(.initiatedBy, "")
        revision = try c.field(.revision, "")
        retryCount = try c.field(.retryCount, 0)
        dryRun = try c.field(.dryRun, false)
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        wave = try c.field(.wave, 0)
        waves = try c.field(.waves, [])
        failed = try c.field(.failed, [])
    }

    private enum CodingKeys: String, CodingKey {
        case phase, message, startedAt, finishedAt, initiatedBy, revision, retryCount, dryRun, done, total, wave, waves, failed
    }
}

/// A resource the operation could not sync, with Argo CD's message.
public struct ArgoResult: Decodable, Equatable, Identifiable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let message: String

    public var id: String { "\(kind)/\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case kind, namespace, name, message }
}

public struct ArgoCondition: Decodable, Equatable, Sendable {
    /// SyncError, ComparisonError, OrphanedResourceWarning...
    public let type: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        message = try c.field(.message, "")
    }

    /// Argo CD's error types; the others (…Warning) are warnings.
    public var isError: Bool { ["ComparisonError", "InvalidSpecError", "SyncError", "UnknownError"].contains(type) }

    private enum CodingKeys: String, CodingKey { case type, message }
}

public struct ArgoResource: Decodable, Equatable, Identifiable, Sendable {
    public let group: String
    public let kind: String
    /// "" for cluster-scoped kinds.
    public let namespace: String
    public let name: String
    public let sync: ArgoSyncState
    /// Nil for kinds without a health check, and on Argo CD 3 by default.
    public let health: ArgoHealth?
    public let healthMessage: String
    public let wave: Int
    public let hook: Bool
    /// Would be deleted by a sync with prune.
    public let prune: Bool
    /// The last operation's word on it: Synced, SyncFailed, Pruned, PruneSkipped or "".
    public let syncResult: String

    public var id: String { "\(group)/\(kind)/\(namespace)/\(name)" }
    public var ref: ArgoResourceRef { ArgoResourceRef(group: group, kind: kind, namespace: namespace, name: name) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        sync = ArgoSyncState(wire: try c.field(.sync, ""))
        let healthWire: String = try c.field(.health, "")
        health = healthWire.isEmpty ? nil : ArgoHealth(wire: healthWire)
        healthMessage = try c.field(.healthMessage, "")
        wave = try c.field(.wave, 0)
        hook = try c.field(.hook, false)
        prune = try c.field(.prune, false)
        syncResult = try c.field(.syncResult, "")
    }

    /// The last operation applied (or pruned) it.
    public var applied: Bool { ["Synced", "Pruned", "PruneSkipped"].contains(syncResult) }
    public var syncFailed: Bool { syncResult == "SyncFailed" }

    private enum CodingKeys: String, CodingKey {
        case group, kind, namespace, name, sync, health, healthMessage, wave, hook, prune, syncResult
    }
}

/// One past deployment.
public struct ArgoHistory: Decodable, Equatable, Identifiable, Sendable {
    public let id: Int64
    public let revision: String
    public let targetRevision: String
    /// "" for a Git source.
    public let chart: String
    /// Unix ms.
    public let deployedAt: Int64
    /// A user name, or "automated".
    public let initiatedBy: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, 0)
        revision = try c.field(.revision, "")
        targetRevision = try c.field(.targetRevision, "")
        chart = try c.field(.chart, "")
        deployedAt = try c.field(.deployedAt, 0)
        initiatedBy = try c.field(.initiatedBy, "")
    }

    /// "grafana 8.5.2" for a chart, the short commit ("4be1d0c") otherwise.
    public var label: String { chart.isEmpty ? shortRevision(revision) : "\(chart) \(revision)" }

    private enum CodingKeys: String, CodingKey { case id, revision, targetRevision, chart, deployedAt, initiatedBy }
}

public struct ArgoAppSet: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Worst level of its apps, critical on an error condition.
    public let level: ServiceHealth
    public let apps: Int
    public let conditions: [ArgoAppSetCondition]

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.decode(String.self, forKey: .name)
        level = ServiceHealth(wire: try c.field(.level, ""))
        apps = try c.field(.apps, 0)
        conditions = try c.field(.conditions, [])
    }

    /// The generator's error ("ErrorOccurred" set), nil when it works.
    public var error: String? {
        conditions.first { $0.type == "ErrorOccurred" && $0.status == "True" }.map(\.message)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, level, apps, conditions }
}

public struct ArgoAppSetCondition: Decodable, Equatable, Sendable {
    public let type: String
    /// True, False or Unknown.
    public let status: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case type, status, message }
}

public struct ArgoProject: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let description: String
    /// How many sync windows it declares (not evaluated).
    public let syncWindows: Int

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.decode(String.self, forKey: .name)
        description = try c.field(.description, "")
        syncWindows = try c.field(.syncWindows, 0)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, description, syncWindows }
}

/// The actions KubeArgoAction runs.
public enum ArgoAction: String, Sendable, CaseIterable {
    case refresh, hardRefresh, sync, terminate, autoSyncOn, autoSyncOff, rollback
}

/// A resource to sync on its own (selective sync).
public struct ArgoResourceRef: Encodable, Equatable, Hashable, Sendable {
    public let group: String
    public let kind: String
    public let namespace: String
    public let name: String

    public init(group: String, kind: String, namespace: String, name: String) {
        self.group = group
        self.kind = kind
        self.namespace = namespace
        self.name = name
    }
}

/// The choices of the sync sheet, and the rollback target (KubeArgoAction's optionsJSON).
public struct ArgoSyncOptions: Encodable, Equatable, Sendable {
    public var prune = false
    public var dryRun = false
    public var force = false
    public var applyOutOfSyncOnly = false
    public var serverSideApply = false
    public var replace = false
    /// Empty: every resource.
    public var resources: [ArgoResourceRef] = []
    /// Rollback only: the history entry to go back to.
    public var historyId: Int64 = 0

    public init(prune: Bool = false, dryRun: Bool = false, force: Bool = false, applyOutOfSyncOnly: Bool = false,
                serverSideApply: Bool = false, replace: Bool = false, resources: [ArgoResourceRef] = [], historyId: Int64 = 0) {
        self.prune = prune
        self.dryRun = dryRun
        self.force = force
        self.applyOutOfSyncOnly = applyOutOfSyncOnly
        self.serverSideApply = serverSideApply
        self.replace = replace
        self.resources = resources
        self.historyId = historyId
    }

    /// The sheet's starting point: the app's own sync options, never prune (the user ticks it).
    public init(defaultsFor app: ArgoApp) {
        applyOutOfSyncOnly = app.syncOptions.contains("ApplyOutOfSyncOnly=true")
        serverSideApply = app.syncOptions.contains("ServerSideApply=true")
        replace = app.syncOptions.contains("Replace=true")
    }

    /// The JSON KubeArgoAction reads, keys sorted.
    public var json: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return (try? encoder.encode(self)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
    }
}

/// A 40-character commit as its first 7 characters; anything else (a chart version, a tag) as is.
public func shortRevision(_ revision: String) -> String {
    let hex = revision.count >= 12 && revision.allSatisfy(\.isHexDigit)
    return hex ? String(revision.prefix(7)) : revision
}
