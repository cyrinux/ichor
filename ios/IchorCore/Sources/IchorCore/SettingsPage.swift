/// The groups of Settings, in the order of the top-level list; each opens its own page.
/// Same pages and order as Android's `SettingsPage.kt`. The localized titles and subtitles
/// live in the app (`SettingsPageText.swift`), where the string catalog sees them.
public enum SettingsPage: String, CaseIterable, Hashable, Sendable {
    case appearance
    case security
    case privacy
    case alerts
    case ai
    case kubernetes
    case clusters
    case support
    case about

    /// The SF Symbol of the page's row.
    public var systemImage: String {
        switch self {
        case .appearance: "paintpalette"
        case .security: "lock"
        case .privacy: "eye.slash"
        case .alerts: "bell.badge"
        case .ai: "sparkles"
        case .kubernetes: "terminal"
        case .clusters: "externaldrive"
        case .support: "questionmark.circle"
        case .about: "info.circle"
        }
    }

    /// The rows of the list: Kubernetes only when the active cluster allows the kubeconfig
    /// feature, hidden otherwise, like its section was.
    public static func visible(kubeconfig: Bool) -> [SettingsPage] {
        allCases.filter { $0 != .kubernetes || kubeconfig }
    }
}
