import SwiftUI

/// Live CPU and memory on the overview: on by default, off to spare data and battery
/// (Android's LiveClusterStatsSetting). Turning it off drops the samples taken so far.
struct LiveStatsSection: View {
    @AppStorage(LiveStatsSettings.key) private var enabled = true

    var body: some View {
        Section {
            Toggle("Live cluster usage", isOn: $enabled)
        } footer: {
            Text("Refresh CPU and memory on the overview every few seconds while it is open. Turn off to save mobile data and battery.")
        }
        .onChange(of: enabled) { _, on in
            if !on { ClusterLiveStore.shared.clear() }
        }
    }
}
