import Foundation

/// State of one phase of a followed run (an upgrade, a node maintenance).
public enum UpgradeStepState: Equatable, Sendable {
    case pending, current, done, failed
}

/// A row of a run's progress timeline.
public struct TimelineStep<Phase: Hashable & Sendable>: Equatable, Identifiable, Sendable {
    public let phase: Phase
    public let state: UpgradeStepState
    /// Unix ms the phase was first reported, 0 when not reached.
    public let at: Int64
    /// Latest message of the phase.
    public let message: String

    public var id: Phase { phase }

    public init(phase: Phase, state: UpgradeStepState, at: Int64 = 0, message: String = "") {
        self.phase = phase
        self.state = state
        self.at = at
        self.message = message
    }
}

/// Folds the events received so far into one step per phase of `phases`, in that order. Phases
/// only move forward: a late event of an earlier phase only updates its message; with
/// `strayMessagesToCurrent`, an event of no known phase lends its message to the furthest step.
/// `finished`, or reaching `terminal`, marks every step done. `failure` marks the furthest
/// phase (the first when nothing was reported) failed, with the failure as its message.
public func foldTimeline<Phase: Comparable & Hashable & Sendable, Event>(
    _ events: [Event], phases: [Phase], finished: Bool = false, failure: String? = nil, terminal: Phase? = nil,
    strayMessagesToCurrent: Bool = false,
    phase phaseOf: (Event) -> Phase?, at atOf: (Event) -> Int64, message messageOf: (Event) -> String
) -> [TimelineStep<Phase>] {
    var first: [Phase: Int64] = [:]
    var messages: [Phase: String] = [:]
    var furthest: Phase?
    for event in events {
        let text = messageOf(event)
        guard let phase = phaseOf(event) else {
            if strayMessagesToCurrent, let furthest, !text.isEmpty { messages[furthest] = text }
            continue
        }
        if first[phase] == nil { first[phase] = atOf(event) }
        if !text.isEmpty { messages[phase] = text }
        if furthest.map({ phase > $0 }) ?? true { furthest = phase }
    }
    return phases.map { phase in
        let at = first[phase] ?? 0
        let message = messages[phase] ?? ""
        if let failure {
            let failed = furthest ?? phases[0]
            if phase < failed { return TimelineStep(phase: phase, state: .done, at: at, message: message) }
            if phase == failed { return TimelineStep(phase: phase, state: .failed, at: at, message: failure) }
            return TimelineStep(phase: phase, state: .pending)
        }
        if finished || (terminal != nil && furthest == terminal) {
            return TimelineStep(phase: phase, state: .done, at: at, message: message)
        }
        guard let furthest else { return TimelineStep(phase: phase, state: .pending) }
        let state: UpgradeStepState = phase < furthest ? .done : phase == furthest ? .current : .pending
        return TimelineStep(phase: phase, state: state, at: at, message: message)
    }
}
