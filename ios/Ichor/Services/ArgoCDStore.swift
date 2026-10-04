import Foundation
import IchorCore

/// The last Argo CD answer of the cluster on screen, shared by the Overview card, the Argo CD
/// screens and the Apps grid (which only shows badges once something else loaded it). Keyed
/// on the context and the screenshot mode generation, so another cluster's apps never show.
@Observable
@MainActor
final class ArgoCDStore {
    static let shared = ArgoCDStore()

    private(set) var status: ArgoStatus?
    private var key = ""
    /// Until when the screens poll every 2 s whatever the apps say: right after an action, the
    /// controller takes a moment to start the operation.
    private(set) var fastUntil = Date.distantPast

    /// What was loaded for key, nil when nothing (or for another cluster).
    func status(for key: String) -> ArgoStatus? { key == self.key ? status : nil }

    /// Loads with client and keeps the answer for key.
    @discardableResult
    func load(with client: TalosClient, key: String) async throws -> ArgoStatus {
        let loaded = try await client.argoCD()
        self.key = key
        status = loaded
        return loaded
    }

    /// Something runs, or an action was just sent: worth reading again in 2 s.
    func shouldPoll(for key: String) -> Bool {
        Date() < fastUntil || (status(for: key)?.running.isEmpty == false)
    }

    /// Runs action on app; nil on success, else the message to show.
    func run(_ action: ArgoAction, on app: ArgoApp, options: ArgoSyncOptions? = nil, with client: TalosClient) async -> String? {
        do {
            try await client.argoAction(namespace: app.namespace, name: app.name, action: action, options: options)
            fastUntil = Date().addingTimeInterval(10)
            return nil
        } catch {
            return "\(app.name): \(error.localizedDescription)"
        }
    }

    /// Runs action on every app in turn (a sync with each app's own options, never prune); nil
    /// when all went through, else one line per failure.
    func run(_ action: ArgoAction, on apps: [ArgoApp], with client: TalosClient) async -> String? {
        var failures: [String] = []
        for app in apps {
            let options = action == .sync ? ArgoSyncOptions(defaultsFor: app) : nil
            if let failure = await run(action, on: app, options: options, with: client) { failures.append(failure) }
        }
        return failures.isEmpty ? nil : failures.joined(separator: "\n")
    }
}

extension AppModel {
    /// What Argo CD data belongs to: the context and the screenshot mode generation.
    var argoKey: String { "\(activeContext)#\(dataGeneration)" }
}
