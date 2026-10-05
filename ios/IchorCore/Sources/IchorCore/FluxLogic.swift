import Foundation

// What the Flux screens compute from KubeFlux's answer (models in Flux.swift): the state of an
// object, filters and counts, what an object allows and when to read again.

/// Whether the inventory shows Flux: only then is KubeFlux called.
public func fluxHinted(_ inventory: ClusterInventory) -> Bool {
    inventory.apps.contains { $0.id == fluxCatalogID }
}

/// Where a Flux object stands, as the Overview counts it.
public enum FluxState: Sendable, CaseIterable {
    case ready, reconciling, suspended, failing

    /// suspended (idle), failing (critical), reconciling (warning: at it, waiting for a
    /// dependency, or a requested reconcile not handled yet), else ready.
    init(level: ServiceHealth, busy: Bool) {
        switch level {
        case .idle: self = .suspended
        case .critical: self = .failing
        case .warning, .unknown: self = .reconciling
        case .ok: self = busy ? .reconciling : .ready
        }
    }
}

public extension FluxApp {
    var isHelmRelease: Bool { kind == "HelmRelease" }
    var isKustomization: Bool { kind == "Kustomization" }
    /// The controller is at it, or a requested reconcile waits for it.
    var isBusy: Bool { reconciling || pending }
    var state: FluxState { FluxState(level: level, busy: isBusy) }
    /// A suspended object refuses everything but resume.
    var canReconcile: Bool { !suspended }
    var canReconcileWithSource: Bool { !suspended && source != nil }
    /// Force upgrade and reset failures: HelmReleases only.
    var canForce: Bool { isHelmRelease && !suspended }
    var canReset: Bool { isHelmRelease && !suspended }

    /// The revision tried last differs from the one applied: it is failing (or rolling) to it.
    var revisionDiffers: Bool { !attemptedRevision.isEmpty && attemptedRevision != revision }

    /// "podinfo@6.9.2" for a release, "main@4be1d0c" for a Git revision.
    var revisionLabel: String {
        if isHelmRelease {
            let version = revision.isEmpty ? chartVersion : revision
            return chart.isEmpty ? version : version.isEmpty ? chart : "\(chart)@\(version)"
        }
        return fluxShortRevision(revision)
    }

    /// "./apps/homelab" for a Kustomization, "chart@version" (the wanted one) for a release.
    var origin: String {
        if isHelmRelease {
            return chartVersion.isEmpty ? chart : "\(chart)@\(chartVersion)"
        }
        return path
    }

    /// One line on what is wrong (or going on): the Ready message, else its reason.
    var summary: String { message.isEmpty ? reason : message }

    /// For AppIconView: the catalog icon the Go core matched, a monogram of the name otherwise.
    var iconApp: InventoryApp {
        InventoryApp(id: name, name: name, icon: icon.isEmpty ? nil : icon, remoteIcon: remoteIcon.isEmpty ? nil : remoteIcon)
    }

    /// The Kustomization's inventory by kind, kinds and names sorted.
    var resourcesByKind: [FluxResourceGroup] {
        Dictionary(grouping: resources, by: \.kind)
            .map { kind, items in
                FluxResourceGroup(kind: kind, resources: items.sorted { ($0.namespace, $0.name) < ($1.namespace, $1.name) })
            }
            .sorted { $0.kind < $1.kind }
    }
}

/// The resources of one kind a Kustomization applied.
public struct FluxResourceGroup: Equatable, Identifiable, Sendable {
    public let kind: String
    public let resources: [FluxResource]
    public var id: String { kind }
}

public extension FluxSource {
    var isBusy: Bool { reconciling || pending }
    var state: FluxState { FluxState(level: level, busy: isBusy) }
    var canReconcile: Bool { !suspended }
    /// "main@4be1d0c", "6.9.2@3b1f9c0".
    var revisionLabel: String { fluxShortRevision(revision) }
}

/// A Flux revision with its hashes cut to 7 characters: "main@sha1:4be1d0c9f2…" is
/// "main@4be1d0c", "sha256:7e1a0f4c2b9d" is "7e1a0f4"; a chart version stays as is.
public func fluxShortRevision(_ revision: String) -> String {
    revision.split(separator: "@", omittingEmptySubsequences: false)
        .map { part in
            let hash = part.split(separator: ":", maxSplits: 1).last.map(String.init) ?? ""
            let digest = part.hasPrefix("sha1:") || part.hasPrefix("sha256:") || part.hasPrefix("sha384:") || part.hasPrefix("sha512:")
            return shortRevision(digest ? hash : String(part))
        }
        .joined(separator: "@")
}

/// The apps screen's filter chips.
public enum FluxFilter: String, Sendable, CaseIterable, Hashable, Identifiable {
    case all, failing, reconciling, suspended, kustomizations, helmReleases

    public var id: String { rawValue }

    public func matches(_ app: FluxApp) -> Bool {
        switch self {
        case .all: true
        case .failing: app.state == .failing
        case .reconciling: app.state == .reconciling
        case .suspended: app.suspended
        case .kustomizations: app.isKustomization
        case .helmReleases: app.isHelmRelease
        }
    }
}

/// Apps of filter whose name, namespace, kind, chart, path, source or its URL contains query
/// (case-insensitive), worst level first, then by name.
public func filterFluxApps(_ apps: [FluxApp], filter: FluxFilter, query: String) -> [FluxApp] {
    let q = query.trimmingCharacters(in: .whitespaces)
    return apps
        .filter { app in
            guard filter.matches(app) else { return false }
            guard !q.isEmpty else { return true }
            let words = [app.name, app.namespace, app.kind, app.chart, app.path, app.sourceURL, app.source?.name ?? ""]
            return words.contains { $0.localizedCaseInsensitiveContains(q) }
        }
        .sorted(by: fluxOrder)
}

/// Sources whose name, namespace, kind or URL contains query, worst level first, then by name.
public func filterFluxSources(_ sources: [FluxSource], query: String) -> [FluxSource] {
    let q = query.trimmingCharacters(in: .whitespaces)
    return sources
        .filter { s in q.isEmpty || [s.name, s.namespace, s.kind, s.url].contains { $0.localizedCaseInsensitiveContains(q) } }
        .sorted { a, b in a.level != b.level ? a.level < b.level : (a.name, a.namespace, a.kind) < (b.name, b.namespace, b.kind) }
}

/// Worst level first, then by name.
func fluxOrder(_ a: FluxApp, _ b: FluxApp) -> Bool {
    a.level != b.level ? a.level < b.level : (a.name, a.namespace, a.kind) < (b.name, b.namespace, b.kind)
}

public extension FluxStatus {
    func count(_ filter: FluxFilter) -> Int { apps.filter(filter.matches).count }

    /// Apps per state, in the health bar's order, the empty ones left out.
    var stateCounts: [(state: FluxState, count: Int)] {
        FluxState.allCases.map { s in (s, apps.filter { $0.state == s }.count) }.filter { $0.count > 0 }
    }

    /// Failing apps, worst first.
    var failing: [FluxApp] { apps.filter { $0.state == .failing }.sorted(by: fluxOrder) }

    /// The worst level over every app and source (ok with none).
    var worst: ServiceHealth { .worst(apps.map(\.level) + sources.map(\.level)) }

    /// Every app is ready (or suspended): the Overview stays calm.
    var allCalm: Bool { apps.allSatisfy { $0.state == .ready || $0.state == .suspended } }

    /// An app or source is reconciling, or a requested reconcile waits: worth reading again soon.
    var anyBusy: Bool { apps.contains(where: \.isBusy) || sources.contains(where: \.isBusy) }

    /// Sources that cannot be fetched.
    var failingSources: [FluxSource] { sources.filter { $0.state == .failing } }

    func app(kind: String, namespace: String, name: String) -> FluxApp? {
        apps.first { $0.kind == kind && $0.namespace == namespace && $0.name == name }
    }

    /// The source an app is built from, nil when not listed.
    func source(of app: FluxApp) -> FluxSource? {
        guard let ref = app.source else { return nil }
        return sources.first { $0.target == ref }
    }

    /// The apps a Kustomization applies from Git, worst first.
    func children(of app: FluxApp) -> [FluxApp] {
        guard app.isKustomization else { return [] }
        return apps.filter { $0.owner == app.target }.sorted(by: fluxOrder)
    }
}
