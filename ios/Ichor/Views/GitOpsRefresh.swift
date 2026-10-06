import IchorCore
import SwiftUI

/// What the Argo CD and Flux stores share: the last status read, kept per cluster key.
@MainActor
protocol GitOpsStatusStore: AnyObject {
    associatedtype Status
    func status(for key: String) -> Status?
    func shouldPoll(for key: String) -> Bool
}

extension ArgoCDStore: GitOpsStatusStore {}
extension FluxStore: GitOpsStatusStore {}

extension GitOpsStatusStore {
    /// Reads the status for `key` into a screen's `state`. With `seed`, a screen still
    /// loading shows what the store already knows meanwhile. A failed read keeps the data on
    /// screen and notes the error; an answer for a cluster no longer on screen is dropped.
    func refresh(
        _ state: Binding<LoadState<Status>>, key: String, seed: Bool = true,
        currentKey: @MainActor () -> String, read: @MainActor () async throws -> Status
    ) async {
        if seed, let seeded = state.wrappedValue.seeded(with: status(for: key)) { state.wrappedValue = seeded }
        let loaded: LoadState<Status>
        do {
            loaded = .loaded(try await read(), at: Date())
        } catch {
            loaded = .failed(error.localizedDescription)
        }
        if let next = state.wrappedValue.refreshed(with: loaded, readFor: key, current: currentKey()) {
            state.wrappedValue = next
        }
    }
}

extension View {
    /// Loads for `key` (again whenever it changes), then reads again every 2 s while
    /// `shouldPoll` says something is in progress and the screen is not `paused`.
    func gitOpsPolling(
        key: String,
        paused: @escaping @MainActor @Sendable () -> Bool = { false },
        shouldPoll: @escaping @MainActor @Sendable () -> Bool,
        load: @escaping @MainActor @Sendable () async -> Void
    ) -> some View {
        task(id: key) {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                if Task.isCancelled { return }
                if await !paused(), await shouldPoll() { await load() }
            }
        }
    }
}
