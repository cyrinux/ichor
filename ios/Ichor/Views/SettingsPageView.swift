import SwiftUI
import IchorCore

/// One group of Settings on its own page, titled like its row in the list.
struct SettingsPageView: View {
    let page: SettingsPage
    @Environment(AppModel.self) private var model

    var body: some View {
        Form {
            switch page {
            case .appearance:
                AppearanceSection()
                LiveStatsSection()
            case .security: SecuritySection()
            case .privacy: PrivacySection()
            case .alerts: MonitoringSection()
            case .ai: AISection()
            // The row is hidden without the feature; a stale path shows an empty page.
            case .kubernetes: if model.allows(.kubeconfig) { KubeconfigSection() }
            case .clusters: ClustersSection()
            case .support: SupportSection()
            case .about:
                AboutSection()
                UpdatesSection()
            }
        }
        .themedBackground()
        .navigationTitle(Text(page.title))
    }
}

// The words of each row, as literals so the string catalog sees them; the same pages as
// Android's SettingsPage.kt.
extension SettingsPage {
    var title: LocalizedStringKey {
        switch self {
        case .appearance: "Appearance"
        case .security: "Security"
        case .privacy: "Privacy"
        case .alerts: "Alerts"
        case .ai: "AI diagnosis"
        case .kubernetes: "Kubernetes"
        case .clusters: "Clusters & backup"
        case .support: "Support"
        case .about: "About"
        }
    }

    var summary: LocalizedStringKey {
        switch self {
        case .appearance: "Theme, language, live usage"
        case .security: "App lock, security keys"
        case .privacy: "Screenshot mode, cluster events, app icons, last known state"
        case .alerts: "Background checks, watches, thresholds"
        case .ai: "Provider, API key, model"
        case .kubernetes: "Node debug shell, kubeconfig export"
        case .clusters: "Add, renew, back up, delete"
        case .support: "Support bundle, activity log, report a bug"
        case .about: "Version, what's new, licenses, updates"
        }
    }
}
