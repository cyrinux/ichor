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

    /// The ended freezes already cleared (or tried) for the key, by project and windows.
    private var cleared: Set<String> = []
    private var clearedKey = ""

    /// Loads with client and keeps the answer for key. cluster: the cluster on screen, whose
    /// freeze reminders follow the answer (nil: none, the demo).
    @discardableResult
    func load(with client: TalosClient, key: String, cluster: ContextSummary? = nil) async throws -> ArgoStatus {
        let loaded = try await client.argoCD()
        self.key = key
        status = loaded
        if let cluster, !cluster.demo { FreezeReminders.sync(loaded, cluster: cluster.fingerprint) }
        clearExpired(loaded, key: key, with: client)
        return loaded
    }

    /// Ichor's ended freezes would fire again a year later (Argo CD has no one-shot window):
    /// removed quietly, once per project; a refusal (a read-only role, the demo) is left alone.
    private func clearExpired(_ status: ArgoStatus, key: String, with client: TalosClient) {
        if key != clearedKey { cleared = []; clearedKey = key }
        for project in status.projectsToClear where !busyProjects.contains(project.id) {
            // Keyed on the ended windows: a later freeze ending in the same project is cleared too.
            let ended = project.windows.filter { $0.ichor?.expired == true }.map(\.id).sorted().joined(separator: ",")
            guard cleared.insert(project.id + "|" + ended).inserted else { continue }
            busyProjects.insert(project.id)
            Task {
                try? await client.argoFreeze(namespace: project.namespace, project: project.name, action: .clearExpired)
                busyProjects.remove(project.id)
            }
        }
    }

    /// Projects whose windows are being changed.
    private(set) var busyProjects: Set<String> = []

    /// Runs action on the sync windows of project, once per options entry; nil on success, else
    /// the message to show.
    func freeze(_ action: ArgoFreezeAction, on project: ArgoProject, options: [ArgoFreezeOptions], with client: TalosClient) async -> String? {
        guard !options.isEmpty else { return nil }
        // Another change is in flight: refusing beats reporting success for nothing done.
        guard !busyProjects.contains(project.id) else {
            return String(localized: "\(project.name): another change to its sync windows is in progress.")
        }
        busyProjects.insert(project.id)
        defer { busyProjects.remove(project.id) }
        for entry in options {
            do {
                try await client.argoFreeze(namespace: project.namespace, project: project.name, action: action, options: entry)
            } catch {
                return "\(project.name): \(error.localizedDescription)"
            }
        }
        fastUntil = Date().addingTimeInterval(10)
        return nil
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
