import Foundation

/// The iPad overview in two columns: when, and which card goes in which column.
public enum OverviewColumns {
    /// The narrowest overview shown in two columns (a full-width or wide Split View iPad); a
    /// half-screen Split View or Stage Manager window keeps one.
    public static let twoColumnMinWidth: Double = 900

    /// 2 on an iPad's regular width at least `twoColumnMinWidth` wide, else 1.
    public static func count(width: Double, regularPad: Bool) -> Int {
        regularPad && width >= twoColumnMinWidth ? 2 : 1
    }

    /// The visible cards in two columns, in the layout editor's order read row by row: the
    /// cluster summary heads the leading column, then the others alternate, trailing first so
    /// the first one sits beside the summary.
    public static func split(_ cards: [OverviewCard]) -> (leading: [OverviewCard], trailing: [OverviewCard]) {
        let others = cards.filter { $0 != .summary }
        var leading = cards.contains(.summary) ? [OverviewCard.summary] : []
        var trailing: [OverviewCard] = []
        for (index, card) in others.enumerated() {
            if index.isMultiple(of: 2) == cards.contains(.summary) {
                trailing.append(card)
            } else {
                leading.append(card)
            }
        }
        return (leading, trailing)
    }
}
