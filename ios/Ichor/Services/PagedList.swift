import Foundation
import Observation
import IchorCore

/// A Kubernetes list of one scope, loaded page by page (same rules as Android's
/// PagedListViewModel): eagerly up to eagerLimit rows (fewer on an expensive
/// or constrained network), rows shown as they arrive while nothing else is on screen, then
/// further pages on demand (`loadMore`) in the server's order. A refresh keeps the rows on
/// screen until the new load completes; an expired list starts again silently; only a
/// complete list is kept as the last known one. Owned by the Kubernetes screen, so switching
/// tabs neither reloads nor stops a load.
@Observable
@MainActor
final class PagedList<T: Codable & Sendable> {
    /// One page of a scope's namespace (nil for every one), "" token for the first page.
    typealias Fetch = @Sendable (_ client: TalosClient, _ namespace: String?, _ token: String) async throws -> KubePage<T>

    private(set) var state: LoadState<PagedLoad<T>> = .loading
    /// The load in flight, for its progress bar; nil when none runs.
    private(set) var progress: PagedLoad<T>?
    /// The namespace listed; nil until the screen sets one (`show`).
    private(set) var scope: KubeScope?
    private(set) var loadingMore = false
    /// Bumped each time a load ended, well or not: what follows the rows live starts over then.
    private(set) var settles = 0

    /// The last known state key prefix ("pods"...), see kubeListKey.
    private let base: String
    private let fetch: Fetch
    /// Whether rows kept from an earlier load carry what only full objects do (PagedLoad.detailed).
    private let detailed: @Sendable ([T]) -> Bool
    /// The cluster, API address and screenshot mode the rows come from.
    private var source: String?
    /// Bumped by each load: the results of a superseded one are dropped.
    private var generation = 0
    private var task: Task<Void, Never>?

    /// Whether a complete list is kept as the last known state (and shown from it).
    private let persist: Bool
    /// Rows the first load reads before further pages wait for a scroll; nil for eagerLimit.
    private let eagerRows: Int?

    init(base: String, persist: Bool = true, eagerRows: Int? = nil,
         detailed: @escaping @Sendable ([T]) -> Bool = { _ in true }, fetch: @escaping Fetch) {
        self.base = base
        self.persist = persist
        self.eagerRows = eagerRows
        self.detailed = detailed
        self.fetch = fetch
    }

    /// Lists `scope` of the active cluster from now on: loads it the first time and when it or
    /// the cluster changes, else nothing.
    func show(_ scope: KubeScope, model: AppModel) async {
        let source = Self.source(of: model)
        guard scope != self.scope || source != self.source else { return }
        self.scope = scope
        self.source = source
        await reload(model: model, reset: true)
    }

    /// Loads the scope again, keeping the rows on screen until the new load completes.
    func refresh(model: AppModel) async {
        await reload(model: model, reset: false)
    }

    /// Replaces the rows on screen with `transform` of them (a change seen live), fresh as of
    /// now; nothing while none are. A load in flight replaces them when it completes.
    func apply(_ transform: (PagedLoad<T>) -> PagedLoad<T>) {
        guard case .loaded(let load, _, _) = state else { return }
        state = .loaded(transform(load), at: Date())
    }

    private static func source(of model: AppModel) -> String {
        "\(model.activeContext)|\(model.client?.kubeServer ?? "")|\(model.dataGeneration)"
    }

    private func reload(model: AppModel, reset: Bool) async {
        guard let client = model.client, let scope else { return }
        task?.cancel()
        generation += 1
        let mine = generation
        progress = nil
        loadingMore = false
        let key = kubeListKey(base, namespace: scope.namespace)
        if reset {
            state = .loading
            if persist, let known = model.lastKnown(key: key, as: [T].self) {
                state = .loaded(.complete(known.value, detailed: detailed(known.value)), at: known.at)
            }
        }
        let task = Task { await load(client: client, scope: scope, key: key, mine: mine, model: model) }
        self.task = task
        await task.value
    }

    private func load(client: TalosClient, scope: KubeScope, key: String, mine: Int, model: AppModel) async {
        let target = model.lastKnownTarget
        let fetch = self.fetch
        // Rows arrive as pages do only while nothing else is on screen (L12).
        let partial: Bool
        if case .loaded = state { partial = false } else { partial = true }
        do {
            let load = try await loadPages(
                cap: eagerRows ?? eagerLimit(scope: scope, metered: MeteredNetwork.shared.isMetered),
                fetch: { token in try await fetch(client, scope.namespace, token) },
                onProgress: { @MainActor step in
                    guard self.generation == mine else { return }
                    self.progress = step
                    if partial { self.state = .loaded(step, at: Date()) }
                }
            )
            guard generation == mine else { return }
            progress = nil
            state = .loaded(load, at: Date())
            settles += 1
            if load.done { keep(load.items, key: key, target: target, model: model) }
        } catch {
            guard generation == mine, !(error is CancellationError) else { return }
            progress = nil
            state = state.refreshed(with: .failed(error.localizedDescription))
            settles += 1
        }
    }

    /// Loads the next page of a list that stopped at its cap (scrolled to its end, or asked);
    /// every page left with `all`.
    func loadMore(model: AppModel, all: Bool = false) {
        guard case .loaded(let start, let at, _) = state, start.hasMore, !loadingMore, progress == nil,
              let client = model.client, let scope else { return }
        let mine = generation
        let key = kubeListKey(base, namespace: scope.namespace)
        let target = model.lastKnownTarget
        let fetch = self.fetch
        loadingMore = true
        Task {
            var shown = start
            do {
                repeat {
                    let next = try await shown.loadingMore { token in try await fetch(client, scope.namespace, token) }
                    // A refresh replaced the list meanwhile: it is the one to follow.
                    guard generation == mine else { return }
                    shown = next
                    state = .loaded(next, at: at)
                    if all { progress = next }
                } while all && shown.hasMore
                loadingMore = false
                progress = nil
                // Complete at last: the last known state from now on (L12).
                if shown.done { keep(shown.items, key: key, target: target, model: model) }
            } catch {
                guard generation == mine else { return }
                loadingMore = false
                progress = nil
                if isKubeListExpired(error) {
                    // An expired list starts again from its first page (L12).
                    await refresh(model: model)
                } else {
                    state = .loaded(shown, at: at, refreshError: error.localizedDescription)
                }
            }
        }
    }

    /// Keeps a complete list as the last known state, encoded off the main actor.
    private func keep(_ items: [T], key: String, target: LastKnownTarget?, model: AppModel) {
        guard persist, let target else { return }
        Task.detached(priority: .utility) {
            guard let data = try? JSONEncoder().encode(items) else { return }
            let json = String(decoding: data, as: UTF8.self)
            await model.remember(key: key, json: json, at: Date(), for: target)
        }
    }
}
