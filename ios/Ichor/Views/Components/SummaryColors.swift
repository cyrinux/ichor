import SwiftUI
import UIKit
import IchorCore

/// The cluster's container colors on the home summary (IchorCore's ClusterPalette, Android's
/// primaryContainer / secondaryContainer roles): the identity area (logo, name, headline) and
/// the non-status icon containers. Status pills, health colours and metric values never take
/// them: the cluster hue can look like a warning or a success.
struct SummaryColors: Equatable {
    let identity: Color
    let onIdentity: Color
    let iconContainer: Color
    let onIconContainer: Color

    init(_ palette: ClusterPalette) {
        identity = Color(uiColor: UIColor(rgb: palette.primaryContainer))
        onIdentity = Color(uiColor: UIColor(rgb: palette.onPrimaryContainer))
        iconContainer = Color(uiColor: UIColor(rgb: palette.secondaryContainer))
        onIconContainer = Color(uiColor: UIColor(rgb: palette.onSecondaryContainer))
    }
}

extension AppModel {
    /// The colors of the cluster on screen for `colorScheme`. Computed on every read from the
    /// observed seed and theme, never kept: switching cluster or editing its hue shows at once.
    func summaryColors(for colorScheme: ColorScheme) -> SummaryColors {
        let appearance: PaletteAppearance =
            settings.theme == .black ? .trueBlack : (colorScheme == .dark ? .dark : .light)
        return SummaryColors(ClusterPalette(seed: seed(of: activeSummary), appearance: appearance))
    }
}

private struct SummaryColorsKey: EnvironmentKey {
    static let defaultValue: SummaryColors? = nil
}

extension EnvironmentValues {
    /// Set by the home summaries: StatTile icons then sit in a cluster-tinted container.
    var summaryColors: SummaryColors? {
        get { self[SummaryColorsKey.self] }
        set { self[SummaryColorsKey.self] = newValue }
    }
}

/// The summary's identity area, as Android's SummaryIdentityBand: its content in the
/// on-container color on a cluster-tinted band edge to edge across the top of the list row
/// (the section rounds its top corners, as Android's card does).
private struct IdentityArea: ViewModifier {
    let colors: SummaryColors

    func body(content: Content) -> some View {
        content
            .foregroundStyle(colors.onIdentity)
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, minHeight: 48, alignment: .leading)
            .background(colors.identity)
    }
}

/// The summary under its band: the row's inset, and the row separator starting there.
private struct SummaryBody: ViewModifier {
    func body(content: Content) -> some View {
        content
            .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            .padding(.horizontal, 16)
            .padding(.top, 12)
            .padding(.bottom, 10)
    }
}

/// Stat tiles side by side on one height, as Android's StatTileRow: the meters a tile pins
/// to its bottom stay level whatever the labels above them wrap to.
struct StatTileRow<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        HStack(alignment: .top, spacing: 12) { content }
            .fixedSize(horizontal: false, vertical: true)
    }
}

extension View {
    func identityArea(_ colors: SummaryColors) -> some View { modifier(IdentityArea(colors: colors)) }

    /// The body of a home summary card, under its identity band.
    func summaryBody() -> some View { modifier(SummaryBody()) }

    /// A home summary card's list row: no inset, so the identity band reaches the row's edges.
    func summaryRow() -> some View { listRowInsets(EdgeInsets()) }

    /// One tile of a StatTileRow: an equal share of the width, the row's full height.
    func statTileCell() -> some View { frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading) }
}

/// A StatTile's icon: in the cluster-tinted container on a home summary, else in the tint.
struct StatIcon: View {
    let systemName: String

    @Environment(\.summaryColors) private var colors

    var body: some View {
        if let colors {
            Image(systemName: systemName)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(colors.onIconContainer)
                .frame(width: 28, height: 28)
                .background(colors.iconContainer, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                .accessibilityHidden(true)
        } else {
            Image(systemName: systemName).foregroundStyle(.tint).accessibilityHidden(true)
        }
    }
}
