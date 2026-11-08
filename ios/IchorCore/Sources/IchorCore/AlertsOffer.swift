import Foundation

/// The home's one-time offer of background alerts (AlertsOfferSection): pending once the first
/// real cluster is added, until turned on or dismissed; never on the demo nor over its tour.
/// Same rule and keys' meaning as Android's AlertsOffer.kt / UiPreferences.
public enum AlertsOffer {
    public static let pendingKey = "alerts.offerPending"
    static let offeredKey = "alerts.offered"

    public static func shouldShow(pending: Bool, alertsEnabled: Bool, isDemo: Bool, tourActive: Bool) -> Bool {
        pending && !alertsEnabled && !isDemo && !tourActive
    }

    /// Offers alerts the first time; later calls (another cluster added) do nothing.
    public static func offerOnce(_ defaults: UserDefaults = .standard) {
        guard !defaults.bool(forKey: offeredKey) else { return }
        defaults.set(true, forKey: offeredKey)
        defaults.set(true, forKey: pendingKey)
    }

    /// Turned on or "Not now": the offer is gone for good (Settings → Monitoring stays).
    public static func dismiss(_ defaults: UserDefaults = .standard) {
        defaults.set(true, forKey: offeredKey)
        defaults.set(false, forKey: pendingKey)
    }
}
