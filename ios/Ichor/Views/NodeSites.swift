import SwiftUI
import IchorCore

extension TopologySite {
    /// "🇫🇷  fr-par-1", or what kind of network it is when it has no label. Same as Android's siteTitle.
    var title: String {
        let label = self.label.trimmingCharacters(in: .whitespaces).isEmpty
            ? (kind == "lan" ? String(localized: "Local network") : String(localized: "Own network"))
            : self.label
        return [countryFlag(country), label].filter { !$0.isEmpty }.joined(separator: "  ")
    }
}

extension NodeGroup {
    /// The site's title, or the nodes the map does not place.
    var title: String { site?.title ?? String(localized: "Not on the map") }
}

/// A site's name over its nodes in the overview's nodes section and the Nodes screen.
struct SiteHeader: View {
    let title: String

    var body: some View {
        Text(verbatim: title)
            .font(.subheadline.weight(.medium))
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .padding(.top, 4)
            .accessibilityAddTraits(.isHeader)
    }
}
