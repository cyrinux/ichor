import Foundation

/// The screens the overview's toolbar leads to. Raw values are Android's `OverviewAction` names.
public enum OverviewAction: String, CaseIterable, Sendable, Hashable, Identifiable {
    case health = "HEALTH"
    case events = "EVENTS"
    case workloads = "WORKLOADS"
    case metrics = "METRICS"
    case kubespan = "KUBESPAN"
    case etcd = "ETCD"
    case settings = "SETTINGS"

    public var id: String { rawValue }
}

/// A row of the toolbar editor: an action, or the line between the icons and the menu.
public enum OverviewBarSlot: Hashable, Sendable, Identifiable {
    case action(OverviewAction)
    case menuLine

    public var id: String {
        switch self {
        case .action(let action): action.rawValue
        case .menuLine: "|"
        }
    }
}

/// The overview's toolbar actions in the order chosen: the first `iconCount` shown as icons,
/// the rest in its menu. Every action is in `order` exactly once, so one added by a later
/// release shows up (last, in the menu) without touching the saved bar. Same rules and saved
/// form as Android's `OverviewBar`; one bar for every cluster.
public struct OverviewBar: Equatable, Sendable {
    /// The `@AppStorage` key the encoded bar is kept under.
    public static let storageKey = "overview.bar"
    public static let defaultIcons = 3
    private static let separator: Character = "|"

    public let order: [OverviewAction]
    public let iconCount: Int

    public init(order: [OverviewAction] = OverviewAction.allCases, iconCount: Int = OverviewBar.defaultIcons) {
        self.order = order
        self.iconCount = min(max(iconCount, 0), order.count)
    }

    public var icons: [OverviewAction] { Array(order.prefix(iconCount)) }
    public var menu: [OverviewAction] { Array(order.dropFirst(iconCount)) }
    public var isDefault: Bool { self == OverviewBar() }

    /// One step towards the start; the menu's first action becomes the last icon.
    public func up(_ action: OverviewAction) -> OverviewBar {
        guard let index = order.firstIndex(of: action) else { return self }
        if index == iconCount { return OverviewBar(order: order, iconCount: iconCount + 1) }
        if index == 0 { return self }
        return OverviewBar(order: swapped(index, index - 1), iconCount: iconCount)
    }

    /// One step towards the end; the last icon becomes the menu's first action.
    public func down(_ action: OverviewAction) -> OverviewBar {
        guard let index = order.firstIndex(of: action) else { return self }
        if index == iconCount - 1 { return OverviewBar(order: order, iconCount: iconCount - 1) }
        if index == order.count - 1 { return self }
        return OverviewBar(order: swapped(index, index + 1), iconCount: iconCount)
    }

    /// Puts an icon at the top of the menu.
    public func toMenu(_ action: OverviewAction) -> OverviewBar {
        guard icons.contains(action) else { return self }
        let rest = order.filter { $0 != action }
        return OverviewBar(order: Array(rest.prefix(iconCount - 1)) + [action] + rest.dropFirst(iconCount - 1), iconCount: iconCount - 1)
    }

    /// Shows a menu action as the last icon.
    public func toBar(_ action: OverviewAction) -> OverviewBar {
        guard menu.contains(action) else { return self }
        let rest = order.filter { $0 != action }
        return OverviewBar(order: Array(rest.prefix(iconCount)) + [action] + rest.dropFirst(iconCount), iconCount: iconCount + 1)
    }

    /// The editor's rows: the icons, the menu line, then the menu.
    public var slots: [OverviewBarSlot] { icons.map(OverviewBarSlot.action) + [.menuLine] + menu.map(OverviewBarSlot.action) }

    /// The same as a SwiftUI `onMove` over `slots`: an action dragged across the menu line
    /// moves between the icons and the menu. The line itself does not move.
    public func moving(fromOffsets source: IndexSet, toOffset destination: Int) -> OverviewBar {
        let current = slots
        guard let line = current.firstIndex(of: .menuLine), !source.contains(line) else { return self }
        let moved = movedElements(current, fromOffsets: source, toOffset: destination)
        let actions = moved.compactMap { slot -> OverviewAction? in
            if case .action(let action) = slot { return action }
            return nil
        }
        return OverviewBar(order: actions, iconCount: moved.firstIndex(of: .menuLine) ?? iconCount)
    }

    /// "HEALTH,EVENTS|METRICS,SETTINGS": the icons, then the menu after the bar.
    public var encoded: String {
        icons.map(\.rawValue).joined(separator: ",") + String(Self.separator) + menu.map(\.rawValue).joined(separator: ",")
    }

    /// Reads `encoded`'s form; unknown or repeated names are skipped, missing actions go last in the menu.
    public static func parse(_ text: String?) -> OverviewBar {
        guard let text, !text.trimmingCharacters(in: .whitespaces).isEmpty,
              let cut = text.firstIndex(of: separator) else { return OverviewBar() }
        let icons = unique(names(text[..<cut]))
        let menu = unique(names(text[text.index(after: cut)...]).filter { !icons.contains($0) })
        let known = icons + menu
        return OverviewBar(order: known + OverviewAction.allCases.filter { !known.contains($0) }, iconCount: icons.count)
    }

    private static func names(_ part: Substring) -> [OverviewAction] {
        part.split(separator: ",").compactMap { OverviewAction(rawValue: $0.trimmingCharacters(in: .whitespaces)) }
    }

    private static func unique(_ actions: [OverviewAction]) -> [OverviewAction] {
        actions.reduce(into: []) { result, action in if !result.contains(action) { result.append(action) } }
    }

    private func swapped(_ a: Int, _ b: Int) -> [OverviewAction] {
        var copy = order
        copy.swapAt(a, b)
        return copy
    }
}
