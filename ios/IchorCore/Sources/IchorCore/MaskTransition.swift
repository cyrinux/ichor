import Foundation

/// The order in which the screenshot mode is turned on or off, so that no screen draws a name
/// under the wrong setting (same as Android's TalosApp.setPrivacyMask): the placeholder goes
/// up first (`begin`), the data held under the old names is dropped (`dropData`), then Go's
/// mask changes (`applyMask`) and the config is read again (`reread`, which reads the privacy
/// generation before parsing). `end` takes the placeholder down, also when `reread` throws.
@MainActor
public enum MaskTransition {
    public static func run(
        begin: () -> Void,
        dropData: () -> Void,
        applyMask: () -> Void,
        reread: () async throws -> Void,
        end: () -> Void
    ) async rethrows {
        begin()
        defer { end() }
        dropData()
        applyMask()
        try await reread()
    }
}

/// Whether a delivered notification is one of the monitor's (an alert of any kind, "private"
/// with the app lock, a Wake-on-LAN answer: "wake|…"), which name nodes and clusters: removed
/// when the screenshot mode turns on. The jobs' and freeze reminders' ones are left.
public func notificationNamesClusters(category: String, identifier: String) -> Bool {
    category.hasPrefix("alert.") || category == "private" || identifier.hasPrefix("wake|")
}

/// The navigation path kept when the screenshot mode switches: only the settings pages on top
/// of it (where the switch is); every screen under them holds names loaded under the old
/// setting (titles, rows, routes), so it goes.
public func pathKeptOnMaskSwitch<Route>(_ path: [Route], isSettings: (Route) -> Bool) -> [Route] {
    Array(path.reversed().prefix(while: isSettings).reversed())
}
