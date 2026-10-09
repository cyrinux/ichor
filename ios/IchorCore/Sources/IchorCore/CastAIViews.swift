import Foundation

// Mirrors the Android model/CastAIViews.kt: how the workload list is cut and sorted.

/// How the workload list is cut: what needs a look first, only what grows, or per namespace.
public enum CastAIListMode: Hashable, Sendable, CaseIterable {
    case overview, grows, namespaces
}

/// What a section of the workload list holds; a namespace section names it in `namespace`.
public enum CastAISectionKind: Hashable, Sendable {
    case attention, grows, reductions, other, namespace
}

/// A titled run of recommendations. `hidden` are left out (the overview shows the first few that
/// grow); the deltas are the section's sums, shown for a namespace.
public struct CastAISection: Equatable, Identifiable, Sendable {
    public let kind: CastAISectionKind
    public let rows: [CastAIRecommendation]
    public let hidden: Int
    public let namespace: String
    public let cpuDeltaMilli: Int64
    public let memoryDeltaBytes: Int64

    public var id: String { "\(kind)/\(namespace)" }
    /// The rows shown plus those left out.
    public var total: Int { rows.count + hidden }

    public init(kind: CastAISectionKind, rows: [CastAIRecommendation], hidden: Int = 0, namespace: String = "",
                cpuDeltaMilli: Int64 = 0, memoryDeltaBytes: Int64 = 0) {
        self.kind = kind
        self.rows = rows
        self.hidden = hidden
        self.namespace = namespace
        self.cpuDeltaMilli = cpuDeltaMilli
        self.memoryDeltaBytes = memoryDeltaBytes
    }
}

/// How many recommendations go each way.
public struct CastAIChangeCounts: Equatable, Sendable {
    public let shrink: Int
    public let grow: Int
    public let same: Int
    public let unknown: Int

    public init(shrink: Int, grow: Int, same: Int, unknown: Int) {
        self.shrink = shrink
        self.grow = grow
        self.same = same
        self.unknown = unknown
    }
}

/// The overview shows this many workloads that grow before "show all".
public let castAIGrowsPreview = 5

public extension CastAIStatus {
    var changeCounts: CastAIChangeCounts {
        let changes = recommendations.map(\.change)
        return CastAIChangeCounts(shrink: changes.filter { $0 == .shrink }.count, grow: changes.filter { $0 == .grow }.count,
                                  same: changes.filter { $0 == .same }.count, unknown: changes.filter { $0 == .unknown }.count)
    }

    /// The apply mode most recommendations use and how many use it; nil when there are none.
    /// A tie goes to the mode seen first.
    var commonMode: (mode: CastAIMode, count: Int)? {
        var counts: [CastAIMode: Int] = [:]
        var order: [CastAIMode] = []
        for rec in recommendations {
            if counts[rec.mode] == nil { order.append(rec.mode) }
            counts[rec.mode, default: 0] += 1
        }
        var best: (mode: CastAIMode, count: Int)?
        for mode in order where counts[mode]! > (best?.count ?? 0) { best = (mode, counts[mode]!) }
        return best
    }

    /// The workload list for `mode`, narrowed by `query` (namespace or workload). Biggest changes come
    /// first: a workload that grows risks OOM kills and unschedulable pods, a shrinking one is the saving.
    func sections(_ mode: CastAIListMode, query: String = "") -> [CastAISection] {
        let q = query.trimmingCharacters(in: .whitespaces)
        let shown = q.isEmpty ? recommendations : recommendations.filter { $0.label.localizedCaseInsensitiveContains(q) }
        let biggest: (CastAIRecommendation, CastAIRecommendation) -> Bool = { a, b in
            a.weight != b.weight ? a.weight > b.weight : a.label < b.label
        }
        let byLabel: (CastAIRecommendation, CastAIRecommendation) -> Bool = { $0.label < $1.label }
        let grows = shown.filter { $0.change == .grow }.sorted { a, b in
            a.nearMemoryLimit != b.nearMemoryLimit ? a.nearMemoryLimit : biggest(a, b)
        }

        let sections: [CastAISection]
        switch mode {
        case .grows:
            sections = [CastAISection(kind: .grows, rows: grows)]
        case .namespaces:
            let byNamespace = Dictionary(grouping: shown, by: \.namespace)
            sections = byNamespace.keys.sorted().map { ns in
                let rows = byNamespace[ns] ?? []
                return CastAISection(kind: .namespace, rows: rows.sorted(by: biggest), namespace: ns,
                                     cpuDeltaMilli: rows.reduce(0) { $0 + $1.cpuDeltaMilli },
                                     memoryDeltaBytes: rows.reduce(0) { $0 + $1.memoryDeltaBytes })
            }
        case .overview:
            sections = [
                CastAISection(kind: .attention, rows: shown.filter(\.health.needsAttention).sorted(by: byLabel)),
                CastAISection(kind: .grows, rows: Array(grows.prefix(castAIGrowsPreview)), hidden: max(grows.count - castAIGrowsPreview, 0)),
                CastAISection(kind: .reductions, rows: shown.filter { $0.change == .shrink }.sorted(by: biggest)),
                CastAISection(kind: .other, rows: shown.filter { $0.change == .same || $0.change == .unknown }.sorted(by: byLabel)),
            ]
        }
        return sections.filter { !$0.rows.isEmpty }
    }
}
