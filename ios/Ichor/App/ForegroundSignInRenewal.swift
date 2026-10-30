import Foundation

/// Renews every sign-in as the app comes to the front, at most every five minutes: the first
/// screen finds a fresh token instead of paying the refresh, and a session the identity provider
/// ended shows its banner at once (same rules as Android).
@MainActor
enum ForegroundSignInRenewal {
    private static let interval: TimeInterval = 5 * 60
    private static var last: Date?

    static func run(model: AppModel) {
        let now = Date()
        if let last, now.timeIntervalSince(last) < interval { return }
        last = now
        Task.detached(priority: .utility) {
            guard let renewals = await TalosClient.renewStoredSignIns() else { return }
            // What was loaded with the ended session goes: the screens reload and show the banner.
            if renewals.contains(where: \.needsUser) {
                await MainActor.run { model.reloadKubernetes() }
            }
        }
    }
}
