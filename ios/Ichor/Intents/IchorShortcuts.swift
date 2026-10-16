import AppIntents

/// The app's only AppShortcutsProvider (it must live in the app target, not the widget): lists
/// "Open in Ichor" under Ichor in the Shortcuts app. The phrases are translated in
/// AppShortcuts.xcstrings.
struct IchorShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: OpenScreenIntent(),
            phrases: [
                "Open \(.applicationName)",
                "Open a link in \(.applicationName)",
            ],
            shortTitle: "Open in Ichor",
            systemImageName: "link"
        )
    }
}
