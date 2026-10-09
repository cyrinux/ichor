import Foundation

// The namespace the Kubernetes lists are loaded for (the Linear plan document "U11. Large clusters: home at scale, namespace-first paged lists", L5, L6).
// Mirrors go/ichorgo/kube_namespaces.go; same rules as Android's model/KubeScope.kt.

/// The cluster's namespace names (KubeNamespaces).
public struct KubeNamespaces: Decodable, Equatable, Sendable {
    public let namespaces: [String]
    /// The credentials may not list namespaces: the user types one instead.
    public let forbidden: Bool
    /// The kubeconfig context's namespace, "" when it sets none.
    public let contextNamespace: String

    public init(namespaces: [String] = [], forbidden: Bool = false, contextNamespace: String = "") {
        self.namespaces = namespaces
        self.forbidden = forbidden
        self.contextNamespace = contextNamespace
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespaces = try c.field(.namespaces, [])
        forbidden = try c.field(.forbidden, false)
        contextNamespace = try c.field(.contextNamespace, "")
    }

    private enum CodingKeys: String, CodingKey { case namespaces, forbidden, contextNamespace }
}

/// The namespace the Kubernetes lists are loaded for: `namespace` nil for every one.
/// `chosen`: the user picked it (remembered per cluster), rather than the default.
public struct KubeScope: Equatable, Hashable, Sendable {
    public let namespace: String?
    public let chosen: Bool

    public init(namespace: String? = nil, chosen: Bool = false) {
        self.namespace = namespace
        self.chosen = chosen
    }

    /// How it is remembered: nil when not `chosen`, "" for every namespace.
    public var stored: String? { chosen ? (namespace ?? "") : nil }

    /// The scope remembered as `value` (see `stored`); the default when nil.
    public static func fromStored(_ value: String?) -> KubeScope {
        switch value {
        case nil: KubeScope()
        case ""?: KubeScope(chosen: true)
        case let namespace?: KubeScope(namespace: namespace, chosen: true)
        }
    }
}

/// The scope to load: the `remembered` one, else every namespace. When namespaces cannot be
/// listed, listing every namespace would be refused too: the kubeconfig context's namespace,
/// else nil: the user must type one.
public func defaultScope(remembered: KubeScope?, namespaces: KubeNamespaces?) -> KubeScope? {
    if let remembered { return remembered }
    guard let namespaces, namespaces.forbidden else { return KubeScope() }
    let context = namespaces.contextNamespace.trimmingCharacters(in: .whitespaces)
    return context.isEmpty ? nil : KubeScope(namespace: namespaces.contextNamespace)
}

/// Whether a Go core error is the API server refusing the credentials (403).
public func isKubeForbidden(_ message: String) -> Bool {
    message.hasPrefix("Kubernetes API: permission denied")
}

/// How many rows the eager load of `scope` reads (L5, L8). Every namespace by default stops
/// after its first page: when that page shows a large cluster, the user picks a namespace (or
/// chooses every namespace, then loaded up to eagerCap). A small cluster fits in that page.
public func eagerLimit(scope: KubeScope, metered: Bool) -> Int {
    scope.namespace == nil && !scope.chosen ? kubePageSize : eagerCap(metered: metered)
}

/// The namespaces to offer: the listed ones, else those of the loaded rows; `scope`'s always.
public func scopeChoices(listed: KubeNamespaces?, loaded: [String], scope: KubeScope) -> [String] {
    let base = listed.map(\.namespaces).flatMap { $0.isEmpty ? nil : $0 } ?? loaded
    return Array(Set(base + [scope.namespace].compactMap { $0 })).sorted()
}

/// Above this many namespaces the picker is a searchable sheet instead of chips.
public let scopeChipsMax = 15

/// `namespaces` containing `query` (case-insensitive).
public func matchingNamespaces(_ namespaces: [String], query: String) -> [String] {
    let needle = query.trimmingCharacters(in: .whitespaces)
    return needle.isEmpty ? namespaces : namespaces.filter { $0.range(of: needle, options: .caseInsensitive) != nil }
}

/// The scopes remembered per cluster (fingerprint -> `KubeScope.stored`) once `scope` is set
/// for `fingerprint`: one not chosen forgets it; a blank fingerprint changes nothing.
public func settingKubeScope(_ scope: KubeScope, for fingerprint: String, in scopes: [String: String]) -> [String: String] {
    guard !fingerprint.trimmingCharacters(in: .whitespaces).isEmpty else { return scopes }
    var next = scopes
    next[fingerprint] = scope.stored
    return next
}

// MARK: - Pages of the Go page functions

/// An item the Go core pages (KubePodsPage, KubeWorkloadsPage, KubeCronJobsPage), under `pageKey`.
public protocol KubePageItem: Decodable, Sendable {
    static var pageKey: String { get }
}

/// One page of `Item`s as the Go page functions answer, in the API server's order.
public struct KubePageWire<Item: KubePageItem>: Decodable, Sendable {
    public let items: [Item]
    public let continueToken: String
    public let remaining: Int64
    public let complete: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        items = try c.field(Key(Item.pageKey), [])
        continueToken = try c.field(Key("continue"), "")
        remaining = try c.field(Key("remaining"), -1)
        complete = try c.field(Key("complete"), true)
    }

    public var page: KubePage<Item> { page(detailed: true) }

    /// `detailed`: asked as full objects rather than a Table.
    public func page(detailed: Bool) -> KubePage<Item> {
        KubePage(items: items, continueToken: continueToken, remaining: remaining, complete: complete, detailed: detailed)
    }

    private struct Key: CodingKey {
        let stringValue: String
        var intValue: Int? { nil }
        init(_ name: String) { stringValue = name }
        init?(stringValue: String) { self.stringValue = stringValue }
        init?(intValue: Int) { nil }
    }
}

public typealias KubePodPage = KubePageWire<KubePod>
public typealias KubeWorkloadPage = KubePageWire<KubeWorkload>
public typealias KubeCronJobPage = KubePageWire<KubeCronJob>

extension KubePod: KubePageItem { public static let pageKey = "pods" }
extension KubeWorkload: KubePageItem { public static let pageKey = "workloads" }
extension KubeCronJob: KubePageItem { public static let pageKey = "cronJobs" }

public extension KubePageWire where Item == KubePod { var pods: [KubePod] { items } }
public extension KubePageWire where Item == KubeWorkload { var workloads: [KubeWorkload] { items } }
public extension KubePageWire where Item == KubeCronJob { var cronJobs: [KubeCronJob] { items } }

// MARK: - Workload kinds, paged side by side

/// The workload kinds the Go core pages, one list each.
public let workloadKinds = ["Deployment", "StatefulSet", "DaemonSet"]

/// The kinds still to load and their continue tokens, from a token `workloadToken` made: ""
/// (the first page) starts every kind. Go's tokens are hex, so '=' and ';' never occur in them.
public func parseWorkloadToken(_ token: String) -> [String: String] {
    if token.isEmpty { return Dictionary(uniqueKeysWithValues: workloadKinds.map { ($0, "") }) }
    var pending: [String: String] = [:]
    for part in token.split(separator: ";", omittingEmptySubsequences: true) {
        let pair = part.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
        guard pair.count == 2, workloadKinds.contains(String(pair[0])) else { continue }
        pending[String(pair[0])] = String(pair[1])
    }
    return pending
}

/// One token for the kinds still to load (each with its next page's token).
public func workloadToken(_ pending: [String: String]) -> String {
    workloadKinds.compactMap { kind in pending[kind].map { "\(kind)=\($0)" } }.joined(separator: ";")
}

/// The next page of every kind still to load, asked side by side through `fetch` and merged
/// into one page: complete once every kind is, its remaining count unknown while any kind's is.
public func fetchWorkloadPage(
    _ token: String,
    fetch: @escaping @Sendable (_ kind: String, _ token: String) async throws -> KubePage<KubeWorkload>
) async throws -> KubePage<KubeWorkload> {
    let pending = parseWorkloadToken(token)
    let pages = try await withThrowingTaskGroup(of: (String, KubePage<KubeWorkload>).self) { group in
        for (kind, kindToken) in pending {
            group.addTask { (kind, try await fetch(kind, kindToken)) }
        }
        var got: [String: KubePage<KubeWorkload>] = [:]
        for try await (kind, page) in group { got[kind] = page }
        return got
    }
    // In the kinds' order, whatever order they came back in.
    let ordered = workloadKinds.compactMap { kind in pages[kind].map { (kind, $0) } }
    let open = ordered.filter { !$0.1.complete }
    let next = Dictionary(uniqueKeysWithValues: open.map { ($0.0, $0.1.continueToken) })
    let remaining = open.map { $0.1.remaining }
    return KubePage(
        items: ordered.flatMap { $0.1.items },
        continueToken: workloadToken(next),
        remaining: remaining.contains { $0 < 0 } ? -1 : remaining.reduce(0, +),
        complete: next.isEmpty
    )
}
