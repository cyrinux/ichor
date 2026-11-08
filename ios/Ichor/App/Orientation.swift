import IchorCore
import SwiftUI
import UIKit

/// The wide screens holding the iPhone's rotation unlocked. A counter, not a flag: a screen
/// pushed over another appears before the one below disappears, so portrait comes back with the
/// last one. The app delegate reads `mask`; the plist (portrait + landscape) is the ceiling.
@MainActor
final class OrientationLock {
    static let shared = OrientationLock()
    private(set) var landscapeAllowed = 0

    private var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    var mask: UIInterfaceOrientationMask {
        switch OrientationPolicy.allowed(landscapeHeld: landscapeAllowed > 0, pad: isPad) {
        case .portrait: .portrait
        case .allButUpsideDown: .allButUpsideDown
        case .all: .all
        }
    }

    func push() {
        landscapeAllowed += 1
        if landscapeAllowed == 1 { refresh() }
    }

    func pop() {
        guard landscapeAllowed > 0 else { return }
        landscapeAllowed -= 1
        guard landscapeAllowed == 0 else { return }
        refresh()
        // Back on a portrait-only screen while the phone is sideways: turn it back now.
        guard !isPad, let scene, scene.interfaceOrientation.isLandscape else { return }
        scene.requestGeometryUpdate(.iOS(interfaceOrientations: .portrait)) { _ in }
    }

    private var scene: UIWindowScene? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
    }

    /// Asks UIKit to read the delegate's mask again (and rotate to the device's orientation).
    private func refresh() {
        scene?.keyWindow?.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations()
    }
}

extension View {
    /// Lets this screen rotate to landscape on an iPhone while it is shown (logs, shells,
    /// charts, YAML, diffs, maps). Apply on a screen's root, never inside a sheet's content.
    func allowsLandscape() -> some View {
        onAppear { OrientationLock.shared.push() }
            .onDisappear { OrientationLock.shared.pop() }
    }

    /// A chart `min` points high, growing to about 45 % of the screen's height in landscape.
    func chartHeight(_ min: CGFloat) -> some View {
        modifier(ChartHeight(min: min))
    }
}

private struct ChartHeight: ViewModifier {
    let min: CGFloat
    @Environment(\.verticalSizeClass) private var verticalSizeClass

    func body(content: Content) -> some View {
        if verticalSizeClass == .compact {
            content.containerRelativeFrame(.vertical) { length, _ in
                CGFloat(OrientationPolicy.chartHeight(min: Double(min), compactHeight: true, available: Double(length)))
            }
        } else {
            content.frame(height: min)
        }
    }
}
