import Foundation

/// The orientations a screen may take, without UIKit types (the app maps them to an
/// orientation mask). Mirrors Android's Orientation.kt.
public enum AllowedOrientations: Equatable, Sendable {
    case portrait
    case allButUpsideDown
    case all
}

/// The rotation policy: an iPhone holds portrait, except while a wide-content screen (logs,
/// shells, charts, YAML, diffs, maps) is on screen; an iPad always rotates.
public enum OrientationPolicy {
    /// What the app allows now: `landscapeHeld` when at least one wide screen is shown.
    public static func allowed(landscapeHeld: Bool, pad: Bool) -> AllowedOrientations {
        if pad { return .all }
        return landscapeHeld ? .allButUpsideDown : .portrait
    }

    /// A chart's height: `min` normally, and about 45 % of the `available` height when the
    /// height is compact (an iPhone in landscape), where the chart is what the phone was turned for.
    public static func chartHeight(min: Double, compactHeight: Bool, available: Double) -> Double {
        compactHeight ? Swift.max(min, (available * 0.45).rounded()) : min
    }
}
