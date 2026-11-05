import Foundation

/// What the search shows before anything is typed (same rule as Android's SearchBlankState.kt).
public enum SearchBlankBody: Equatable, Sendable {
    /// No cluster was ever read: open one first.
    case nothingKnown
    /// Clusters are known but nothing was searched or opened yet: what typing does, and screens to open.
    case firstUse
    /// The recently opened objects and recent searches.
    case recents
}

/// Recents win even over an empty snapshot: a recently opened object can still be opened.
public func searchBlankBody(snapshotEmpty: Bool, recentQueries: Int, openedTargets: Int) -> SearchBlankBody {
    if recentQueries > 0 || openedTargets > 0 { return .recents }
    return snapshotEmpty ? .nothingKnown : .firstUse
}

/// The cluster screens offered first, most used first; node and settings screens never are.
/// The ids are ScreenCatalog.swift's (and Android's ScreenCatalog.kt's).
public let firstUseShortcutIDs = ["nodes", "workloads", "health", "alerts", "argocd", "flux", "etcd", "events"]

/// The first `max` of `entries` in `firstUseShortcutIDs` order: only what the role's catalog allows.
public func firstUseShortcuts(_ entries: [ScreenEntry], max: Int = 4) -> [ScreenEntry] {
    let byID = Dictionary(entries.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
    return Array(firstUseShortcutIDs.compactMap { byID[$0] }.prefix(max))
}
