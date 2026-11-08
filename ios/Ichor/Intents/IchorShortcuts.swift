import AppIntents

/// The app's only AppShortcutsProvider (it must live in the app target, not the widget): lists
/// "Open in Ichor", "Cluster status", "Open cluster" and "Run a checkup" under Ichor in the
/// Shortcuts app. The phrases are translated in AppShortcuts.xcstrings; those naming a cluster
/// are refreshed when the cluster list changes (SharedStore.publish).
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
        AppShortcut(
            intent: ClusterStatusIntent(),
            phrases: [
                "What is the state of my cluster in \(.applicationName)",
                "What is the state of \(\.$cluster) in \(.applicationName)",
            ],
            shortTitle: "Cluster status",
            systemImageName: "server.rack"
        )
        AppShortcut(
            intent: OpenClusterIntent(),
            phrases: [
                "Open my cluster in \(.applicationName)",
                "Open \(\.$cluster) in \(.applicationName)",
            ],
            shortTitle: "Open cluster",
            systemImageName: "rectangle.stack"
        )
        AppShortcut(
            intent: RunCheckupIntent(),
            phrases: [
                "Run a checkup in \(.applicationName)",
                "Run a checkup of \(\.$cluster) in \(.applicationName)",
            ],
            shortTitle: "Run a checkup",
            systemImageName: "stethoscope"
        )
    }
}
