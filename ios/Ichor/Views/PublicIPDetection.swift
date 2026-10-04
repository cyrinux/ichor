import SwiftUI

/// Said before anything runs on the cluster to find the public IPs Talos does not know.
let publicIPDetectionNotice: LocalizedStringKey = "Talos does not know the public IP of every node. Ichor can start a short-lived pod on each node, in a temporary namespace deleted afterwards, that asks ifconfig.co (else api.ipify.org or icanhazip.com) which address it comes from. Pulling the curl image can take a minute."

/// In the nodes section's header: find the public IPs Talos does not know, or the run going on.
struct DetectPublicIPsButton: View {
    let running: Bool
    let start: () -> Void

    var body: some View {
        if running {
            ProgressView().controlSize(.small).accessibilityLabel(Text("Finding public IPs…"))
        } else {
            Button("Find public IPs", action: start).font(.caption).textCase(nil)
        }
    }
}
