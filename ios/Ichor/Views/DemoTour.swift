import SwiftUI
import TipKit
import IchorCore

/// The demo tour (DemoTourSteps): five TipKit coach marks on the Talos demo's overview, shown
/// once after the demo is first added, one at a time, skippable at any step; Settings → About
/// replays it. Sequenced with a step parameter rather than TipGroup (iOS 18), so it works on 17.
enum DemoTour {
    /// The tour is under way: set when the Talos demo is first added and on a replay, cleared
    /// by Skip and after the last step.
    @Parameter static var pending: Bool = false
    /// The position in DemoTourSteps.order of the coach mark showing now.
    @Parameter static var step: Int = 0

    private static let offeredKey = "demoTour.offered"
    private static let generationKey = "demoTour.generation"
    static let skipActionID = "skip"

    /// The tips' identity: a replay starts new ones, since TipKit never shows an invalidated
    /// tip again (and its datastore can only be reset before Tips.configure).
    static var generation: Int { UserDefaults.standard.integer(forKey: generationKey) }

    /// At launch, before any tip is shown.
    static func configure() {
        do {
            try Tips.configure([.displayFrequency(.immediate)])
        } catch {
            // Already configured, or no datastore: the tour just does not show.
        }
    }

    /// The Talos demo was added: the tour, the first time only.
    static func offerOnce() {
        let defaults = UserDefaults.standard
        guard !defaults.bool(forKey: offeredKey) else { return }
        defaults.set(true, forKey: offeredKey)
        start()
    }

    /// Settings → "Replay the demo tour": every coach mark again, from the first.
    static func replay() {
        UserDefaults.standard.set(generation + 1, forKey: generationKey)
        UserDefaults.standard.set(true, forKey: offeredKey)
        start()
    }

    private static func start() {
        step = 0
        pending = true
    }

    /// "Skip" on any coach mark: none of the five shows again (until a replay).
    static func skip() {
        pending = false
        for item in DemoTourSteps.order { tip(item).invalidate(reason: .tipClosed) }
    }

    /// A coach mark's button.
    static func perform(_ action: Tips.Action) {
        if action.id == skipActionID { skip() }
    }

    static func tip(_ item: DemoTourStep) -> any Tip {
        let generation = generation
        switch item {
        case .switcher: return SwitcherTip(generation: generation)
        case .nodes: return NodesTip(generation: generation)
        case .degradedNode: return DegradedNodeTip(generation: generation)
        case .actions: return ActionsTip(generation: generation)
        case .settings: return SettingsTip(generation: generation)
        }
    }

    /// Moves the tour on as each coach mark is closed, skipping those whose anchor is not on
    /// screen (`available`), until the last one; returns when the screen goes (cancelled).
    @MainActor
    static func follow(available: Set<DemoTourStep>) async {
        while pending, !Task.isCancelled {
            guard let index = DemoTourSteps.shown(from: step, available: available) else {
                pending = false
                return
            }
            if index != step { step = index }
            var closed = false
            for await status in tip(DemoTourSteps.order[index]).statusUpdates {
                if case .invalidated = status {
                    closed = true
                    break
                }
            }
            guard closed, !Task.isCancelled, pending else { return }
            if let next = DemoTourSteps.next(after: index, available: available) {
                step = next
            } else {
                pending = false
            }
        }
    }
}

/// What every coach mark shares: its text from DemoTourSteps, its identity, "Skip".
private protocol DemoTourTip: Tip {
    static var item: DemoTourStep { get }
    var generation: Int { get }
}

extension DemoTourTip {
    var id: String { "demoTour.\(Self.item.rawValue).\(generation)" }
    var title: Text { Text(LocalizedStringKey(Self.item.titleKey)) }
    var message: Text? { Text(LocalizedStringKey(Self.item.textKey)) }
    var actions: [Tips.Action] { [Tips.Action(id: DemoTour.skipActionID, title: String(localized: "Skip"))] }
}

// One type per step: a #Rule closure compares with a literal, not with a stored property.

private struct SwitcherTip: DemoTourTip {
    static let item = DemoTourStep.switcher
    let generation: Int
    var image: Image? { Image(systemName: "arrow.left.arrow.right") }
    var rules: [Rule] { [#Rule(DemoTour.$pending) { $0 == true }, #Rule(DemoTour.$step) { $0 == 0 }] }
}

private struct NodesTip: DemoTourTip {
    static let item = DemoTourStep.nodes
    let generation: Int
    var image: Image? { Image(systemName: "server.rack") }
    var rules: [Rule] { [#Rule(DemoTour.$pending) { $0 == true }, #Rule(DemoTour.$step) { $0 == 1 }] }
}

private struct DegradedNodeTip: DemoTourTip {
    static let item = DemoTourStep.degradedNode
    let generation: Int
    var image: Image? { Image(systemName: "exclamationmark.triangle") }
    var rules: [Rule] { [#Rule(DemoTour.$pending) { $0 == true }, #Rule(DemoTour.$step) { $0 == 2 }] }
}

private struct ActionsTip: DemoTourTip {
    static let item = DemoTourStep.actions
    let generation: Int
    var image: Image? { Image(systemName: "square.grid.2x2") }
    var rules: [Rule] { [#Rule(DemoTour.$pending) { $0 == true }, #Rule(DemoTour.$step) { $0 == 3 }] }
}

private struct SettingsTip: DemoTourTip {
    static let item = DemoTourStep.settings
    let generation: Int
    var image: Image? { Image(systemName: "gearshape") }
    var rules: [Rule] { [#Rule(DemoTour.$pending) { $0 == true }, #Rule(DemoTour.$step) { $0 == 4 }] }
}

extension View {
    /// The demo tour's coach mark for `item` on this view, while `active` (the Talos demo's
    /// overview on screen, no cover over it).
    @ViewBuilder
    func demoTourTip(_ item: DemoTourStep, active: Bool) -> some View {
        if active {
            let generation = DemoTour.generation
            switch item {
            case .switcher: popoverTip(SwitcherTip(generation: generation), action: DemoTour.perform)
            case .nodes: popoverTip(NodesTip(generation: generation), action: DemoTour.perform)
            case .degradedNode: popoverTip(DegradedNodeTip(generation: generation), action: DemoTour.perform)
            case .actions: popoverTip(ActionsTip(generation: generation), action: DemoTour.perform)
            case .settings: popoverTip(SettingsTip(generation: generation), action: DemoTour.perform)
            }
        } else {
            self
        }
    }
}

extension DemoTour {
    /// Whether the first coach mark shows now, as it changes: its row is in the list only then.
    @MainActor
    static func watchSwitcher(_ update: @MainActor (Bool) -> Void) async {
        for await shown in SwitcherTip(generation: generation).shouldDisplayUpdates {
            update(shown)
        }
    }
}

/// The first coach mark, inline at the top of the overview: the title it points at (with its
/// switch menu) belongs to the navigation bar, where no popover can anchor.
struct DemoTourSwitcherTip: View {
    var body: some View {
        TipView(SwitcherTip(generation: DemoTour.generation), action: DemoTour.perform)
    }
}
