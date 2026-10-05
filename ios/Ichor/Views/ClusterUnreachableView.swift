import SwiftUI
import IchorCore

/// Replaces the list of red "unreachable" nodes when none answered: one explanation of why
/// (usually the phone is off the cluster's network), the error once, and what to do about it.
struct ClusterUnreachableView: View {
    let outage: ClusterOutage
    let endpoints: [String]
    let retry: () async -> Void
    let showNodes: () -> Void

    @Environment(AppModel.self) private var model
    @State private var editing: ContextSummary?
    @State private var scanning = false

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                Image(systemName: outage.cause.icon)
                    .font(.system(size: 44))
                    .foregroundStyle(outage.cause.tint)
                    .accessibilityHidden(true)
                Text("Can’t reach the cluster").font(.title2.bold()).multilineTextAlignment(.center).accessibilityAddTraits(.isHeader)
                Text(outage.cause.explanation).font(.callout).multilineTextAlignment(.center)
                if !endpoints.isEmpty {
                    Text("Endpoints: \(endpoints.joined(separator: ", "))")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                ForEach(outage.errors, id: \.self) { error in
                    Text(error).font(.caption).foregroundStyle(.statusBad).multilineTextAlignment(.center)
                }
                Button("Retry") { Task { await retry() } }
                    .buttonStyle(.borderedProminent)
                    .padding(.top, 8)
                // Local network access denied is often the cause on Wi-Fi.
                if outage.cause == .network {
                    LocalNetworkNotice(centered: true)
                }
                // A talosconfig shared from elsewhere may list no endpoint reachable from here.
                HStack {
                    if outage.cause == .network {
                        Button("Search the local network") { scanning = true }.buttonStyle(.bordered)
                    }
                    if let editable { Button("Edit endpoints") { editing = editable } }
                }
                // The nodes stay one tap away (Copy IP and the like).
                Button("Show \(outage.nodes) nodes", action: showNodes)
                Text("Tries again every \(unreachableRetrySeconds) s, and as soon as the network changes.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .padding(32)
            .frame(maxWidth: .infinity)
        }
        .refreshable { await retry() }
        .endpointTools(editing: $editing, scanning: $scanning)
    }

    /// The cluster on screen, unless its endpoints cannot be edited: in screenshot mode the
    /// editor would show fake endpoints; the demo has none.
    private var editable: ContextSummary? {
        guard !model.labels.masked, let context = model.activeSummary, !context.demo else { return nil }
        return context
    }
}

extension OutageCause {
    var icon: String {
        switch self {
        case .network: "wifi.exclamationmark"
        case .credentials: "lock.shield"
        case .other: "exclamationmark.triangle"
        }
    }

    /// Orange when the fix is likely on the phone (network), red otherwise.
    var tint: Color { self == .network ? .orange : .red }

    var explanation: String {
        switch self {
        case .network:
            String(localized: "No node answered. Your phone is probably not on the cluster’s network: check that your VPN is connected, or that you are on the right Wi-Fi.")
        case .credentials:
            String(localized: "The cluster answered but refused this talosconfig: its certificate, CA or role may be wrong or expired.")
        case .other:
            String(localized: "No node answered. The details are below.")
        }
    }
}
