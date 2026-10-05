import Foundation

// What the Argo CD screens compute from KubeArgoCD's answer (models in ArgoCD.swift): filters,
// grouping, what an app allows, the sync-wave timeline and the likely cause of a problem.

/// Whether the inventory shows Argo CD: only then is KubeArgoCD called.
public func argoCDHinted(_ inventory: ClusterInventory) -> Bool {
    inventory.apps.contains { $0.id == argoCDCatalogID }
}

public extension ArgoApp {
    /// No ApplicationSet or parent app rewrites its spec: auto-sync and rollback may change it.
    var canChangeSpec: Bool { owner == nil }
    /// A sync runs (or is being terminated).
    var isRunning: Bool { operation?.phase.active ?? false }
    /// A new sync would overwrite the running one: the Go core refuses it.
    var canSync: Bool { !isRunning }
    var canTerminate: Bool { operation?.phase == .running }
    /// Unowned, auto-sync paused (or the controller syncs straight back) and nothing running.
    var canRollback: Bool { canChangeSpec && !autoSync.enabled && !isRunning }
    var lastSyncFailed: Bool { operation?.phase.failed ?? false }

    /// "grafana@8.5.2" for a chart, the short commit otherwise.
    var revisionLabel: String {
        if let chart = sources.first?.chart, !chart.isEmpty {
            let version = revision.isEmpty ? sources.first?.targetRevision ?? "" : revision
            return version.isEmpty ? chart : "\(chart)@\(version)"
        }
        return shortRevision(revision)
    }

    /// When the current revision was deployed (unix ms), 0 when unknown.
    var deployedAt: Int64 { history.first?.deployedAt ?? 0 }

    /// What a sync with prune deletes.
    var pruneCandidates: [ArgoResource] { resources.filter(\.prune) }

    /// For AppIconView: its own icon, else the catalog icon the Go core matched, a monogram of the name otherwise.
    var iconApp: InventoryApp {
        InventoryApp(
            id: name, name: name, icon: icon.isEmpty ? nil : icon, remoteIcon: remoteIcon.isEmpty ? nil : remoteIcon,
            iconURL: iconURL.isEmpty ? nil : iconURL
        )
    }

    /// The likely cause of a problem app, most telling first: an unhealthy pod on a node that is
    /// down (downNodes: hostnames Talos reports not ready), else such a pod's status, else Argo
    /// CD's health message, the first failed resource or the operation's message, else the first
    /// condition. Nil for an app that needs no look, or with nothing to say.
    func likelyCause(downNodes: Set<String>) -> ArgoCause? {
        guard level.needsAttention else { return nil }
        let pods = unhealthyPods.filter { !$0.healthy }
        let unhealthy = health != .healthy && health != .suspended
        if unhealthy, let pod = pods.first(where: { downNodes.contains($0.node) }) {
            return .nodeDown(node: pod.node, pod: pod.name)
        }
        if unhealthy, let pod = pods.first(where: { !$0.transitional }) ?? pods.first {
            return .pod(name: pod.name, status: pod.status, node: pod.node)
        }
        if unhealthy, !healthMessage.isEmpty { return .message(healthMessage) }
        if let failed = operation?.failed.first(where: { !$0.message.isEmpty }) { return .message(failed.message) }
        if let op = operation, op.phase.failed, !op.message.isEmpty { return .message(op.message) }
        if let condition = conditions.first(where: { !$0.message.isEmpty }) { return .message(condition.message) }
        return nil
    }

    /// The resources grouped by sync wave, lowest first, each with where the sync stands: during
    /// a sync, waves below the current one are done; otherwise a wave is done when all its
    /// resources (hooks aside) are Synced. A wave with a resource the operation failed on is failed.
    var waveSteps: [ArgoWaveStep] {
        let failedIDs = Set((operation?.failed ?? []).map(\.id))
        let byWave = Dictionary(grouping: resources, by: \.wave)
        return byWave.keys.sorted().map { wave in
            let items = byWave[wave] ?? []
            let counted = items.filter { !$0.hook }
            let running = operation.map { $0.phase.active } ?? false
            let done = counted.filter { running ? $0.applied : $0.sync == .synced }.count
            let failed = operation != nil && items.contains { $0.syncFailed || failedIDs.contains("\($0.kind)/\($0.namespace)/\($0.name)") }
            let state: ArgoStepState
            if failed {
                state = .failed
            } else if let op = operation, running {
                state = wave < op.wave ? .done : wave == op.wave ? .current : .pending
            } else {
                state = done == counted.count ? .done : .pending
            }
            return ArgoWaveStep(wave: wave, resources: items, state: state, done: done, total: counted.count)
        }
    }
}

/// Why an app is in trouble, as the Overview and the app screen say it.
public enum ArgoCause: Equatable, Sendable {
    /// A pod of the app sits on a node Talos reports down.
    case nodeDown(node: String, pod: String)
    case pod(name: String, status: String, node: String)
    case message(String)
}

public enum ArgoStepState: Sendable {
    case done, current, pending, failed
}

/// One sync wave of the timeline.
public struct ArgoWaveStep: Equatable, Identifiable, Sendable {
    public let wave: Int
    public let resources: [ArgoResource]
    public let state: ArgoStepState
    /// Resources applied (during a sync) or Synced, of total; hooks are not counted.
    public let done: Int
    public let total: Int

    public var id: Int { wave }
    public var hasHooks: Bool { resources.contains(where: \.hook) }
}

/// The apps screen's filter chips.
public enum ArgoFilter: String, Sendable, CaseIterable, Hashable, Identifiable {
    case all, degraded, outOfSync, progressing, syncing, autoSyncOff, failed

    public var id: String { rawValue }

    public func matches(_ app: ArgoApp) -> Bool {
        switch self {
        case .all: true
        case .degraded: app.health.broken
        case .outOfSync: app.sync == .outOfSync
        case .progressing: app.health == .progressing
        case .syncing: app.isRunning
        case .autoSyncOff: !app.autoSync.enabled
        case .failed: app.lastSyncFailed
        }
    }
}

/// How the apps screen groups its rows.
public enum ArgoGrouping: String, Sendable, CaseIterable, Hashable, Identifiable {
    case none, project, appSet, namespace

    public var id: String { rawValue }
}

/// A titled run of apps; title "" holds the apps without one (no ApplicationSet).
public struct ArgoGroup: Equatable, Identifiable, Sendable {
    public let title: String
    public let apps: [ArgoApp]
    public var id: String { title }
}

/// Apps of filter whose name, project, namespaces, owner, chart or repo contains query
/// (case-insensitive), worst level first, then by name.
public func filterArgoApps(_ apps: [ArgoApp], filter: ArgoFilter, query: String) -> [ArgoApp] {
    let q = query.trimmingCharacters(in: .whitespaces)
    return apps
        .filter { app in
            guard filter.matches(app) else { return false }
            guard !q.isEmpty else { return true }
            let words = [app.name, app.project, app.namespace, app.destination.namespace, app.owner?.name ?? ""] +
                app.sources.flatMap { [$0.chart, $0.repo, $0.path] }
            return words.contains { $0.localizedCaseInsensitiveContains(q) }
        }
        .sorted(by: argoOrder)
}

/// Worst level first, then by name.
func argoOrder(_ a: ArgoApp, _ b: ArgoApp) -> Bool {
    a.level != b.level ? a.level < b.level : (a.name, a.namespace) < (b.name, b.namespace)
}

/// apps (kept in order) grouped; groups with the worst app first, then by title, the untitled
/// one last. .none gives a single untitled group.
public func groupArgoApps(_ apps: [ArgoApp], by grouping: ArgoGrouping) -> [ArgoGroup] {
    let key: (ArgoApp) -> String = switch grouping {
    case .none: { _ in "" }
    case .project: { $0.project }
    case .appSet: { $0.owner?.kind == "ApplicationSet" ? $0.owner?.name ?? "" : "" }
    case .namespace: { $0.destination.namespace }
    }
    var order: [String] = []
    var members: [String: [ArgoApp]] = [:]
    for app in apps {
        let k = key(app)
        if members[k] == nil { order.append(k) }
        members[k, default: []].append(app)
    }
    let worst = { (k: String) in ServiceHealth.worst(members[k, default: []].map(\.level)) }
    return order
        .sorted { a, b in
            if a.isEmpty != b.isEmpty { return b.isEmpty }
            return worst(a) != worst(b) ? worst(a) < worst(b) : a < b
        }
        .map { ArgoGroup(title: $0, apps: members[$0] ?? []) }
}

/// The apps "Sync all OutOfSync" syncs: drifting and not already syncing.
public func syncAllCandidates(_ apps: [ArgoApp]) -> [ArgoApp] {
    apps.filter { $0.sync == .outOfSync && $0.canSync }
}

public extension ArgoStatus {
    func count(_ filter: ArgoFilter) -> Int { apps.filter(filter.matches).count }

    /// Apps with a sync running.
    var running: [ArgoApp] { apps.filter(\.isRunning) }

    /// Apps that need a look, worst first.
    var problems: [ArgoApp] { apps.filter(\.level.needsAttention).sorted(by: argoOrder) }

    /// The worst level over every app (ok with none).
    var worst: ServiceHealth { .worst(apps.map(\.level)) }

    /// Apps per health, in the health bar's order, the empty ones left out.
    var healthCounts: [(health: ArgoHealth, count: Int)] {
        let order: [ArgoHealth] = [.healthy, .progressing, .suspended, .missing, .degraded, .unknown]
        return order.map { h in (h, apps.filter { $0.health == h }.count) }.filter { $0.count > 0 }
    }

    /// Everything is Synced and Healthy (or Suspended): the Overview stays calm.
    var allCalm: Bool { apps.allSatisfy { !$0.level.needsAttention } }

    /// The apps an ApplicationSet generated, worst first.
    func apps(of appSet: ArgoAppSet) -> [ArgoApp] {
        apps.filter { $0.owner?.kind == "ApplicationSet" && $0.owner?.name == appSet.name }.sorted(by: argoOrder)
    }

    func apps(of project: ArgoProject) -> [ArgoApp] { apps.filter { $0.project == project.name } }

    func app(namespace: String, name: String) -> ArgoApp? { apps.first { $0.namespace == namespace && $0.name == name } }
}

/// The Argo CD Applications deploying an inventory app, worst first. The first rule with a
/// match wins: the catalog icon the Go core gave the Application is the app's; then the
/// Application is named after the app (id or display name); then it deploys into one of the
/// app's namespaces (not for system apps, whose namespaces hold everything).
public func argoApps(for inv: InventoryApp, in status: ArgoStatus) -> [ArgoApp] {
    let byIcon = status.apps.filter { !$0.icon.isEmpty && ($0.icon == inv.id || $0.icon == inv.icon) }
    if !byIcon.isEmpty { return byIcon.sorted(by: argoOrder) }
    let byName = status.apps.filter { $0.name == inv.id || $0.name == inv.name.lowercased() }
    if !byName.isEmpty { return byName.sorted(by: argoOrder) }
    guard !inv.system else { return [] }
    return status.apps.filter { !$0.destination.namespace.isEmpty && inv.namespaces.contains($0.destination.namespace) }
        .sorted(by: argoOrder)
}

/// A tile badge: one of the app's Applications is critical or drifting.
public func argoNeedsBadge(_ apps: [ArgoApp]) -> Bool {
    apps.contains { $0.level == .critical || $0.sync == .outOfSync }
}
