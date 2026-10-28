import Foundation
import SwiftUI
import IchorCore

/// The objects opened lately, newest first (the search's "Recently opened"), kept in the
/// standard defaults in the JSON Android keeps too. Dropped with the recent searches, never in
/// the backup, and nothing is recorded in screenshot mode (labels are stored as shown).
@Observable
@MainActor
final class RecentTargetStore {
    static let shared = RecentTargetStore()
    private static let key = "search.recentTargets"

    private(set) var list = RecentTargetList.parse(UserDefaults.standard.string(forKey: RecentTargetStore.key))

    /// Records `target`, on the active cluster, as opened now.
    func record(_ target: ShareTarget, label: String, detail: String = "", model: AppModel) {
        guard !model.privacyMask, let cluster = model.activeSummary?.clusterID, !cluster.isEmpty else { return }
        var opened = target
        opened.cluster = cluster
        let at = Int64((Date().timeIntervalSince1970 * 1000).rounded())
        save(list.adding(RecentTarget(target: opened, label: label, detail: detail, at: at)))
    }

    func remove(_ target: ShareTarget) { save(list.removing(target)) }

    func clear() { save(RecentTargetList()) }

    private func save(_ next: RecentTargetList) {
        guard next != list else { return }
        list = next
        if next.targets.isEmpty {
            UserDefaults.standard.removeObject(forKey: RecentTargetStore.key)
        } else {
            UserDefaults.standard.set(next.encoded, forKey: RecentTargetStore.key)
        }
    }
}

/// Records the object a screen shows among the recently opened, once it appears.
private struct RecordsOpened: ViewModifier {
    let target: ShareTarget?
    let label: String
    let detail: String
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        content.task(id: target) {
            if let target { RecentTargetStore.shared.record(target, label: label, detail: detail, model: model) }
        }
    }
}

extension View {
    /// Records `target` (nil: nothing) as opened when the screen shows.
    func recordsOpened(_ target: ShareTarget?, label: String, detail: String = "") -> some View {
        modifier(RecordsOpened(target: target, label: label, detail: detail))
    }
}
