import Foundation
import Observation
import IchorCore

/// The recent searches, newest first (a device preference, cleared with the last known state).
enum SearchRecents {
    private static let key = "search.recent"

    static func load() -> [String] { UserDefaults.standard.stringArray(forKey: key) ?? [] }
    static func save(_ recent: [String]) { UserDefaults.standard.set(recent, forKey: key) }
    static func clear() { UserDefaults.standard.removeObject(forKey: key) }
}

/// The search screen's state: the blobs to rank, built once when it opens from every cluster's
/// last known state (plus the active cluster's GitOps apps and what a refresh fetched), and the
/// results of the query, ranked by the Go core 150 ms after the last keystroke. Typing never
/// calls a cluster; only `refresh` does.
@Observable
@MainActor
final class SearchStore {
    private(set) var results: SearchResults = .empty
    /// The active cluster's screens that match the query (see screenCatalog).
    private(set) var screens: ScreenMatches = .empty
    /// The nodes the active cluster's overview lists, for its node screens.
    private(set) var nodes: [NodeOverview] = []
    /// The catalog the screens are ranked over, set by the view.
    private var catalog: [ScreenEntry] = []
    private(set) var recent: [String] = SearchRecents.load()
    /// The blobs are being read from the phone.
    private(set) var building = true
    private(set) var refreshing = false
    /// What a refresh could not fetch, nil when it all came.
    private(set) var refreshError: String?
    /// Nothing is known about any cluster (no blob at all).
    private(set) var nothingKnown = false

    private var entries: [SearchEntry] = []
    /// What a refresh fetched from the active cluster, by key.
    private var fetched: [String: SearchEntry] = [:]
    private var query = ""
    private var task: Task<Void, Never>?

    /// Reads every cluster's last known state off the main actor, then ranks the query again.
    func build(model: AppModel) async {
        building = true
        let contexts = model.summary?.contexts ?? []
        let active = model.activeContext
        let extra = activeEntries(model: model)
        let demo = contexts.contains(where: \.demo) ? TalosClient.demoSearchEntries() : []
        entries = await Task.detached(priority: .userInitiated) {
            searchEntries(contexts: contexts, activeContext: active, read: { LastKnownStore.read(fingerprint: $0, key: $1) }, active: extra) + demo
        }.value
        nodes = searchOverviewNodes(entries, context: active)
        nothingKnown = entries.isEmpty
        building = false
        run(query, debounce: false)
    }

    /// The active cluster's screens, titles localised: ranked with the query from now on.
    func setCatalog(_ screens: [ScreenEntry]) {
        catalog = screens
        run(query, debounce: false)
    }

    /// The query typed: ranked once typing pauses for 150 ms.
    func search(_ text: String) {
        query = text
        run(text, debounce: true)
    }

    private func run(_ text: String, debounce: Bool) {
        task?.cancel()
        guard !text.trimmingCharacters(in: .whitespaces).isEmpty else {
            results = .empty
            screens = .empty
            return
        }
        let entries = self.entries
        let catalog = self.catalog
        task = Task {
            if debounce { try? await Task.sleep(for: .milliseconds(150)) }
            guard !Task.isCancelled else { return }
            let ranked = (try? await TalosClient.search(entries: entries, query: text)) ?? .empty
            var matched = ScreenMatches.empty
            if !catalog.isEmpty, let found = try? await TalosClient.searchScreens(entries: catalog, query: text) { matched = found }
            guard !Task.isCancelled else { return }
            results = ranked
            screens = matched
        }
    }

    /// Keeps the query among the recent searches (on submit, or when a result is opened).
    func remember(_ text: String) {
        recent = searchRecents(adding: text, to: recent)
        SearchRecents.save(recent)
    }

    func forget(_ text: String) {
        recent.removeAll { $0 == text }
        SearchRecents.save(recent)
    }

    /// Fetches the active cluster's nodes, pods, workloads, CronJobs and GitOps apps again (what
    /// its role may read), then ranks the query over them.
    func refresh(model: AppModel) async {
        guard let client = model.client, let context = model.activeSummary, !context.demo else { return }
        refreshing = true
        refreshError = nil
        let cluster = searchCluster(of: context)
        var domains: [LastKnownDomain] = model.activeIsKube ? [] : [.overview]
        if model.allows(.workloads) { domains += [.pods, .workloads, .cronJobs] }
        for domain in domains {
            do {
                let json = try await client.rawJSON(domain)
                fetched[domain.key] = SearchEntry(cluster: cluster, context: context.name, key: domain.key, at: Date(), json: json)
            } catch {
                refreshError = error.localizedDescription
            }
        }
        if model.allows(.workloads) {
            // The stores keep them for the GitOps screens too.
            _ = try? await ArgoCDStore.shared.load(with: client, key: model.argoKey, cluster: context)
            _ = try? await FluxStore.shared.load(with: client, key: model.fluxKey)
        }
        refreshing = false
        await build(model: model)
    }

    /// The active cluster's in-memory data: its Argo CD and Flux apps, what a refresh fetched.
    private func activeEntries(model: AppModel) -> [SearchEntry] {
        guard let context = model.activeSummary, !context.demo else { return [] }
        let cluster = searchCluster(of: context)
        var out = Array(fetched.values).filter { $0.cluster == cluster }
        if let argo = ArgoCDStore.shared.status(for: model.argoKey), argo.installed {
            out.append(SearchEntry(cluster: cluster, context: context.name, key: "argocd", at: Date(), json: argo.searchJSON))
        }
        if let flux = FluxStore.shared.status(for: model.fluxKey), flux.installed {
            out.append(SearchEntry(cluster: cluster, context: context.name, key: "flux", at: Date(), json: flux.searchJSON))
        }
        return out.sorted { $0.key < $1.key }
    }
}
