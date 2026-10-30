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
        let appearance: PaletteAppearance = theme == .black ? .trueBlack : (colorScheme == .dark ? .dark : .light)
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

/// The summary's identity area: its content in the on-container color on a rounded
/// cluster-tinted surface.
private struct IdentityArea: ViewModifier {
    let colors: SummaryColors

    func body(content: Content) -> some View {
        content
            .foregroundStyle(colors.onIdentity)
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(colors.identity, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}

extension View {
    func identityArea(_ colors: SummaryColors) -> some View { modifier(IdentityArea(colors: colors)) }
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
