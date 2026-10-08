import Foundation

/// An action a screen's toolbar offers. Raw values are the Android enum's names, the saved form.
public protocol BarAction: RawRepresentable, CaseIterable, Sendable, Hashable, Identifiable where RawValue == String, AllCases == [Self] {
    /// The `@AppStorage` key the screen's encoded bar is kept under.
    static var barStorageKey: String { get }
    /// How many of `allCases` (in that order) are icons by default; the rest are in the menu.
    static var defaultIcons: Int { get }
}

/// The screens the overview's toolbar leads to. Raw values are Android's `OverviewAction` names.
public enum OverviewAction: String, BarAction {
    case health = "HEALTH"
    case events = "EVENTS"
    case workloads = "WORKLOADS"
    case metrics = "METRICS"
    case kubespan = "KUBESPAN"
    case etcd = "ETCD"
    case settings = "SETTINGS"

    public var id: String { rawValue }
    public static let barStorageKey = "overview.bar"
    public static let defaultIcons = 3
}

/// What the Kubernetes screen's toolbar offers: the screens it leads to, its share link and the
/// API address. Raw values are Android's `KubernetesAction` names.
public enum KubernetesAction: String, BarAction {
    case checkup = "CHECKUP"
    case networkPolicies = "NETWORK_POLICIES"
    case share = "SHARE"
    case apiHealth = "API_HEALTH"
    case flows = "FLOWS"
    case resources = "RESOURCES"
    case helm = "HELM"
    case apiAddress = "API_ADDRESS"

    public var id: String { rawValue }
    public static let barStorageKey = "kubernetes.bar"
    public static let defaultIcons = 3
}

/// The screens the Kubernetes home's toolbar leads to (a cluster added from a kubeconfig): the
/// Kubernetes screens that work with its credentials alone, no Talos one. Raw values are
/// Android's `KubeHomeAction` names.
public enum KubeHomeAction: String, BarAction {
    case workloads = "WORKLOADS"
    case resources = "RESOURCES"
    case metrics = "METRICS"
    case helm = "HELM"
    case dataServices = "DATA_SERVICES"
    case checkup = "CHECKUP"
    case apiHealth = "API_HEALTH"
    case networkPolicies = "NETWORK_POLICIES"
    /// The cluster's Kubernetes events (the Talos home's Events are the machines').
    case events = "EVENTS"
    case settings = "SETTINGS"

    public var id: String { rawValue }
    public static let barStorageKey = "kubeHome.bar"
    public static let defaultIcons = 3
}

/// The overview's toolbar; one bar for every cluster.
public typealias OverviewBar = ActionBar<OverviewAction>
/// The Kubernetes home's toolbar; one bar for every cluster added from a kubeconfig.
public typealias KubeHomeBar = ActionBar<KubeHomeAction>
public typealias OverviewBarSlot = ActionBarSlot<OverviewAction>
public typealias KubernetesBar = ActionBar<KubernetesAction>

/// A row of the toolbar editor: an action, or the line between the icons and the menu.
public enum ActionBarSlot<Action: BarAction>: Hashable, Sendable, Identifiable {
    case action(Action)
    case menuLine

    public var id: String {
        switch self {
        case .action(let action): action.rawValue
        case .menuLine: "|"
        }
    }
}

/// A screen's toolbar actions in the order chosen: the first `iconCount` shown as icons, the
/// rest in its menu, so the title keeps room on a phone. Every action is in `order` exactly
/// once, so one added by a later release shows up (last, in the menu) without touching the
/// saved bar. Same rules and saved form as Android's `ActionBar`.
public struct ActionBar<Action: BarAction>: Equatable, Sendable {
    /// The `@AppStorage` key the encoded bar is kept under.
    public static var storageKey: String { Action.barStorageKey }
    public static var defaultIcons: Int { Action.defaultIcons }
    private static var separator: Character { "|" }

    public let order: [Action]
    public let iconCount: Int

    public init(order: [Action] = Action.allCases, iconCount: Int = Action.defaultIcons) {
        self.order = order
        self.iconCount = min(max(iconCount, 0), order.count)
    }

    public var icons: [Action] { Array(order.prefix(iconCount)) }
    public var menu: [Action] { Array(order.dropFirst(iconCount)) }
    public var isDefault: Bool { self == ActionBar() }

    /// One step towards the start; the menu's first action becomes the last icon.
    public func up(_ action: Action) -> ActionBar {
        guard let index = order.firstIndex(of: action) else { return self }
        if index == iconCount { return ActionBar(order: order, iconCount: iconCount + 1) }
        if index == 0 { return self }
        return ActionBar(order: swapped(index, index - 1), iconCount: iconCount)
    }

    /// One step towards the end; the last icon becomes the menu's first action.
    public func down(_ action: Action) -> ActionBar {
        guard let index = order.firstIndex(of: action) else { return self }
        if index == iconCount - 1 { return ActionBar(order: order, iconCount: iconCount - 1) }
        if index == order.count - 1 { return self }
        return ActionBar(order: swapped(index, index + 1), iconCount: iconCount)
    }

    /// Puts an icon at the top of the menu.
    public func toMenu(_ action: Action) -> ActionBar {
        guard icons.contains(action) else { return self }
        let rest = order.filter { $0 != action }
        return ActionBar(order: Array(rest.prefix(iconCount - 1)) + [action] + rest.dropFirst(iconCount - 1), iconCount: iconCount - 1)
    }

    /// Shows a menu action as the last icon.
    public func toBar(_ action: Action) -> ActionBar {
        guard menu.contains(action) else { return self }
        let rest = order.filter { $0 != action }
        return ActionBar(order: Array(rest.prefix(iconCount)) + [action] + rest.dropFirst(iconCount), iconCount: iconCount + 1)
    }

    /// The editor's rows: the icons, the menu line, then the menu.
    public var slots: [ActionBarSlot<Action>] { icons.map(ActionBarSlot<Action>.action) + [.menuLine] + menu.map(ActionBarSlot<Action>.action) }

    /// The same as a SwiftUI `onMove` over `slots`: an action dragged across the menu line
    /// moves between the icons and the menu. The line itself does not move.
    public func moving(fromOffsets source: IndexSet, toOffset destination: Int) -> ActionBar {
        let current = slots
        guard let line = current.firstIndex(of: .menuLine), !source.contains(line) else { return self }
        let moved = movedElements(current, fromOffsets: source, toOffset: destination)
        let actions = moved.compactMap { slot -> Action? in
            if case .action(let action) = slot { return action }
            return nil
        }
        return ActionBar(order: actions, iconCount: moved.firstIndex(of: .menuLine) ?? iconCount)
    }

    /// "HEALTH,EVENTS|METRICS,SETTINGS": the icons, then the menu after the bar.
    public var encoded: String {
        icons.map(\.rawValue).joined(separator: ",") + String(Self.separator) + menu.map(\.rawValue).joined(separator: ",")
    }

    /// Reads `encoded`'s form; unknown or repeated names are skipped, missing actions go last in the menu.
    public static func parse(_ text: String?) -> ActionBar {
        guard let text, !text.trimmingCharacters(in: .whitespaces).isEmpty,
              let cut = text.firstIndex(of: separator) else { return ActionBar() }
        let icons = unique(names(text[..<cut]))
        let menu = unique(names(text[text.index(after: cut)...]).filter { !icons.contains($0) })
        let known = icons + menu
        return ActionBar(order: known + Action.allCases.filter { !known.contains($0) }, iconCount: icons.count)
    }

    private static func names(_ part: Substring) -> [Action] {
        part.split(separator: ",").compactMap { Action(rawValue: $0.trimmingCharacters(in: .whitespaces)) }
    }

    private static func unique(_ actions: [Action]) -> [Action] {
        actions.reduce(into: []) { result, action in if !result.contains(action) { result.append(action) } }
    }

    private func swapped(_ a: Int, _ b: Int) -> [Action] {
        var copy = order
        copy.swapAt(a, b)
        return copy
    }
}
