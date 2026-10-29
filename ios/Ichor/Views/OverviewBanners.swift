import SwiftUI
import IchorCore

/// The notices above the overview's cards: the last known state of a cluster no node answers,
/// the demo notice, the tour's first tip, what changed since last looked, the client
/// certificate, the members discovery found and the support ask.
struct OverviewBannerSections: View {
    /// Set when no node answers and the nodes are known from before.
    let lastKnownOutage: ClusterOutage?
    let retry: () async -> Void
    let showsSwitcherTip: Bool
    let look: HistoryLook
    let discovered: Int
    let showDiscovered: () -> Void
    @Environment(AppModel.self) private var model
    @Environment(SupportPrompt.self) private var support

    var body: some View {
        // Offered once, after the first real cluster: first, above everything else.
        AlertsOfferSection()
        if let lastKnownOutage {
            Section { LastKnownBanner(outage: lastKnownOutage, retry: retry) }
        }
        if model.activeSummary?.demo == true {
            Section {
                Text(
                    "Demo cluster · Sample data. Cluster changes are unavailable. Remove the demo from Manage clusters when finished."
                )
                .font(.callout).foregroundStyle(.secondary)
            }
        }
        if showsSwitcherTip {
            Section { DemoTourSwitcherTip() }
        }
        HistorySinceSection(look: look)
        if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
            Section { CertExpiryBanner(notAfter: ctx.certNotAfter) }
        }
        if discovered > 0 {
            Section { DiscoveredNodesBanner(count: discovered, open: showDiscovered) }
        }
        if support.visible { Section { SupportCard(prompt: support) } }
    }
}

/// The client certificate expires within certWarnDays (or has expired): renew it (os:admin;
/// other roles get the role notice there).
private struct CertExpiryBanner: View {
    let notAfter: Int64

    var body: some View {
        let days = daysUntil(notAfter)
        NavigationLink(value: Route.issueConfig(renew: true)) {
            Label {
                Text(
                    days < 0
                        ? String(localized: "The client certificate expired \(-days) days ago.")
                        : String(
                            localized: "The client certificate expires in \(days) days. Generate a new talosconfig."))
            } icon: {
                Image(systemName: "exclamationmark.triangle.fill")
            }
            .foregroundStyle(days < 0 ? Color.red : Color.orange)
        }
    }
}
