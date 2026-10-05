import Foundation
import IchorCore

/// The last Flux answer of the cluster on screen, shared by the Overview card, the Flux
/// screens and the app sheet of the Flux tile. Keyed on the context and the screenshot mode
/// generation (like ArgoCDStore), so another cluster's objects never show.
@Observable
@MainActor
final class FluxStore {
    static let shared = FluxStore()

    private(set) var status: FluxStatus?
    private var key = ""
    /// Until when the screens poll every 2 s whatever the objects say: right after an action,
    /// the controller takes a moment to pick the request up.
    private(set) var fastUntil = Date.distantPast

    /// What was loaded for key, nil when nothing (or for another cluster).
    func status(for key: String) -> FluxStatus? { key == self.key ? status : nil }

    /// Loads with client and keeps the answer for key.
    @discardableResult
    func load(with client: TalosClient, key: String) async throws -> FluxStatus {
        let loaded = try await client.flux()
        self.key = key
        status = loaded
        return loaded
    }

    /// Something reconciles, a requested reconcile waits, or an action was just sent: worth
    /// reading again in 2 s.
    func shouldPoll(for key: String) -> Bool {
        Date() < fastUntil || (status(for: key)?.anyBusy ?? false)
    }

    /// Runs action on target; nil on success, else the message to show.
    func run(_ action: FluxAction, on target: FluxRef, with client: TalosClient) async -> String? {
        do {
            try await client.fluxAction(action, on: target)
            fastUntil = Date().addingTimeInterval(10)
            return nil
        } catch {
            return "\(target.name): \(error.localizedDescription)"
        }
    }
}

extension AppModel {
    /// What Flux data belongs to: the context and the screenshot mode generation.
    var fluxKey: String { argoKey }
}
