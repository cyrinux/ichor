import IchorCore
import UIKit

/// Home Screen quick actions (touch and hold the app icon): one per cluster, opening the app
/// on it. Keyed by the context fingerprint, which survives renames and the screenshot mode.
@MainActor
enum QuickActions {
    private static let clusterType = "name.levis.ichor.cluster"
    private static let searchType = "name.levis.ichor.search"
    private static let fingerprintKey = "fingerprint"
    /// The Home Screen shows 4 actions: the first 3 clusters and "Search clusters".
    private static let maxClusters = 3

    /// One action per cluster of `summary` (the first 3), titled as in the app, then "Search
    /// clusters"; none once no cluster is left.
    static func update(summary: ConfigSummary?, labels: ClusterLabels) {
        var seen = Set<String>()
        let clusters = (summary?.contexts ?? [])
            .filter { !$0.fingerprint.isEmpty && seen.insert($0.fingerprint).inserted }
            .prefix(maxClusters)
            .map { context in
                UIApplicationShortcutItem(
                    type: clusterType,
                    localizedTitle: labels.of(context),
                    // Renamed: which talosconfig context that is.
                    localizedSubtitle: labels.given(context) == nil ? nil : context.name,
                    icon: UIApplicationShortcutIcon(systemImageName: "server.rack"),
                    userInfo: [fingerprintKey: context.fingerprint as NSString]
                )
            }
        let search = UIApplicationShortcutItem(
            type: searchType,
            localizedTitle: String(localized: "Search clusters"),
            localizedSubtitle: nil,
            icon: UIApplicationShortcutIcon(systemImageName: "magnifyingglass"),
            userInfo: nil
        )
        UIApplication.shared.shortcutItems = clusters.isEmpty ? [] : clusters + [search]
    }

    /// Whether `item` is the "Search clusters" action.
    static func isSearch(_ item: UIApplicationShortcutItem) -> Bool { item.type == searchType }

    /// The fingerprint of the cluster `item` opens, if it is one of ours.
    static func fingerprint(of item: UIApplicationShortcutItem) -> String? {
        guard item.type == clusterType else { return nil }
        return item.userInfo?[fingerprintKey] as? String
    }
}

/// Gives the window scene a delegate receiving quick actions; SwiftUI still builds the window.
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        configurationForConnecting session: UISceneSession,
        options: UIScene.ConnectionOptions
    ) -> UISceneConfiguration {
        let configuration = UISceneConfiguration(name: nil, sessionRole: session.role)
        configuration.delegateClass = QuickActionSceneDelegate.self
        return configuration
    }

    /// iOS relaunched the app for the model download's background session (ModelDownloadSession).
    func application(_ application: UIApplication, handleEventsForBackgroundURLSession identifier: String,
                     completionHandler: @escaping () -> Void) {
        guard identifier == ModelDownloadSession.identifier else {
            completionHandler()
            return
        }
        ModelDownloadSession.backgroundCompletion = completionHandler
        _ = ModelDownloadSession.shared
    }

    /// iPhone: portrait, except while a wide screen holds landscape (OrientationLock); iPad: all.
    @MainActor
    func application(_ application: UIApplication, supportedInterfaceOrientationsFor window: UIWindow?) -> UIInterfaceOrientationMask {
        OrientationLock.shared.mask
    }
}

/// A quick action or share link launching the app (cold start) or brought to it while it runs.
@MainActor
final class QuickActionSceneDelegate: NSObject, UIWindowSceneDelegate {
    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
        if let item = connectionOptions.shortcutItem { open(item) }
        if let url = connectionOptions.urlContexts.first?.url { open(url) }
        // A Spotlight result launching the app.
        if let url = connectionOptions.userActivities.lazy.compactMap(SpotlightIndex.link(of:)).first { open(url) }
    }

    /// A Spotlight result chosen while the app runs.
    func scene(_ scene: UIScene, continue userActivity: NSUserActivity) {
        if let url = SpotlightIndex.link(of: userActivity) { open(url) }
    }

    /// A share link opened while the app runs.
    func scene(_ scene: UIScene, openURLContexts URLContexts: Set<UIOpenURLContext>) {
        if let url = URLContexts.first?.url { open(url) }
    }

    func windowScene(
        _ windowScene: UIWindowScene,
        performActionFor shortcutItem: UIApplicationShortcutItem,
        completionHandler: @escaping (Bool) -> Void
    ) {
        completionHandler(open(shortcutItem))
    }

    /// Checked (ParseShareLink) once the app is unlocked, not here. A file is a config opened
    /// with Ichor, for the import preview.
    private func open(_ url: URL) {
        if url.isFileURL {
            IncomingConfig.receive(url)
            return
        }
        if let kind = OperationKind(link: url) {
            NotificationRouter.shared.pendingOperation = kind
            return
        }
        guard url.scheme == "ichor", url.host == "open" else { return }
        NotificationRouter.shared.pendingShareLink = url
    }

    @discardableResult
    private func open(_ item: UIApplicationShortcutItem) -> Bool {
        if QuickActions.isSearch(item) {
            NotificationRouter.shared.pendingSearch = true
            return true
        }
        guard let fingerprint = QuickActions.fingerprint(of: item) else { return false }
        NotificationRouter.shared.pendingCluster = fingerprint
        return true
    }
}
