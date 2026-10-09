import Foundation

// Kubernetes lists loaded page by page (the Linear plan document "U11. Large clusters: home at scale, namespace-first paged lists", L7, L8, L12, L13).
// Same rules as Android's model/PagedLoad.kt.

/// One page of a Kubernetes list from a Go page function (KubePodsPage...): `continueToken`
/// asks for the next one, `remaining` is how many items the next pages hold (-1 when the API
/// server does not say). `detailed`: full objects, not Table rows (pods: images, containers).
public struct KubePage<T> {
    public let items: [T]
    public let continueToken: String
    public let remaining: Int64
    public let complete: Bool
    public let detailed: Bool

    public init(items: [T], continueToken: String = "", remaining: Int64 = -1, complete: Bool = true, detailed: Bool = true) {
        self.items = items
        self.continueToken = continueToken
        self.remaining = remaining
        self.complete = complete
        self.detailed = detailed
    }
}

extension KubePage: Sendable where T: Sendable {}
extension KubePage: Equatable where T: Equatable {}

/// Prefix of the Go core's message for a 410 Gone (kube_client.go kubeListExpired).
public let kubeListExpired = "Kubernetes API: list expired"

/// The API server expired the list being paged (its continue token is ~5 minutes old): load it again.
public func isKubeListExpired(_ message: String) -> Bool {
    message.contains(kubeListExpired)
}

/// Same, for an error thrown by a page call.
public func isKubeListExpired(_ error: Error) -> Bool {
    isKubeListExpired((error as? LocalizedError)?.errorDescription ?? error.localizedDescription) ||
        isKubeListExpired(String(describing: error))
}

/// Rows a page function is asked for.
public let kubePageSize = 500

/// Rows the eager load reads before it stops (L8): fewer on an expensive or constrained network.
public func eagerCap(metered: Bool) -> Int {
    metered ? 5_000 : 10_000
}

/// A Kubernetes list loaded page by page: `items` in the API server's order (namespace, then
/// name), all of them once `done`. A load that reached its cap stops `capped`; further pages
/// then come on scroll (`loadMore`).
public struct PagedLoad<T> {
    public private(set) var items: [T]
    /// Asks for the next page; "" before the first one and once `done`.
    public private(set) var continueToken: String
    /// Items the server says remain after `items`, -1 when unknown.
    public private(set) var remaining: Int64
    public private(set) var pages: Int
    public private(set) var done: Bool
    public var capped: Bool
    /// Every page held full objects: what only they carry (images) can be searched.
    public private(set) var detailed: Bool

    public init(items: [T] = [], continueToken: String = "", remaining: Int64 = -1, pages: Int = 0,
                done: Bool = false, capped: Bool = false, detailed: Bool = true) {
        self.items = items
        self.continueToken = continueToken
        self.remaining = remaining
        self.pages = pages
        self.done = done
        self.capped = capped
        self.detailed = detailed
    }

    /// A list read whole (the last known one, kept only when complete); see `detailed`.
    public static func complete(_ items: [T], detailed: Bool = true) -> PagedLoad<T> {
        PagedLoad(items: items, remaining: 0, pages: 1, done: true, detailed: detailed)
    }

    /// More pages to load: the load stopped at its cap.
    public var hasMore: Bool { pages > 0 && !done }

    /// This load with `items` in place of its rows (a change seen live); the pages stay as they are.
    public func replacing(items: [T]) -> PagedLoad<T> {
        var load = self
        load.items = items
        return load
    }

    /// Loaded plus remaining rows; nil while the server does not say.
    public var estimatedTotal: Int64? {
        if done { return Int64(items.count) }
        return remaining >= 0 ? Int64(items.count) + remaining : nil
    }

    public func appending(_ page: KubePage<T>) -> PagedLoad<T> {
        PagedLoad(
            items: items + page.items,
            continueToken: page.complete ? "" : page.continueToken,
            remaining: page.complete ? 0 : page.remaining,
            pages: pages + 1,
            done: page.complete,
            capped: capped,
            detailed: detailed && page.detailed
        )
    }

    /// The next page, for a load that stopped at its cap; `self` once done.
    public func loadingMore(_ fetch: (String) async throws -> KubePage<T>) async throws -> PagedLoad<T> {
        hasMore ? appending(try await fetch(continueToken)) : self
    }
}

extension PagedLoad: Sendable where T: Sendable {}
extension PagedLoad: Equatable where T: Equatable {}

/// Loads every page through `fetch` ("" for the first page), reporting each step to
/// `onProgress`, until the list is done or holds `cap` rows (then marked capped). When the API
/// server expires the list mid-way it starts again from the first page, silently, up to
/// `maxRestarts` times (L12). Other errors, and cancellation, propagate.
public func loadPages<T>(
    cap: Int,
    maxRestarts: Int = 3,
    fetch: (String) async throws -> KubePage<T>,
    onProgress: (PagedLoad<T>) async -> Void = { _ in }
) async throws -> PagedLoad<T> {
    var restarts = 0
    while true {
        do {
            return try await loadOnce(cap: cap, fetch: fetch, onProgress: onProgress)
        } catch {
            if error is CancellationError || !isKubeListExpired(error) || restarts >= maxRestarts { throw error }
            restarts += 1
        }
    }
}

private func loadOnce<T>(cap: Int, fetch: (String) async throws -> KubePage<T>,
                         onProgress: (PagedLoad<T>) async -> Void) async throws -> PagedLoad<T> {
    var load = PagedLoad<T>()
    repeat {
        try Task.checkCancellation()
        load = load.appending(try await fetch(load.continueToken))
        if !load.done && load.items.count >= cap { load.capped = true }
        await onProgress(load)
    } while !load.done && !load.capped
    return load
}

/// The last known state key of a list of `base` ("pods", "workloads", "cronjobs") loaded for
/// `namespace` (nil for every one): kept per scope, as Android's podsKey.
public func kubeListKey(_ base: String, namespace: String?) -> String {
    "\(base)|\(namespace ?? "*")"
}
