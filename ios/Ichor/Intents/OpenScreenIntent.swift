import AppIntents
import Foundation

/// "Open in Ichor" in the Shortcuts app (and Siri, with the phrase a shortcut is given): opens
/// the screen a share link names, like tapping the link. Runs in the app's process
/// (openAppWhenRun); the link only becomes pending here: the app checks it (ParseShareLink,
/// either the ichor://open? or the https form) and opens it once unlocked (openShareLink), so
/// the app lock is never bypassed.
struct OpenScreenIntent: AppIntent {
    static let title: LocalizedStringResource = "Open in Ichor"
    static let description = IntentDescription("Opens the screen a share link leads to, once Ichor is unlocked.")
    static let openAppWhenRun = true

    @Parameter(title: "Link")
    var link: URL

    init() {}

    init(link: URL) {
        self.link = link
    }

    /// NotificationRouter is main-actor isolated, like the scene delegate that also feeds it.
    @MainActor
    func perform() async throws -> some IntentResult {
        NotificationRouter.shared.pendingShareLink = link
        return .result()
    }
}
