import Foundation
import IchorCore

/// What kube-green has done to the namespaces whose workload or CronJob rows were opened, kept for
/// a minute per namespace so opening several sheets on the same namespace costs one read. Read
/// only, and a refusal or a cluster without kube-green is swallowed: the label just stays hidden.
/// Keyed on the namespace plus the cluster key (context and screenshot mode generation), so
/// another cluster's state never shows.
@Observable
@MainActor
final class KubeGreenStore {
    static let shared = KubeGreenStore()

    private struct Entry {
        let value: KubeGreenNamespace
        let at: Date
    }

    private var entries: [String: Entry] = [:]
    /// Keys being read right now: a second sheet opening mid-read waits rather than firing again.
    private var loading: Set<String> = []

    /// How long a read stays good: the same namespace's sheets share one call.
    private static let fresh: TimeInterval = 60

    /// The namespace's state if it was read for key within the last minute, else nil.
    func status(namespace: String, key: String) -> KubeGreenNamespace? {
        guard let entry = entries[Self.id(namespace, key)], Date().timeIntervalSince(entry.at) < Self.fresh else { return nil }
        return entry.value
    }

    /// Reads the namespace's state, unless it was read for key a minute ago already. A failure
    /// (a role that may not list sleepinfos, a cluster without kube-green) leaves nil.
    @discardableResult
    func load(namespace: String, key: String, with client: TalosClient) async -> KubeGreenNamespace? {
        if let fresh = status(namespace: namespace, key: key) { return fresh }
        let id = Self.id(namespace, key)
        if loading.contains(id) { return nil }
        loading.insert(id)
        defer { loading.remove(id) }
        guard let value = try? await client.kubeGreenNamespace(namespace) else { return nil }
        entries[id] = Entry(value: value, at: Date())
        return value
    }

    /// Drops what was read for the namespace: after a schedule saved, the next read is the new one.
    func forget(namespace: String, key: String) {
        entries[Self.id(namespace, key)] = nil
    }

    private static func id(_ namespace: String, _ key: String) -> String { "\(namespace)#\(key)" }
}

extension AppModel {
    /// What kube-green data belongs to: the context and the screenshot mode generation (see argoKey).
    var kubeGreenKey: String { "\(activeContext)#\(dataGeneration)" }
}
