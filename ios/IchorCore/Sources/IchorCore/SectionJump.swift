import Foundation

/// One section of a long list that the section rail can jump to: `id` is the scroll id of its
/// header view, `label` the short name shown while scrubbing and read by VoiceOver.
public struct JumpSection: Hashable, Identifiable, Sendable {
    public let id: String
    public let label: String

    public init(id: String, label: String) {
        self.id = id
        self.label = label
    }
}

/// The section rail's rules, kept out of the views so they are tested on Linux.
public enum SectionJump {
    /// How far below the list's top edge a header may sit and still count as reached: a header
    /// scrolled to with `anchor: .top` lands a few points under it.
    public static let reachedSlack = 24.0

    /// The section the list is showing, from the on-screen tops of the headers in view
    /// (`visibleTops`, by section id) and the top edge of the list's content (`top`): the last
    /// header at or above that edge; when every header in view is below it, the section before
    /// the first of them; with no header in view, `previous`.
    public static func current(sections: [JumpSection], visibleTops: [String: Double], top: Double, previous: String?) -> String? {
        guard !sections.isEmpty else { return nil }
        let inView = sections.enumerated().filter { visibleTops[$0.element.id] != nil }
        guard let first = inView.first else { return previous }
        let reached = inView.last { (visibleTops[$0.element.id] ?? .infinity) <= top + reachedSlack }
        if let reached { return reached.element.id }
        return sections[max(first.offset - 1, 0)].id
    }

    /// The section under a finger at `fraction` (0 top, 1 bottom) of the rail, each section an
    /// equal slice; out-of-range fractions are clamped.
    public static func section(at fraction: Double, in sections: [JumpSection]) -> JumpSection? {
        guard !sections.isEmpty else { return nil }
        let clamped = fraction.isNaN ? 0 : min(max(fraction, 0), 1)
        let slot = Int(clamped * Double(sections.count))
        return sections[min(slot, sections.count - 1)]
    }
}
