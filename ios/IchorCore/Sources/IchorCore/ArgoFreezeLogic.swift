import Foundation

// What the freeze screens derive from ArgoStatus: who a freeze would stop, the windows screen's
// sections, the reminders to schedule. The design is in the Linear plan document "D9. Argo CD freeze: hotfix live without being reverted".

/// How much of its project a freeze around an app covers.
public enum FreezeScope: String, Sendable, CaseIterable, Hashable, Identifiable {
    case app, namespace, project

    public var id: String { rawValue }

    /// A namespace freeze needs the app to deploy to one.
    public func available(for app: ArgoApp) -> Bool { self != .namespace || !app.destination.namespace.isEmpty }
}

/// The durations the freeze sheet offers, in minutes (plus "until 09:00").
public let freezeDurations = [15, 60, 240]
public let defaultFreezeMinutes = 60
public let freezeMinMinutes = 5
public let freezeMaxMinutes = 7 * 24 * 60
/// What "+1 h" adds to a running freeze.
public let freezeExtendMinutes = 60

/// The options KubeArgoFreeze takes for a freeze of scope around app.
public func freezeOptions(for app: ArgoApp, scope: FreezeScope, minutes: Int, manualSync: Bool, reason: String) -> ArgoFreezeOptions {
    ArgoFreezeOptions(
        applications: scope == .app ? [app.name] : scope == .project ? ["*"] : [],
        namespaces: scope == .namespace ? [app.destination.namespace] : [],
        minutes: minutes,
        manualSync: manualSync,
        reason: reason.trimmingCharacters(in: .whitespacesAndNewlines)
    )
}

/// Minutes from now to the next hour:00 in calendar's time zone, within what a freeze may last.
public func minutesUntil(hour: Int, from now: Date, calendar: Calendar = .current) -> Int {
    var target = calendar.date(bySettingHour: hour, minute: 0, second: 0, of: now) ?? now
    if target <= now.addingTimeInterval(TimeInterval(freezeMinMinutes * 60)) {
        target = calendar.date(byAdding: .day, value: 1, to: target) ?? target
    }
    let minutes = Int((target.timeIntervalSince(now) / 60).rounded(.up))
    return min(max(minutes, freezeMinMinutes), freezeMaxMinutes)
}

/// A window with its project, for the windows screen, the banners and the reminders.
public struct ProjectWindow: Equatable, Identifiable, Sendable {
    public let project: ArgoProject
    public let window: ArgoWindow

    public var id: String { "\(project.id)/\(window.id)" }
}

public enum WindowSection: String, Sendable, CaseIterable, Identifiable {
    case active, upcoming, expired

    public var id: String { rawValue }
}

/// The resources Argo CD puts back as Git has them once a freeze ends: the hand-made changes.
public extension ArgoApp {
    var drifted: [ArgoResource] { resources.filter { $0.sync == .outOfSync } }
}

public extension ArgoStatus {
    /// The project app belongs to: the one with its name, in the app's namespace first.
    func project(of app: ArgoApp) -> ArgoProject? {
        let same = projects.filter { $0.name == app.project }
        return same.first { $0.namespace == app.namespace } ?? same.first
    }

    /// The apps a freeze of scope around app stops: its project's, matched as Argo CD would.
    func freezeTargets(around app: ArgoApp, scope: FreezeScope) -> [ArgoApp] {
        apps.filter { other in
            guard other.project == app.project else { return false }
            switch scope {
            case .app: return other.name == app.name
            case .namespace: return other.destination.namespace == app.destination.namespace
            case .project: return true
            }
        }
        .sorted { $0.name < $1.name }
    }

    private var allWindows: [ProjectWindow] {
        projects.flatMap { p in p.windows.map { ProjectWindow(project: p, window: $0) } }
    }

    /// The windows screen: active windows ending first, then the others by next start (unreadable
    /// ones last), then Ichor's ended freezes.
    func windowSections() -> [WindowSection: [ProjectWindow]] {
        let all = allWindows
        let expired = all.filter { $0.window.ichor?.expired == true }
        let live = all.filter { $0.window.ichor?.expired != true }
        return [
            .active: live.filter(\.window.active).sorted { $0.window.endsAt < $1.window.endsAt },
            .upcoming: live.filter { !$0.window.active }.sorted {
                ($0.window.start == 0 ? 1 : 0, $0.window.start) < ($1.window.start == 0 ? 1 : 0, $1.window.start)
            },
            .expired: expired.sorted { $0.window.endsAt > $1.window.endsAt },
        ]
    }

    /// The deny windows freezing apps now (Ichor's ended ones aside), ending first.
    var activeFreezes: [ProjectWindow] { (windowSections()[.active] ?? []).filter(\.window.isDeny) }

    /// Ichor's freezes still running, for the reminders before they end.
    var runningIchorFreezes: [ProjectWindow] { activeFreezes.filter { $0.window.ichor != nil } }

    /// Projects holding an ended Ichor freeze: the app clears them before they come back next year.
    var projectsToClear: [ArgoProject] { projects.filter { $0.windows.contains { $0.ichor?.expired == true } } }

    /// The windows freezing app, with its project.
    func freezeWindows(of app: ArgoApp) -> [ProjectWindow] {
        guard let freeze = app.freeze, let project = project(of: app) else { return [] }
        return project.windows.filter { freeze.windows.contains($0.id) }.map { ProjectWindow(project: project, window: $0) }
    }

    /// The app that would undo a hand-made change to kind namespace/name within minutes: it
    /// deploys that resource with auto-sync and self-heal on, and is not frozen.
    func selfHealingOwner(kind: String, namespace: String, name: String) -> ArgoApp? {
        apps.first { app in
            app.autoSync.enabled && app.autoSync.selfHeal && app.freeze == nil &&
                app.resources.contains { $0.kind == kind && $0.namespace == namespace && $0.name == name }
        }
    }
}
