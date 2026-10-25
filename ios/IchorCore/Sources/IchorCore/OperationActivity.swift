import Foundation

// A run the app follows (an upgrade, a maintenance, a config try…) as a Live Activity on the
// Lock Screen and in the Dynamic Island. The app builds the texts (they are localized there);
// these are the shared types and the rules, the same in the app and the widget extension.

/// The runs the app follows app-wide, one at a time each.
public enum OperationKind: String, Codable, Hashable, Sendable, CaseIterable {
    case upgrade, maintenance, configTry, configMulti, clusterUpgrade, k8sUpgrade, imagePull, join, imageScan
    /// Kubernetes-side runs started from the phone: an Argo CD sync, a Flux reconcile, a
    /// rollout restart and a network test.
    case argoSync, fluxReconcile, rollout, netPerf
    /// A runbook's ordered steps (docs/runbooks.md).
    case runbook
    /// "Clean up dead pods": a namespace's dead pods deleted one after another.
    case podSweep
    /// A cluster's Talos API CA rotated, node by node, in five steps.
    case caRotation

    /// The in-app link that opens the run's screen (ichor://open?op=…).
    public var link: URL { URL(string: "ichor://open?op=\(rawValue)")! }

    /// The run a link opens, nil for any other link (a share link goes to ParseShareLink).
    public init?(link: URL) {
        guard link.scheme == "ichor", link.host == "open",
              let item = URLComponents(url: link, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "op" }),
              let kind = item.value.flatMap(OperationKind.init(rawValue:))
        else { return nil }
        self = kind
    }

    /// Its run shows on a screen of its own, which the activity opens; a config rollout, an
    /// image pull, an image scan and a pod sweep show in a sheet or a section instead (the app
    /// as it was).
    public var hasScreen: Bool {
        switch self {
        case .configMulti, .imagePull, .imageScan, .podSweep: false
        default: true
        }
    }

    /// Its SF Symbol, in the activity and the Dynamic Island.
    public var symbol: String {
        switch self {
        case .upgrade, .clusterUpgrade: "arrow.up.circle"
        case .k8sUpgrade: "helm"
        case .maintenance: "wrench.and.screwdriver"
        case .configTry: "timer"
        case .configMulti: "doc.text"
        case .imagePull: "square.and.arrow.down"
        case .join: "plus.circle"
        case .imageScan: "shield.lefthalf.filled"
        case .argoSync: "arrow.triangle.2.circlepath"
        case .fluxReconcile: "arrow.clockwise"
        case .rollout: "arrow.counterclockwise.circle"
        case .netPerf: "speedometer"
        case .runbook: "list.bullet.rectangle"
        case .podSweep: "sparkles"
        case .caRotation: "key.horizontal"
        }
    }
}

/// Where a run stands.
public enum OperationPhase: String, Codable, Hashable, Sendable {
    case running, paused, succeeded, failed, stopped

    public var ended: Bool { self == .succeeded || self == .failed || self == .stopped }
}

/// A button on a running operation's activity; every one asks for the device to be unlocked
/// first. With the app lock on, those that `opensAppWhenLocked` open the app on the run instead
/// (unlocked first), like the run's own screen.
public enum OperationButton: String, Codable, Hashable, Sendable, CaseIterable {
    case stop, stopFollowing, pause, resume, abort, keep, revert
    /// An Argo CD sync's Terminate.
    case terminate

    public var opensAppWhenLocked: Bool {
        switch self {
        case .stop, .abort, .keep, .terminate: true
        case .stopFollowing, .pause, .resume, .revert: false
        }
    }

    /// With the app lock on, the button opens `kind`'s screen rather than acting; a run with no
    /// screen of its own keeps the button (it would otherwise lead nowhere).
    public func opensApp(for kind: OperationKind, locked: Bool) -> Bool {
        locked && opensAppWhenLocked && kind.hasScreen
    }

    /// What each run offers while it runs: `stopping` once a stop was asked (nothing more to ask),
    /// `paused` for a roll waiting on Resume, `waiting` for a try holding the change.
    public static func offered(for kind: OperationKind, stopping: Bool = false, paused: Bool = false, waiting: Bool = false) -> [OperationButton] {
        if stopping { return [] }
        switch kind {
        case .upgrade, .join, .fluxReconcile, .rollout: return [.stopFollowing]
        case .argoSync: return [.terminate]
        case .maintenance, .configMulti, .k8sUpgrade, .imagePull, .imageScan, .netPerf, .runbook, .podSweep, .caRotation: return [.stop]
        case .clusterUpgrade: return paused ? [.resume, .abort] : [.pause, .abort]
        case .configTry: return waiting ? [.keep, .revert] : []
        }
    }
}

/// What the activity shows now: the run's title and latest step (as the app words them), how
/// far it is (`done` of `total`, when the core counts), a countdown to `deadline` (a config
/// try's revert), where it stands, its buttons, and when this was true (`updated`: the activity
/// is updated only while the app runs).
public struct OperationContent: Codable, Hashable, Sendable {
    public var title: String
    public var step: String
    public var done: Int?
    public var total: Int?
    public var deadline: Date?
    public var phase: OperationPhase
    public var buttons: [OperationButton]
    /// With the app lock on: the title is generic and the step left out (the Lock Screen shows it).
    public var locked: Bool
    public var updated: Date

    public init(title: String, step: String, done: Int? = nil, total: Int? = nil, deadline: Date? = nil,
                phase: OperationPhase = .running, buttons: [OperationButton] = [], locked: Bool = false, updated: Date = Date()) {
        self.title = title
        self.step = step
        self.done = done
        self.total = total
        self.deadline = deadline
        self.phase = phase
        self.buttons = phase.ended ? [] : buttons
        self.locked = locked
        self.updated = updated
    }

    /// A bar, rather than a spinner: the core counts nodes or images.
    public var isDeterminate: Bool { (total ?? 0) > 0 && done != nil }

    /// How far along the bar is, 0…1.
    public var fraction: Double {
        guard let total, total > 0, let done else { return 0 }
        return Double(min(max(done, 0), total)) / Double(total)
    }

    /// "2/5" for the Dynamic Island, nil without a count.
    public var count: String? {
        guard isDeterminate, let done, let total else { return nil }
        return "\(min(max(done, 0), total))/\(total)"
    }

    /// The same content for the Lock Screen with the app lock on: no node, version or cluster,
    /// only `title` (the kind's generic name) and the count.
    public func masked(title: String) -> OperationContent {
        var shown = self
        shown.title = title
        shown.step = ""
        shown.locked = true
        return shown
    }
}

/// The bar of a run on step `index` (from 0) of `total`: (done, total), nil without a count.
public func operationCount(index: Int, total: Int) -> (done: Int, total: Int)? {
    total > 0 ? (min(max(index, 0), total), total) : nil
}

/// What a button on an activity does, set by the app at launch: the intent runs in the app's
/// process, where the runs are.
@MainActor
public enum OperationButtonHandler {
    public static var perform: ((OperationKind, OperationButton) -> Void)?
}

// iOS only: ActivityKit imports on macOS (the package's tests run there too) but is unavailable.
#if canImport(ActivityKit) && os(iOS)
import ActivityKit

/// The Live Activity of a run: one per kind at a time. Defined once here, so the app and the
/// widget extension share the very same type.
public struct OperationAttributes: ActivityAttributes {
    public typealias ContentState = OperationContent

    public let kind: OperationKind

    public init(kind: OperationKind) {
        self.kind = kind
    }
}
#endif
