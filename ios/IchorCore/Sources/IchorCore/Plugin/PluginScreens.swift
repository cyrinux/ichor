import Foundation

/// The app screens a plugin may name with `screen: native:<route>`: only these, so a plugin
/// (one day a user's) can never send the app anywhere else. A data service's catalog id
/// ("longhorn") opens its Data services tab. Same names as Android's PluginRoutes.kt.
public enum PluginNativeScreen: Hashable, Sendable {
    case argoCD, flux, argoRollouts, policies, netpol, autoscaling, vms, crossplane, tailscale
    case clusterLifecycle, loadBalancing, storage, services, jobs, helm
    case dataService(DataServiceKind)

    private static let named: [String: PluginNativeScreen] = [
        "argocd": .argoCD, "flux": .flux, "argo-rollouts": .argoRollouts, "policies": .policies,
        "netpol": .netpol, "autoscaling": .autoscaling, "vms": .vms, "crossplane": .crossplane,
        "tailscale": .tailscale, "cluster-lifecycle": .clusterLifecycle, "load-balancing": .loadBalancing,
        "storage": .storage, "services": .services, "jobs": .jobs, "helm": .helm,
    ]

    /// The screen `screen` names ("native:argocd"); nil for "", the generic screen, or a name
    /// this build does not know.
    public init?(screen: String) {
        guard let name = nativePluginScreen(screen) else { return nil }
        if let known = Self.named[name] {
            self = known
        } else if let kind = DataServiceKind(rawValue: name) {
            self = .dataService(kind)
        } else {
            return nil
        }
    }
}

private let nativePrefix = "native:"

/// "argocd" for "native:argocd"; nil for "" or anything else.
public func nativePluginScreen(_ screen: String) -> String? {
    let trimmed = screen.trimmingCharacters(in: .whitespaces)
    guard trimmed.hasPrefix(nativePrefix) else { return nil }
    let name = trimmed.dropFirst(nativePrefix.count).trimmingCharacters(in: .whitespaces)
    return name.isEmpty ? nil : name
}

/// The family of the Kubernetes home's tools a plugin of another category joins; storage gets
/// none: data services plugins are tabs of the Data services screen instead.
public enum PluginToolFamily: Sendable, CaseIterable {
    case workloads, networking, security, browse

    public init(category: String) {
        switch category {
        case "workloads", "compute": self = .workloads
        case "networking": self = .networking
        case "policies", "security": self = .security
        default: self = .browse
        }
    }
}

/// A plugin tone as a health level: ok, idle, warning and critical as such; info reads calm
/// (idle), unknown and anything else unknown.
public func pluginHealth(_ tone: String) -> ServiceHealth {
    switch tone {
    case "ok": .ok
    case "idle", "info": .idle
    case "warning": .warning
    case "critical": .critical
    default: .unknown
    }
}

/// A tab of the Data services hub: a native system, or a CRD plugin drawn from its render tree.
public enum DataServicesTab: Hashable, Sendable, Identifiable {
    case native(DataServiceKind)
    case plugin(id: String, name: String)

    /// "longhorn", "plugin:kube-green".
    public var id: String {
        switch self {
        case .native(let kind): kind.rawValue
        case .plugin(let id, _): "plugin:\(id)"
        }
    }

    /// The native systems first, then one tab per plugin without a native screen.
    public static func tabs(detected: [DataServiceKind], plugins: PluginDetect?) -> [DataServicesTab] {
        detected.map { .native($0) } + (plugins?.dataServiceTabs ?? []).map { .plugin(id: $0.id, name: $0.name) }
    }

    /// The hub's tabs: `solo`'s alone when it runs; else, on a Kubernetes cluster (whose home
    /// files the other systems in their families), storage, databases and backups; on a Talos
    /// cluster, which has no tools card yet, every system. The plugins follow, except with `solo`.
    public static func hubTabs(_ services: DataServices, plugins: PluginDetect?, kube: Bool, solo: DataServiceKind?) -> [DataServicesTab] {
        if let solo { return services.detected.contains(solo) ? [.native(solo)] : [] }
        return tabs(detected: services.detected(kube ? [.data] : Set(ServiceFamily.allCases)), plugins: plugins)
    }
}

/// When a plugin tab reads again: a new `scope` (the cluster and its config generation) starts
/// over, so another cluster's rows never show; a new `bump` (a refresh, a change made from the
/// tab) reads in place, keeping the rows.
public struct PluginReload: Hashable, Sendable {
    public let scope: String
    public let bump: Int

    public init(scope: String, bump: Int) {
        self.scope = scope
        self.bump = bump
    }

    public enum Change: Equatable, Sendable { case none, reset, refresh }

    /// What moving from `previous` (nil: nothing loaded yet) to self asks for.
    public func change(from previous: PluginReload?) -> Change {
        guard let previous, previous.scope == scope else { return .reset }
        return previous.bump == bump ? .none : .refresh
    }
}

/// A line of the Data services card: a native system, or a CRD plugin with its summary tile.
public enum DataServicesCardLine: Equatable, Sendable, Identifiable {
    case native(DataServiceKind, health: ServiceHealth?)
    case plugin(PluginDetectItem)

    public var id: String {
        switch self {
        case .native(let kind, _): kind.rawValue
        case .plugin(let item): "plugin:\(item.id)"
        }
    }

    /// Nil when nothing says (a system not read yet, a plugin without its tile).
    public var health: ServiceHealth? {
        switch self {
        case .native(_, let health): health
        case .plugin(let item): item.summary?.serviceHealth
        }
    }

    /// Worst first (critical, warning, then the calm levels; no health counts as unknown), as on
    /// Android; equal ones keep their order, the native systems before the plugins.
    public static func lines(natives: [DataServiceKind], summary: (DataServiceKind) -> ServiceSummary?,
                             plugins: [PluginDetectItem]) -> [DataServicesCardLine] {
        let all = natives.map { DataServicesCardLine.native($0, health: summary($0)?.health) } + plugins.map { .plugin($0) }
        return all.enumerated()
            .sorted { a, b in
                let ha = a.element.health ?? .unknown
                let hb = b.element.health ?? .unknown
                return ha == hb ? a.offset < b.offset : ha < hb
            }
            .map(\.element)
    }

    /// "N need a look": what each native system counts, plus one per plugin whose tile needs
    /// attention (a tile has no count of its own).
    public static func attention(natives: [DataServiceKind], summary: (DataServiceKind) -> ServiceSummary?,
                                 plugins: [PluginDetectItem]) -> Int {
        natives.compactMap { summary($0)?.attention }.reduce(0, +)
            + plugins.filter { $0.summary?.serviceHealth.needsAttention == true }.count
    }
}

public extension DataServices {
    /// The card's lines: these systems and `plugins`, worst first.
    func cardLines(plugins: [PluginDetectItem]) -> [DataServicesCardLine] {
        DataServicesCardLine.lines(natives: detected, summary: { summary($0) }, plugins: plugins)
    }

    /// The card's "N need a look", plugins included.
    func attention(plugins: [PluginDetectItem]) -> Int {
        DataServicesCardLine.attention(natives: detected, summary: { summary($0) }, plugins: plugins)
    }
}
