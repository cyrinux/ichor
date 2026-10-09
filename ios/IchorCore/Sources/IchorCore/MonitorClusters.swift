import Foundation

// The background monitor checks every stored cluster (CYR-37), one context each, keeps a
// snapshot per cluster, and can say when a cluster stops answering. Same rules as Android.

/// What a cluster's monitor state (its snapshot, its unreachable count, its widget entry) is
/// kept under: its `clusterID`, the same for each of its contexts; else the context's fingerprint,
/// else its name.
public func monitorClusterKey(_ context: ContextSummary) -> String {
    if !context.clusterID.isEmpty { return context.clusterID }
    return context.fingerprint.isEmpty ? "name:\(context.name)" : context.fingerprint
}

/// One context per cluster, in the stored order: the active one (`active`, a context name) when
/// it is of that cluster, else the cluster's first.
public func clusterContexts(_ contexts: [ContextSummary], active: String?) -> [ContextSummary] {
    var order: [String] = []
    var chosen: [String: ContextSummary] = [:]
    for context in contexts {
        let key = monitorClusterKey(context)
        if chosen[key] == nil {
            order.append(key)
            chosen[key] = context
        } else if context.name == active {
            chosen[key] = context
        }
    }
    return order.compactMap { chosen[$0] }
}

/// Whether `context`'s cluster is watched in the background: on unless one of its contexts was
/// turned off (`unwatched`, by fingerprint).
public func watchedInBackground(_ context: ContextSummary, contexts: [ContextSummary], unwatched: Set<String>) -> Bool {
    let key = monitorClusterKey(context)
    return !contexts.contains { monitorClusterKey($0) == key && unwatched.contains($0.fingerprint) }
}

/// `unwatched` once `context`'s cluster is watched (`on`: none of its contexts left in it) or not.
public func settingWatched(_ on: Bool, for context: ContextSummary, contexts: [ContextSummary],
                           unwatched: Set<String>) -> Set<String> {
    guard !context.fingerprint.isEmpty else { return unwatched }
    guard on else { return unwatched.union([context.fingerprint]) }
    let key = monitorClusterKey(context)
    let ofCluster = contexts.filter { monitorClusterKey($0) == key }.map(\.fingerprint)
    return unwatched.subtracting(ofCluster + [context.fingerprint])
}

/// The contexts a background run checks: one per cluster (clusterContexts), but not the
/// clusters turned off.
public func monitoredContexts(_ contexts: [ContextSummary], active: String?, unwatched: Set<String>) -> [ContextSummary] {
    clusterContexts(contexts, active: active).filter { watchedInBackground($0, contexts: contexts, unwatched: unwatched) }
}

// MARK: - Snapshots per cluster

/// `snapshots` (by monitor key) with the single snapshot older versions kept (`legacy`) given
/// to the active cluster, so the first run after the update compares it instead of taking a
/// silent baseline. Unchanged when that cluster already has one.
public func migratedSnapshots(_ snapshots: [String: ClusterSnapshot], legacy: ClusterSnapshot?,
                              activeCluster: String?) -> [String: ClusterSnapshot] {
    guard let legacy, let activeCluster, !activeCluster.isEmpty, snapshots[activeCluster] == nil else { return snapshots }
    var migrated = snapshots
    migrated[activeCluster] = legacy
    return migrated
}

/// The entries of `map` whose cluster is still stored (`clusters`, monitor keys): a removed
/// cluster's snapshot and unreachable count go with it.
public func keepingClusters<Value>(_ map: [String: Value], clusters: [String]) -> [String: Value] {
    let kept = Set(clusters)
    return map.filter { kept.contains($0.key) }
}

/// A cluster on the widget's list: its monitor key, how the app names it, and the link a widget
/// set to it opens (ichor://open?…, the cluster's screen; nil without a `clusterID`).
public struct WidgetCluster: Codable, Equatable, Hashable, Sendable {
    public let id: String
    public let name: String
    public var link: String?

    public init(id: String, name: String, link: String? = nil) {
        self.id = id
        self.name = name
        self.link = link
    }
}

/// The clusters the widget can show, one per cluster, named as on screen; `link` makes the
/// share link of a context's cluster (the app builds it through Go).
public func widgetClusters(_ contexts: [ContextSummary], active: String?, labels: ClusterLabels,
                           link: (ContextSummary) -> String? = { _ in nil }) -> [WidgetCluster] {
    clusterContexts(contexts, active: active).map { WidgetCluster(id: monitorClusterKey($0), name: labels.of($0), link: link($0)) }
}

/// The in-app form (ichor://open?…) of a web share link (https://…/open/#…): what a widget tap
/// opens, the app routing it like any share link. Nil for a link without a fragment.
public func appShareLink(web link: String) -> String? {
    guard let fragment = URLComponents(string: link)?.percentEncodedFragment, !fragment.isEmpty else { return nil }
    return "ichor://open?\(fragment)"
}

extension SharedSnapshot {
    /// Every cluster's snapshot, by monitor key (CYR-37); `key` holds older versions' single one.
    public static let snapshotsKey = "snapshots"
    /// The clusters the widget can pick ([WidgetCluster]) and the active one's key.
    public static let clustersKey = "widgetClusters"
    public static let activeKey = "activeCluster"

    public static func loadAll(from defaults: UserDefaults?) -> [String: ClusterSnapshot] {
        defaults?.data(forKey: snapshotsKey).flatMap { try? JSONDecoder().decode([String: ClusterSnapshot].self, from: $0) } ?? [:]
    }

    public static func clusters(from defaults: UserDefaults?) -> [WidgetCluster] {
        defaults?.data(forKey: clustersKey).flatMap { try? JSONDecoder().decode([WidgetCluster].self, from: $0) } ?? []
    }

    public static func activeCluster(from defaults: UserDefaults?) -> String? {
        defaults?.string(forKey: activeKey)
    }

    /// The snapshot of `cluster` (nil: the active one); before the first run of this version,
    /// the active cluster has the single snapshot older versions kept (also shown before the app,
    /// updated, said which cluster is active).
    public static func load(from defaults: UserDefaults?, cluster: String?) -> ClusterSnapshot? {
        let active = activeCluster(from: defaults)
        if cluster == nil && active == nil { return load(from: defaults) }
        let all = migratedSnapshots(loadAll(from: defaults), legacy: load(from: defaults), activeCluster: active)
        return (cluster ?? active).flatMap { all[$0] }
    }
}

// MARK: - Unreachable for N runs in a row

/// How many runs in a row a cluster may not answer before the opt-in "unreachable" alert, and
/// the range the setting offers.
public let unreachableRunsDefault = 3
public let unreachableRunsRange = 2...10

/// A cluster's runs without an answer in a row, and whether the "unreachable" alert went out.
public struct Reachability: Codable, Equatable, Sendable {
    public var misses: Int
    public var alerted: Bool

    public init(misses: Int = 0, alerted: Bool = false) {
        self.misses = misses
        self.alerted = alerted
    }
}

/// One run's answer (`reachable`) counted: the "unreachable" alert once the cluster missed
/// `runs` in a row (clamped to unreachableRunsRange), and "reachable again" once when it answers
/// after it. `enabled` off counts without alerting. A run that did not try the cluster (waiting
/// for its VPN) is not counted at all: callers skip it.
public func evaluateReachability(previous: Reachability?, reachable: Bool, runs: Int,
                                 enabled: Bool) -> (alert: Alert?, next: Reachability) {
    let before = previous ?? Reachability()
    if reachable {
        let alert = before.alerted && enabled
            ? Alert(key: "unreachable", title: "Cluster reachable again", text: "It answers the background checks again.", problem: false)
            : nil
        return (alert, Reachability())
    }
    let threshold = min(max(runs, unreachableRunsRange.lowerBound), unreachableRunsRange.upperBound)
    var next = Reachability(misses: before.misses + 1, alerted: before.alerted)
    guard enabled, !before.alerted, next.misses >= threshold else { return (nil, next) }
    next.alerted = true
    return (Alert(key: "unreachable", title: "Cluster unreachable", text: "No answer to the last \(threshold) checks.", problem: true), next)
}

// MARK: - Budget

/// iOS gives a background refresh about 30 s: a run stays within this, checking at most
/// `monitorParallel` clusters at a time.
public let monitorBudget: TimeInterval = 25
public let monitorParallel = 4

/// How long one cluster may take: the budget split over the waves of `monitorParallel`, between
/// 8 s and 20 s (a Go call gives up after 20 s). Past that it counts as unreachable for the run.
public func monitorClusterTimeout(clusters: Int, budget: TimeInterval = monitorBudget, parallel: Int = monitorParallel) -> TimeInterval {
    let width = max(1, parallel)
    let waves = max(1, (clusters + width - 1) / width)
    return min(20, max(8, budget / Double(waves)))
}

/// `work`'s result, or nil once `seconds` passed or the calling task was cancelled. Unlike a
/// task group it never waits for work that ignores cancellation (a blocking Go call): that work
/// ends in the background and its result is dropped.
public func withDeadline<T: Sendable>(_ seconds: TimeInterval, _ work: @escaping @Sendable () async -> T?) async -> T? {
    let gate = FirstResult<T>()
    return await withTaskCancellationHandler {
        await withCheckedContinuation { (continuation: CheckedContinuation<T?, Never>) in
            gate.wait(continuation)
            let worker = Task { gate.finish(await work()) }
            Task {
                try? await Task.sleep(nanoseconds: UInt64(max(0, seconds) * 1_000_000_000))
                worker.cancel()
                gate.finish(nil)
            }
        }
    } onCancel: {
        gate.finish(nil)
    }
}

/// Resumes a continuation with the first result given, once, whichever comes first.
private final class FirstResult<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<T?, Never>?
    private var done = false
    private var value: T?

    func wait(_ continuation: CheckedContinuation<T?, Never>) {
        lock.lock()
        if done {
            let value = value
            lock.unlock()
            continuation.resume(returning: value)
            return
        }
        self.continuation = continuation
        lock.unlock()
    }

    func finish(_ result: T?) {
        lock.lock()
        guard !done else {
            lock.unlock()
            return
        }
        done = true
        value = result
        let waiting = continuation
        continuation = nil
        lock.unlock()
        waiting?.resume(returning: result)
    }
}
