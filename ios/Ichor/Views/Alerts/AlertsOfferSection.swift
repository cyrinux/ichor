import SwiftUI
import UIKit
import IchorCore

/// Background alerts, offered once on the home after the first real cluster (AlertsOffer): the
/// notification permission is asked here, when the person chose to be paged, not at install.
/// Refused, it points at the Settings app instead of asking again. "Not now" leaves the toggle
/// in Settings → Monitoring. Renders nothing when the offer does not apply.
struct AlertsOfferSection: View {
    @Environment(AppModel.self) private var model
    @AppStorage(AlertsOffer.pendingKey) private var pending = false
    @State private var alertsEnabled = BackgroundMonitor.alertsEnabled
    @State private var blocked = false

    var body: some View {
        if AlertsOffer.shouldShow(
            pending: pending, alertsEnabled: alertsEnabled,
            isDemo: model.activeSummary?.demo ?? true, tourActive: DemoTour.pending)
        {
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Label("Get paged when a node goes down", systemImage: "bell.badge").font(.headline)
                    Text(
                        "Ichor checks your clusters in the background and notifies you when a node, the API or a volume needs a look. Nothing leaves your phone."
                    )
                    .font(.callout)
                    if blocked {
                        Text("Notifications are off for Ichor: allow them in the Settings app.")
                            .font(.footnote).foregroundStyle(.red)
                    }
                    HStack {
                        Spacer()
                        Button("Not now") {
                            AlertsOffer.dismiss(); pending = false
                        }
                        .buttonStyle(.borderless)
                        if blocked {
                            Button("Open notification settings") {
                                if let url = URL(string: UIApplication.openSettingsURLString) {
                                    UIApplication.shared.open(url)
                                }
                            }
                            .buttonStyle(.borderedProminent)
                        } else {
                            Button("Turn on alerts") { turnOn() }
                                .buttonStyle(.borderedProminent)
                        }
                    }
                }
                .padding(.vertical, 4)
            }
            .onAppear { alertsEnabled = BackgroundMonitor.alertsEnabled }
        }
    }

    private func turnOn() {
        Task {
            guard await BackgroundMonitor.requestPermission() else {
                blocked = true
                return
            }
            BackgroundMonitor.alertsEnabled = true
            BackgroundMonitor.schedule()
            alertsEnabled = true
            AlertsOffer.dismiss()
            pending = false
        }
    }
}
