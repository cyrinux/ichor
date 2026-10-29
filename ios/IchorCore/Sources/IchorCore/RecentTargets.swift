import Foundation

/// An object opened lately (a node, workload, pod, CronJob, Argo CD or Flux app), for the
/// search's "Recently opened": the screen it opens as a share link names it, the label and
/// detail it was shown with, and when (epoch ms). Same JSON as Android's `RecentTarget`.
public struct RecentTarget: Codable, Hashable, Sendable, Identifiable {
    public let target: ShareTarget
    public let label: String
    public let detail: String
    public let at: Int64

    public init(target: ShareTarget, label: String, detail: String = "", at: Int64 = 0) {
        self.target = target
        self.label = label
        self.detail = detail
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case target, label, detail, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        target = try c.decode(ShareTarget.self, forKey: .target)
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        at = try c.decodeIfPresent(Int64.self, forKey: .at) ?? 0
    }

    /// The object it names, whatever tab it was opened on (Android's `RecentTarget.key`).
    public var id: String { RecentTarget.key(target) }

    static func key(_ target: ShareTarget) -> String {
        [target.cluster, target.target.rawValue, target.host, target.addr, target.kind, target.namespace, target.name]
            .joined(separator: "|")
    }
}

/// The phone's recently opened objects, newest first, at most `limit`; saved as a JSON array.
public struct RecentTargetList: Equatable, Sendable {
    public static let limit = 20
    /// What is recorded: objects a link can name, as pins (not the cluster-wide screens).
    public static let recorded = Pin.pinnable

    public let targets: [RecentTarget]

    public init(_ targets: [RecentTarget] = []) {
        var seen: Set<String> = []
        self.targets = Array(
            targets.filter { RecentTargetList.recorded.contains($0.target.target) && seen.insert($0.id).inserted }
                .prefix(RecentTargetList.limit))
    }

    /// The list with `opened` first and its earlier entry dropped; unchanged for what is not
    /// recorded, names no cluster (a link could not open it again) or has no label.
    public func adding(_ opened: RecentTarget) -> RecentTargetList {
        guard RecentTargetList.recorded.contains(opened.target.target), !opened.target.cluster.isEmpty,
            !opened.label.trimmingCharacters(in: .whitespaces).isEmpty
        else { return self }
        return RecentTargetList([opened] + targets.filter { $0.id != opened.id })
    }

    public func removing(_ target: ShareTarget) -> RecentTargetList {
        let key = RecentTarget.key(target)
        return RecentTargetList(targets.filter { $0.id != key })
    }

    /// Those of a cluster still on the phone: a removed cluster's are not shown.
    public func known(clusterIDs: Set<String>) -> [RecentTarget] {
        targets.filter { clusterIDs.contains($0.target.cluster) }
    }

    public static func parse(_ text: String?) -> RecentTargetList {
        guard let text, !text.isEmpty, let decoded = try? TalosJSON.decode([RecentTarget].self, from: text) else {
            return RecentTargetList()
        }
        return RecentTargetList(decoded)
    }

    public var encoded: String { (try? TalosJSON.encode(targets)) ?? "[]" }
}

public extension KubeFocus {
    /// The share target of the object the Kubernetes screen focuses on; nil for none.
    var shareTarget: ShareTarget? {
        guard !name.isEmpty else { return nil }
        switch tab {
        case .workloads:
            let parts = id.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
            return parts.count == 3 ? .workload(kind: parts[0], namespace: parts[1], name: parts[2]) : nil
        case .pods: return .pod(namespace: namespace, name: name)
        case .cronJobs: return .cronJob(namespace: namespace, name: name)
        }
    }
}
