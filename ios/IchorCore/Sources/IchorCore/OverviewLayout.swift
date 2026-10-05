import Foundation

/// The overview's sections that can be moved and hidden; banners and notices stay on top.
/// Raw values are Android's `OverviewCard` names, so the saved form reads the same on both.
public enum OverviewCard: String, CaseIterable, Sendable, Hashable, Identifiable {
    case summary = "SUMMARY"
    case apps = "APPS"
    case dataServices = "DATA_SERVICES"
    case argoCD = "ARGO_CD"
    case flux = "FLUX"
    case nodes = "NODES"
    case timeDrift = "TIME_DRIFT"

    public var id: String { rawValue }

    /// Only shown when the cluster has what they report on.
    public var whenDetected: Bool { self == .dataServices || self == .argoCD || self == .flux }
}

/// The overview's sections in the order chosen, and those hidden. Every section is in `order`
/// exactly once, so one added by a later release shows up (last) without touching the saved
/// layout. Same rules and saved form as Android's `OverviewLayout`; one layout for every cluster.
public struct OverviewLayout: Equatable, Sendable {
    /// The `@AppStorage` key the encoded layout is kept under.
    public static let storageKey = "overview.layout"

    public let order: [OverviewCard]
    public let hidden: Set<OverviewCard>

    public init(order: [OverviewCard] = OverviewCard.allCases, hidden: Set<OverviewCard> = []) {
        self.order = order
        self.hidden = hidden
    }

    public var visible: [OverviewCard] { order.filter { !hidden.contains($0) } }
    public var hiddenCards: [OverviewCard] { order.filter { hidden.contains($0) } }
    public var isDefault: Bool { self == OverviewLayout() }

    /// Moves the shown section at `from` to `to`, both indices in `visible`. Out of range: unchanged.
    public func move(from: Int, to: Int) -> OverviewLayout {
        let shown = visible
        guard shown.indices.contains(from), shown.indices.contains(to), from != to else { return self }
        var moved = shown
        moved.insert(moved.remove(at: from), at: to)
        return OverviewLayout(order: moved + hiddenCards, hidden: hidden)
    }

    /// `visible` without the sections the cluster lacks (`absent`): what the editor offers to arrange.
    public func visible(absent: Set<OverviewCard>) -> [OverviewCard] { visible.filter { !absent.contains($0) } }

    /// `hiddenCards` without the sections the cluster lacks (`absent`).
    public func hiddenCards(absent: Set<OverviewCard>) -> [OverviewCard] { hiddenCards.filter { !absent.contains($0) } }

    /// The same as a SwiftUI `onMove` over `visible(absent:)`; absent sections keep their place,
    /// for clusters that have them. No actual move: unchanged.
    public func moving(fromOffsets source: IndexSet, toOffset destination: Int, absent: Set<OverviewCard> = []) -> OverviewLayout {
        let shown = visible(absent: absent)
        let moved = movedElements(shown, fromOffsets: source, toOffset: destination)
        guard moved != shown else { return self }
        var next = moved.makeIterator()
        let order = visible.map { absent.contains($0) ? $0 : next.next()! }
        return OverviewLayout(order: order + hiddenCards, hidden: hidden)
    }

    public func hiding(_ card: OverviewCard) -> OverviewLayout {
        OverviewLayout(order: order, hidden: hidden.union([card]))
    }

    /// Shows `card` again, after the sections already shown.
    public func showing(_ card: OverviewCard) -> OverviewLayout {
        guard hidden.contains(card) else { return self }
        let rest = hidden.subtracting([card])
        return OverviewLayout(order: visible + [card] + order.filter { rest.contains($0) }, hidden: rest)
    }

    /// "SUMMARY,-APPS,NODES": the order, a dash before hidden sections.
    public var encoded: String {
        order.map { hidden.contains($0) ? "-\($0.rawValue)" : $0.rawValue }.joined(separator: ",")
    }

    /// Reads `encoded`'s form; unknown or repeated names are skipped, missing sections appended.
    public static func parse(_ text: String?) -> OverviewLayout {
        guard let text, !text.trimmingCharacters(in: .whitespaces).isEmpty else { return OverviewLayout() }
        var known: [OverviewCard] = []
        var hidden: Set<OverviewCard> = []
        for raw in text.split(separator: ",", omittingEmptySubsequences: false) {
            let name = raw.trimmingCharacters(in: .whitespaces)
            let isHidden = name.hasPrefix("-")
            guard let card = OverviewCard(rawValue: isHidden ? String(name.dropFirst()) : name),
                  !known.contains(card) else { continue }
            known.append(card)
            if isHidden { hidden.insert(card) }
        }
        return OverviewLayout(order: known + OverviewCard.allCases.filter { !known.contains($0) }, hidden: hidden)
    }
}

/// `items` after moving those at `source` before the element at `destination` (SwiftUI's
/// `move(fromOffsets:toOffset:)`, which is not available outside SwiftUI). Offsets out of range are ignored.
func movedElements<T>(_ items: [T], fromOffsets source: IndexSet, toOffset destination: Int) -> [T] {
    let picked = source.filter { items.indices.contains($0) }
    let moving = picked.map { items[$0] }
    let rest = items.enumerated().filter { !picked.contains($0.offset) }.map(\.element)
    let at = min(max(destination - picked.filter { $0 < destination }.count, 0), rest.count)
    return Array(rest[..<at]) + moving + Array(rest[at...])
}
