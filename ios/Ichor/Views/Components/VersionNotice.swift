import SwiftUI
import IchorCore

extension VersionNotice {
    /// "Needs Talos v1.8 or newer", or the generic sentence when the version is unknown.
    var localizedMessage: String {
        minVersion.isEmpty
            ? String(localized: "Not available on this node's Talos version")
            : String(localized: "Needs Talos \(minVersion) or newer")
    }
}

extension FeatureSupport {
    /// What to show for an unsupported feature: the version it needs, else Go's reason, else
    /// the generic sentence. Nil when supported.
    var localizedNotice: String? {
        guard let notice else { return nil }
        if notice.minVersion.isEmpty && !reason.isEmpty { return reason }
        return notice.localizedMessage
    }
}

/// Full-screen information for something this node's Talos version cannot do: not styled as
/// an error, but a failed request can still be retried (the node may have been upgraded).
/// `detail` is Go's message, shown when it says more than the version.
struct VersionNoticeView: View {
    let notice: VersionNotice
    var detail = ""
    var retry: (() async -> Void)?

    var body: some View {
        ContentUnavailableView {
            Label("Not available on this node", systemImage: "info.circle")
        } description: {
            Text(notice.localizedMessage)
            // Go's own words when they say more than the version (e.g. which version the node runs).
            if notice.minVersion.isEmpty && !detail.isEmpty { Text(verbatim: detail).font(.caption) }
        } actions: {
            if let retry { Button("Retry") { Task { await retry() } } }
        }
    }
}

/// The same information as a list row (a secondary info row inside a screen that has more).
struct VersionNoticeRow: View {
    let text: String

    var body: some View {
        Label {
            Text(text).font(.footnote).foregroundStyle(.secondary)
        } icon: {
            Image(systemName: "info.circle").foregroundStyle(.secondary)
        }
    }
}

/// A menu or list entry for a feature that depends on the node's Talos version: unsupported
/// entries stay visible, disabled, with "Needs Talos vX or newer" under the title.
struct FeatureButton: View {
    let title: String
    let systemImage: String
    let support: FeatureSupport
    var role: ButtonRole?
    let action: () -> Void

    var body: some View {
        Button(role: role, action: action) {
            Label(title, systemImage: systemImage)
            if let notice = support.localizedNotice { Text(notice) }
        }
        .disabled(!support.supported)
    }
}

/// Replaces `content` with the version notice when the node's Talos version lacks `feature`.
struct FeatureGated<Content: View>: View {
    let support: FeatureSupport
    @ViewBuilder let content: () -> Content

    var body: some View {
        if let notice = support.notice {
            VersionNoticeView(notice: notice, detail: support.reason)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            content()
        }
    }
}

/// An error line inside a screen: red for a failure, a secondary info row when it only says
/// that this node's Talos version cannot do it.
struct ErrorOrNoticeText: View {
    let message: String

    var body: some View {
        if let notice = versionNotice(message) {
            VersionNoticeRow(text: notice.minVersion.isEmpty ? message : notice.localizedMessage)
        } else {
            Text(message).font(.footnote).foregroundStyle(.statusBad)
        }
    }
}
