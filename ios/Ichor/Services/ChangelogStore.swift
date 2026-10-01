import Foundation
import IchorCore

/// The release history bundled at build time (changelog.json, from scripts/changelog.py) and
/// the build launched last time, to tell what is new after an update.
enum ChangelogStore {
    private static let lastBuildKey = "lastLaunchedBuild"

    /// Empty when the file is missing or corrupt.
    static func load() -> Changelog {
        decodeChangelog(Bundle.main.url(forResource: "changelog", withExtension: "json").flatMap { try? Data(contentsOf: $0) })
    }

    /// CFBundleVersion, the commit count of the build; nil when it is not a number.
    static var currentBuild: Int? {
        (Bundle.main.infoDictionary?["CFBundleVersion"] as? String).flatMap { Int($0) }
    }

    static var currentVersion: String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? ""
    }

    /// The releases to present after an update (see whatsNewReleases). When there is nothing
    /// to present (first install, same build, downgrade, no notes) the build is stored right
    /// away; otherwise the caller stores it once the notes were dismissed.
    static func pendingWhatsNew() -> [ChangelogRelease] {
        guard let current = currentBuild else { return [] }
        let previous = UserDefaults.standard.object(forKey: lastBuildKey) as? Int
        let releases = whatsNewReleases(previousBuild: previous, currentBuild: current, releases: load().releases)
        if releases.isEmpty { storeCurrentBuild() }
        return releases
    }

    static func storeCurrentBuild() {
        guard let current = currentBuild else { return }
        UserDefaults.standard.set(current, forKey: lastBuildKey)
    }
}
