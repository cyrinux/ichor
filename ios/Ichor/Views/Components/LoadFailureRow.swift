import SwiftUI
import IchorCore

/// A section, card or sheet whose read failed: a warning glyph, what went wrong in muted text and
/// a Retry that reruns it when `retry` is given. Red stays for what is red in the cluster and for
/// invalid input. `saying` false shows `message` as it is, for a failure that is not a read (a save,
/// a connection test) or that already says what failed. Same as Android's LoadFailureRow.
struct LoadFailureRow: View {
    let message: String
    var retry: (() async -> Void)?
    var compact = false
    var saying = true

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle")
                .foregroundStyle(.statusWarn)
                .accessibilityHidden(true)
            Text(verbatim: text)
                .font(compact ? .caption : .callout)
                .foregroundStyle(.secondary)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let retry {
                Button("Retry") {
                    Task { await retry() }
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            }
        }
        .accessibilityElement(children: .combine)
        .onAppear { announce(text) }
    }

    private var text: String {
        // Not failures: what this node's Talos version cannot do, a cluster that needs a sign-in.
        if let notice = versionNotice(message) { return notice.localizedMessage }
        if let reason = kubeSignInRequiredReason(message), !reason.isEmpty { return reason }
        return saying ? String(localized: "Couldn't load: \(message)") : message
    }
}
