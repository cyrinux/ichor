import SwiftUI

/// The cluster on screen is reached over a VPN only and none is up: a warning rather than an
/// error. The screen reloads by itself once the VPN connects (VpnMonitor). iOS has no public
/// link to its VPN settings, so only a retry is offered.
struct VpnRequiredView: View {
    let retry: () async -> Void

    var body: some View {
        ContentUnavailableView {
            Label {
                Text("VPN only")
            } icon: {
                Image(systemName: "lock.shield").foregroundStyle(.orange)
            }
        } description: {
            Text("This cluster is set to be reached over a VPN only, and no VPN is connected. Connect it: this screen reloads by itself.")
        } actions: {
            Button("Retry") { Task { await retry() } }
        }
    }
}
