import Foundation

/// A home screen's section that can be moved and hidden; banners and notices stay on top. Raw
/// values are Android's enum names, so the saved form reads the same on both.
public protocol HomeCard: RawRepresentable, CaseIterable, Sendable, Hashable, Identifiable where RawValue == String, AllCases == [Self] {
    /// The `@AppStorage` key the home's encoded layout is kept under.
    static var layoutStorageKey: String { get }
    /// Only shown when the cluster has what it reports on.
    var whenDetected: Bool { get }
    /// Was pinned above the sections before it could be arranged: a layout saved then keeps it first.
    var leadsWhenNew: Bool { get }
}

/// The Talos overview's sections. Raw values are Android's `OverviewCard` names.
public enum OverviewCard: String, HomeCard {
    case talosUpdate = "TALOS_UPDATE"
    case summary = "SUMMARY"
    case apps = "APPS"
    case dataServices = "DATA_SERVICES"
    case argoCD = "ARGO_CD"
    case flux = "FLUX"
    case nodes = "NODES"
    case timeDrift = "TIME_DRIFT"
    /// The Alertmanager's alerts by severity: last, so saved layouts keep their order.
    case alerts = "ALERTS"

    public var id: String { rawValue }
    public static let layoutStorageKey = "overview.layout"

    public var whenDetected: Bool { self == .dataServices || self == .argoCD || self == .flux || self == .alerts }

    public var leadsWhenNew: Bool { self == .talosUpdate }
}

/// The sections of the Kubernetes home (a cluster added from a kubeconfig): the API server and
/// the credentials, the nodes as Kubernetes lists them, the Kubernetes screens, and the operators
/// found on the cluster. Raw values are Android's `KubeHomeCard` names.
public enum KubeHomeCard: String, HomeCard {
    case summary = "SUMMARY"
    case nodes = "NODES"
    case tools = "TOOLS"
    case dataServices = "DATA_SERVICES"
    case argoCD = "ARGO_CD"
    case flux = "FLUX"
    /// The Alertmanager's alerts by severity.
    case alerts = "ALERTS"

    public var id: String { rawValue }
    public static let layoutStorageKey = "kubeHome.layout"

    public var whenDetected: Bool { self == .dataServices || self == .argoCD || self == .flux || self == .alerts }

    public var leadsWhenNew: Bool { false }
}

/// A home's sections in the order chosen, and those hidden. Every section is in `order`
/// exactly once, so one added by a later release shows up (last, or first when it `leadsWhenNew`)
/// without touching the saved layout. Same rules and saved form as Android's `CardLayout`; one layout for every cluster.
public struct CardLayout<Card: HomeCard>: Equatable, Sendable {
    /// The `@AppStorage` key the encoded layout is kept under.
    public static var storageKey: String { Card.layoutStorageKey }

    public let order: [Card]
    public let hidden: Set<Card>

    public init(order: [Card] = Card.allCases, hidden: Set<Card> = []) {
        self.order = order
        self.hidden = hidden
    }

    public var visible: [Card] { order.filter { !hidden.contains($0) } }
    public var hiddenCards: [Card] { order.filter { hidden.contains($0) } }
    public var isDefault: Bool { self == CardLayout() }

    /// Moves the shown section at `from` to `to`, both indices in `visible`. Out of range: unchanged.
    public func move(from: Int, to: Int) -> CardLayout {
        let shown = visible
        guard shown.indices.contains(from), shown.indices.contains(to), from != to else { return self }
        var moved = shown
        moved.insert(moved.remove(at: from), at: to)
        return CardLayout(order: moved + hiddenCards, hidden: hidden)
    }

    /// `visible` without the sections the cluster lacks (`absent`): what the editor offers to arrange.
    public func visible(absent: Set<Card>) -> [Card] { visible.filter { !absent.contains($0) } }

    /// `hiddenCards` without the sections the cluster lacks (`absent`).
    public func hiddenCards(absent: Set<Card>) -> [Card] { hiddenCards.filter { !absent.contains($0) } }

    /// The same as a SwiftUI `onMove` over `visible(absent:)`; absent sections keep their place,
    /// for clusters that have them. No actual move: unchanged.
    public func moving(fromOffsets source: IndexSet, toOffset destination: Int, absent: Set<Card> = []) -> CardLayout {
        let shown = visible(absent: absent)
        let moved = movedElements(shown, fromOffsets: source, toOffset: destination)
        guard moved != shown else { return self }
        var next = moved.makeIterator()
        let order = visible.map { absent.contains($0) ? $0 : next.next()! }
        return CardLayout(order: order + hiddenCards, hidden: hidden)
    }

    public func hiding(_ card: Card) -> CardLayout {
        CardLayout(order: order, hidden: hidden.union([card]))
    }

    /// Shows `card` again, after the sections already shown.
    public func showing(_ card: Card) -> CardLayout {
        guard hidden.contains(card) else { return self }
        let rest = hidden.subtracting([card])
        return CardLayout(order: visible + [card] + order.filter { rest.contains($0) }, hidden: rest)
    }

    /// "SUMMARY,-APPS,NODES": the order, a dash before hidden sections.
    public var encoded: String {
        order.map { hidden.contains($0) ? "-\($0.rawValue)" : $0.rawValue }.joined(separator: ",")
    }

    /// Reads `encoded`'s form; unknown or repeated names are skipped, missing sections added.
    public static func parse(_ text: String?) -> CardLayout {
        guard let text, !text.trimmingCharacters(in: .whitespaces).isEmpty else { return CardLayout() }
        var known: [Card] = []
        var hidden: Set<Card> = []
        for raw in text.split(separator: ",", omittingEmptySubsequences: false) {
            let name = raw.trimmingCharacters(in: .whitespaces)
            let isHidden = name.hasPrefix("-")
            guard let card = Card(rawValue: isHidden ? String(name.dropFirst()) : name),
                  !known.contains(card) else { continue }
            known.append(card)
            if isHidden { hidden.insert(card) }
        }
        let missing = Card.allCases.filter { !known.contains($0) }
        return CardLayout(order: missing.filter(\.leadsWhenNew) + known + missing.filter { !$0.leadsWhenNew }, hidden: hidden)
    }
}

/// The Talos overview's sections as arranged.
public typealias OverviewLayout = CardLayout<OverviewCard>

/// The Kubernetes home's sections as arranged.
public typealias KubeHomeLayout = CardLayout<KubeHomeCard>

/// `items` after moving those at `source` before the element at `destination` (SwiftUI's
/// `move(fromOffsets:toOffset:)`, which is not available outside SwiftUI). Offsets out of range are ignored.
func movedElements<T>(_ items: [T], fromOffsets source: IndexSet, toOffset destination: Int) -> [T] {
    let picked = source.filter { items.indices.contains($0) }
    let moving = picked.map { items[$0] }
    let rest = items.enumerated().filter { !picked.contains($0.offset) }.map(\.element)
    let at = min(max(destination - picked.filter { $0 < destination }.count, 0), rest.count)
    return Array(rest[..<at]) + moving + Array(rest[at...])
}
