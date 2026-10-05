import Foundation
import IchorCore
import Observation

/// Searches the networks around the phone for the nodes of the stored clusters (a talosconfig
/// shared without an endpoint reachable from here) and adds those that answer with a cluster's
/// credentials to its endpoints. Outlives its sheet: a search closed early still adds what it finds.
@Observable
@MainActor
final class EndpointScanJob {
    enum State {
        case idle
        case scanning(networks: [String])
        /// `matches` were added to their contexts.
        case done(matches: [EndpointMatch], networks: [String])
        case failed(String)
    }

    static let shared = EndpointScanJob()

    private(set) var state: State = .idle

    private init() {}

    var isScanning: Bool {
        if case .scanning = state { return true }
        return false
    }

    /// Searches the phone's networks and those around the stored clusters' endpoints and nodes.
    func start(model: AppModel) {
        guard !isScanning, let yaml = model.yaml else { return }
        let known = (model.summary?.contexts ?? []).flatMap { $0.endpoints + $0.nodes }
        let networks = scanNetworks(local: LocalNetwork.addresses(), known: known)
        state = .scanning(networks: networks)
        Task {
            do {
                let matches = try await TalosClient.findEndpoints(stored: yaml, networks: networks)
                if !matches.isEmpty { try await model.addFoundEndpoints(matches) }
                state = .done(matches: matches, networks: networks)
            } catch {
                state = .failed(error.localizedDescription)
            }
        }
    }

    /// Drops the result. A search under way keeps going: it adds what it finds.
    func reset() {
        if !isScanning { state = .idle }
    }
}
