import SwiftUI
import IchorCore

/// Replaces the list of red "unreachable" nodes when none answered: one explanation of why
/// (usually the phone is off the cluster's network), the error once, and what to do about it.
struct ClusterUnreachableView: View {
    let outage: ClusterOutage
    let endpoints: [String]
    let retry: () async -> Void
    let showNodes: () -> Void
    /// The import screen, for a renewed talosconfig.
    let openImport: () -> Void

    @Environment(AppModel.self) private var model
    @State private var editing: ContextSummary?
    @State private var scanning = false
    @State private var signingIn: KubeSignInTarget?

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                Image(systemName: outage.cause.icon)
                    .font(.system(size: 44))
                    .foregroundStyle(outage.cause.tint)
                    .accessibilityHidden(true)
                Text("Can’t reach the cluster").font(.title2.bold()).multilineTextAlignment(.center)
                    .accessibilityAddTraits(.isHeader)
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
                RecoveryButtons(recovery: recovery, retry: retry, signIn: signIn, openImport: openImport)
                    .padding(.top, 8)
                if let explanation = recovery.explanation {
                    Text(explanation.text).font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
                }
                // Local network access denied is often the cause on Wi-Fi.
                if outage.cause == .network {
                    LocalNetworkNotice(centered: true)
                }
                // A talosconfig shared from elsewhere may list no endpoint reachable from here.
                HStack {
                    if recovery.scan {
                        Button("Search the local network") { scanning = true }.buttonStyle(.bordered)
                    }
                    if recovery.editEndpoints, let editable { Button("Edit endpoints") { editing = editable } }
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
        .sheet(item: $signingIn) { target in
            KubeSignInSheet(target: target) { Task { await retry() } }
        }
    }

    private var recovery: OutageRecovery { model.outageRecovery(outage) }

    /// The Omni sign-in sheet, when the cluster on screen has one.
    private var signIn: (() -> Void)? {
        model.activeSignInTarget.map { target in { signingIn = target } }
    }

    /// The cluster on screen, unless its endpoints cannot be edited: in screenshot mode the
    /// editor would show fake endpoints; the demo has none.
    private var editable: ContextSummary? {
        guard !model.labels.masked, let context = model.activeSummary, !context.demo else { return nil }
        return context
    }
}

/// The recovery's primary action, then Retry beside it; side by side when they fit, else stacked
/// (long labels at large text sizes).
struct RecoveryButtons: View {
    let recovery: OutageRecovery
    let retry: () async -> Void
    /// Nil when the context has no sign-in to offer: Retry stands alone.
    let signIn: (() -> Void)?
    let openImport: () -> Void
    var compact = false

    var body: some View {
        ViewThatFits {
            HStack(spacing: 12) { buttons }
            VStack(spacing: 8) { buttons }
        }
    }

    @ViewBuilder private var buttons: some View {
        switch recovery.primary {
        case .signIn where signIn != nil:
            prominent(Button("Sign in again") { signIn?() })
            retryButton(prominent: false)
        case .replaceConfig:
            prominent(Button("Import the renewed talosconfig", action: openImport))
            retryButton(prominent: false)
        default:
            retryButton(prominent: !compact)
        }
    }

    @ViewBuilder
    private func prominent(_ button: Button<Text>) -> some View {
        if compact {
            button.buttonStyle(.bordered)
        } else {
            button.buttonStyle(.borderedProminent)
        }
    }

    @ViewBuilder
    private func retryButton(prominent: Bool) -> some View {
        let button = Button("Retry") { Task { await retry() } }
        if prominent {
            button.buttonStyle(.borderedProminent)
        } else {
            button.buttonStyle(.bordered)
        }
    }
}

extension RecoveryExplanation {
    var text: String {
        switch self {
        case .omniSession:
            String(localized: "Your Omni session may have ended. Signing in again keeps this cluster as it is.")
        case .replaceConfig:
            String(
                localized:
                    "Import the talosconfig you renewed with talosctl. If it is for the same cluster and CA, choose Replace when the import offers it: the cluster keeps its name, colour, tags and history. The app cannot renew a certificate itself."
            )
        }
    }
}

extension AppModel {
    /// What the outage notice and banner offer for the cluster on screen.
    func outageRecovery(_ outage: ClusterOutage) -> OutageRecovery {
        let context = activeSummary
        return IchorCore.outageRecovery(
            outage, omni: context?.omni == true, demo: context?.demo == true, masked: labels.masked)
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
            String(
                localized:
                    "No node answered. Your phone is probably not on the cluster’s network: check that your VPN is connected, or that you are on the right Wi-Fi."
            )
        case .credentials:
            String(
                localized:
                    "The cluster answered but refused this talosconfig: its certificate, CA or role may be wrong or expired."
            )
        case .other:
            String(localized: "No node answered. The details are below.")
        }
    }
}
