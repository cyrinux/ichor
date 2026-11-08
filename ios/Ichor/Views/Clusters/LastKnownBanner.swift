import SwiftUI
import IchorCore

/// Above the node list when no node answers but some were seen before (the last known state):
/// replaces ClusterUnreachableView, with the same cause, icon, retries and first fix.
struct LastKnownBanner: View {
    let outage: ClusterOutage
    let recovery: OutageRecovery
    let retry: () async -> Void
    let openImport: () -> Void

    @Environment(AppModel.self) private var model
    @State private var signingIn: KubeSignInTarget?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label {
                Text("Can’t reach the cluster").font(.headline)
            } icon: {
                Image(systemName: outage.cause.icon).foregroundStyle(outage.cause.tint)
            }
            Text("Showing the last known state.").font(.callout)
            // The likely fix is on the phone: say which.
            if outage.cause == .network {
                Text(outage.cause.explanation).font(.footnote).foregroundStyle(.secondary)
            }
            RecoveryButtons(
                recovery: recovery, retry: retry,
                signIn: model.activeSignInTarget.map { target in { signingIn = target } },
                openImport: openImport, compact: true
            )
            .tint(outage.cause.tint)
            if let explanation = recovery.explanation {
                Text(explanation.text).font(.footnote).foregroundStyle(.secondary)
            }
            Text("Tries again every \(unreachableRetrySeconds) s, and as soon as the network changes.")
                .font(.caption2)
                .foregroundStyle(.secondary)
            if outage.cause == .network { LocalNetworkNotice() }
        }
        .padding(.vertical, 4)
        .sheet(item: $signingIn) { target in
            KubeSignInSheet(target: target) { Task { await retry() } }
        }
    }
}
