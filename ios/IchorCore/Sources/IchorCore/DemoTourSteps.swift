import Foundation

/// One coach mark of the demo tour: what it points at on the Talos demo's overview. Raw values
/// are Android's `TourAnchor` names.
public enum DemoTourStep: String, CaseIterable, Sendable, Hashable {
    /// The title: tap it to switch clusters or add one.
    case switcher = "SWITCHER"
    /// The nodes section's header.
    case nodes = "NODES"
    /// The first node that is not ready: long-press for its actions.
    case degradedNode = "DEGRADED_NODE"
    /// The toolbar's actions.
    case actions = "ACTIONS"
    /// The way to Settings (its toolbar icon, else the ⋯ menu).
    case settings = "SETTINGS"

    /// The localization key (the English text) of its title.
    public var titleKey: String {
        switch self {
        case .switcher: "Switch clusters"
        case .nodes: "Your nodes"
        case .degradedNode: "A node needs a look"
        case .actions: "Quick actions"
        case .settings: "Settings"
        }
    }

    /// The localization key (the English text) of its message.
    public var textKey: String {
        switch self {
        case .switcher: "Tap the cluster name to switch clusters or add one."
        case .nodes: "Every node with its state. Tap one to open it."
        case .degradedNode: "Long-press a node for its actions: reboot, shut down, logs."
        case .actions: "The bar at the top holds the actions you use most. Choose them in Settings."
        case .settings: "Alerts, screenshot mode, backups and this tour live here."
        }
    }
}

/// The demo tour's order and rules, the same as Android's: five coach marks on the Talos demo's
/// overview, shown once, skippable at any step; one whose anchor is not on screen is skipped.
public enum DemoTourSteps {
    /// The coach marks in the order they show.
    public static let order: [DemoTourStep] = [.switcher, .nodes, .degradedNode, .actions, .settings]

    /// Only the Talos demo has the tour: the Kubernetes demo's home has other cards.
    public static func runs(demo: Bool, kube: Bool) -> Bool {
        demo && !kube
    }

    /// The position in `order` of the step to show from `index` on (itself when its anchor is
    /// available), skipping those whose anchor is not; nil once the tour is over.
    public static func shown(from index: Int, available: Set<DemoTourStep>) -> Int? {
        guard index >= 0 else { return shown(from: 0, available: available) }
        return order.indices.first { $0 >= index && available.contains(order[$0]) }
    }

    /// The position of the step after the one at `index`, skipping missing anchors; nil once
    /// the tour is over.
    public static func next(after index: Int, available: Set<DemoTourStep>) -> Int? {
        shown(from: index + 1, available: available)
    }
}
