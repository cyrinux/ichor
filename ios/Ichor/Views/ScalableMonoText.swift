import SwiftUI

/// The screens whose dense monospaced text the user resizes; each keeps its own size.
enum MonoScreen: String {
    case logs, capture
}

/// What a `scalableMonoText` container hands its rows: the picked size and the screen-reader
/// actions (rows carry them because VoiceOver focuses rows, not the list).
struct MonoTextScale {
    var size: CGFloat
    var larger: () -> Void
    var smaller: () -> Void

    static let minSize: CGFloat = 10
    static let maxFactor: Double = 2
    static let step: Double = 0.15

    /// Never below `minSize` points (unless the base already is), at most `maxFactor` times the base.
    static func clamp(_ factor: Double, base: CGFloat) -> Double {
        min(max(factor, min(Double(minSize / base), 1)), maxFactor)
    }
}

private struct MonoTextScaleKey: EnvironmentKey {
    static let defaultValue: MonoTextScale? = nil
}

extension EnvironmentValues {
    var monoTextScale: MonoTextScale? {
        get { self[MonoTextScaleKey.self] }
        set { self[MonoTextScaleKey.self] = newValue }
    }
}

/// Dense monospaced text (log lines, packets, hex): the size picked on this screen, else the
/// caption size; both follow Dynamic Type. Adds "Larger text" / "Smaller text" to the row.
private struct MonoFont: ViewModifier {
    var weight: Font.Weight
    @Environment(\.monoTextScale) private var scale
    @ScaledMetric(relativeTo: .caption) private var base: CGFloat = 12

    @ViewBuilder
    func body(content: Content) -> some View {
        if let scale {
            content
                .font(.system(size: scale.size, weight: weight, design: .monospaced))
                .accessibilityAction(named: Text("Larger text"), scale.larger)
                .accessibilityAction(named: Text("Smaller text"), scale.smaller)
        } else {
            content.font(.system(size: base, weight: weight, design: .monospaced))
        }
    }
}

/// Lets the user resize the monospaced rows inside: pinch, or the rows' accessibility actions.
/// Remembered per screen.
private struct ScalableMonoText: ViewModifier {
    @AppStorage private var factor: Double
    @ScaledMetric(relativeTo: .caption) private var base: CGFloat = 12
    @GestureState private var pinch: CGFloat = 1

    init(screen: MonoScreen) {
        _factor = AppStorage(wrappedValue: 1, "monoTextScale.\(screen.rawValue)")
    }

    func body(content: Content) -> some View {
        let shown = MonoTextScale.clamp(factor * Double(pinch), base: base)
        content
            .environment(\.monoTextScale, MonoTextScale(
                size: base * CGFloat(shown),
                larger: { factor = MonoTextScale.clamp(factor + MonoTextScale.step, base: base) },
                smaller: { factor = MonoTextScale.clamp(factor - MonoTextScale.step, base: base) }
            ))
            .simultaneousGesture(
                MagnifyGesture()
                    .updating($pinch) { value, state, _ in state = value.magnification }
                    .onEnded { value in factor = MonoTextScale.clamp(factor * Double(value.magnification), base: base) }
            )
    }
}

extension View {
    /// The dense monospaced font of a resizable screen (see `scalableMonoText`).
    func monoFont(weight: Font.Weight = .regular) -> some View {
        modifier(MonoFont(weight: weight))
    }

    /// Makes the `monoFont` text inside resizable by pinching; remembered per screen.
    func scalableMonoText(_ screen: MonoScreen) -> some View {
        modifier(ScalableMonoText(screen: screen))
    }
}
