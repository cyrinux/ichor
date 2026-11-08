import Foundation

// The container colors of a cluster's main color, for the home summary's identity area and
// its icon containers: Android's primaryContainer / secondaryContainer roles (ui/theme/
// SeedScheme.kt), same tones and saturation rules, so both apps tint the summary alike.

/// Which tones a palette is read at. True black keeps the dark tones, as on Android: only the
/// page behind goes black, the containers stay lifted off it.
public enum PaletteAppearance: Sendable {
    case light, dark, trueBlack

    var isDark: Bool { self != .light }
}

/// Two container colors and the color of what sits on each (0xRRGGBB).
public struct ClusterPalette: Equatable, Sendable {
    /// Background of the identity area (logo, name, headline).
    public let primaryContainer: Int
    /// Text and icons on `primaryContainer`.
    public let onPrimaryContainer: Int
    /// Background of a non-status icon container.
    public let secondaryContainer: Int
    /// The icon on `secondaryContainer`.
    public let onSecondaryContainer: Int

    /// Android's secondary palette: the seed's hue, quieter.
    static let secondarySaturation = 0.4
    static let lightContainerTone = 90.0, lightOnContainerTone = 10.0
    static let darkContainerTone = 30.0, darkOnContainerTone = 90.0

    public init(seed: Int, appearance: PaletteAppearance) {
        let (hue, rawSaturation, _) = hsl(seed)
        let saturation = seedSaturation(rawSaturation)
        let dark = appearance.isDark
        let container = dark ? Self.darkContainerTone : Self.lightContainerTone
        let onContainer = dark ? Self.darkOnContainerTone : Self.lightOnContainerTone
        let secondary = saturation * Self.secondarySaturation
        primaryContainer = tonal(hue: hue, saturation: saturation, tone: container)
        onPrimaryContainer = tonal(hue: hue, saturation: saturation, tone: onContainer)
        secondaryContainer = tonal(hue: hue, saturation: secondary, tone: container)
        onSecondaryContainer = tonal(hue: hue, saturation: secondary, tone: onContainer)
    }
}
