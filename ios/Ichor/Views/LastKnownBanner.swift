import SwiftUI
import IchorCore

/// Above the node list when no node answers but some were seen before (the last known state):
/// replaces ClusterUnreachableView, with the same cause, icon and retries.
struct LastKnownBanner: View {
    let outage: ClusterOutage
    let retry: () async -> Void

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
            HStack {
                Button("Retry") { Task { await retry() } }
                    .buttonStyle(.bordered)
                    .tint(outage.cause.tint)
                Spacer()
                Text("Tries again every \(unreachableRetrySeconds) s, and as soon as the network changes.")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.trailing)
            }
        }
        .padding(.vertical, 4)
    }
}
