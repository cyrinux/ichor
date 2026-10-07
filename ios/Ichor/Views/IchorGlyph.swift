import SwiftUI

/// The Ichor glyph (docs/brand/ichor-glyph.svg), drawn in the current foreground style.
struct IchorGlyph: View {
    var body: some View {
        ZStack {
            IchorGlyphShape(part: .rings).fill()
            IchorGlyphShape(part: .spike).fill()
            // The body's face is a hole: even-odd.
            IchorGlyphShape(part: .body).fill(style: FillStyle(eoFill: true))
            IchorGlyphShape(part: .drop).fill()
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }
}

/// One part of the glyph's 512-unit artwork, scaled to fit the rect. Parts overlap, so each is
/// filled on its own.
private struct IchorGlyphShape: Shape {
    enum Part { case rings, spike, body, drop }

    let part: Part

    func path(in rect: CGRect) -> Path {
        let scale = min(rect.width, rect.height) / 512
        let transform = CGAffineTransform(translationX: rect.midX - 256 * scale, y: rect.midY - 256 * scale)
            .scaledBy(x: scale, y: scale)
        return art.applying(transform)
    }

    private var art: Path {
        switch part {
        case .rings: Self.rings
        case .spike: Self.spike
        case .body: Self.body
        case .drop: Self.drop
        }
    }

    /// Four arcs of a circle of radius 212 around (256, 280), stroked 40 units wide; the SVG's
    /// sweep flag as `clockwise` (on screen).
    private static let rings: Path = {
        let center = CGPoint(x: 256, y: 280)
        let arcs: [(from: CGPoint, to: CGPoint, clockwise: Bool)] = [
            (CGPoint(x: 300.1, y: 72.6), CGPoint(x: 463.4, y: 235.9), true),
            (CGPoint(x: 467.9, y: 272.6), CGPoint(x: 355.5, y: 467.2), true),
            (CGPoint(x: 211.9, y: 72.6), CGPoint(x: 48.6, y: 235.9), false),
            (CGPoint(x: 44.1, y: 272.6), CGPoint(x: 156.5, y: 467.2), false),
        ]
        var path = Path()
        for arc in arcs {
            let start = atan2(arc.from.y - center.y, arc.from.x - center.x)
            let end = atan2(arc.to.y - center.y, arc.to.x - center.x)
            var delta = end - start
            if arc.clockwise, delta < 0 { delta += 2 * .pi }
            if !arc.clockwise, delta > 0 { delta -= 2 * .pi }
            var line = Path()
            // y points down: a positive delta turns clockwise on screen.
            line.addRelativeArc(center: center, radius: 212, startAngle: .radians(start), delta: .radians(delta))
            path.addPath(line.strokedPath(StrokeStyle(lineWidth: 40)))
        }
        return path
    }()

    private static let spike: Path = {
        var path = Path()
        path.addLines(IchorGlyphShape.points([(256, 8), (228, 24), (240, 116), (256, 132), (272, 116), (284, 24)]))
        path.closeSubpath()
        return path
    }()

    private static let body: Path = {
        var path = Path()
        path.move(to: CGPoint(x: 256, y: 100))
        path.addCurve(to: CGPoint(x: 152, y: 204), control1: CGPoint(x: 190, y: 100), control2: CGPoint(x: 152, y: 144))
        for point in IchorGlyphShape.points([(152, 300), (192, 350), (320, 350), (360, 300), (360, 204)]) {
            path.addLine(to: point)
        }
        path.addCurve(to: CGPoint(x: 256, y: 100), control1: CGPoint(x: 360, y: 144), control2: CGPoint(x: 322, y: 100))
        path.closeSubpath()
        path.addLines(IchorGlyphShape.points([
            (162, 184), (256, 210), (350, 184), (344, 232), (302, 250), (280, 250),
            (280, 350), (232, 350), (232, 250), (210, 250), (168, 232),
        ]))
        path.closeSubpath()
        return path
    }()

    private static let drop: Path = {
        var path = Path()
        path.move(to: CGPoint(x: 256, y: 380))
        path.addCurve(to: CGPoint(x: 214, y: 456), control1: CGPoint(x: 256, y: 380), control2: CGPoint(x: 214, y: 430))
        // The round bottom: from the left point to the right one, through the bottom.
        path.addRelativeArc(center: CGPoint(x: 256, y: 456), radius: 42, startAngle: .degrees(180), delta: .degrees(-180))
        path.addCurve(to: CGPoint(x: 256, y: 380), control1: CGPoint(x: 298, y: 430), control2: CGPoint(x: 256, y: 380))
        path.closeSubpath()
        return path
    }()

    private static func points(_ xy: [(CGFloat, CGFloat)]) -> [CGPoint] {
        xy.map { CGPoint(x: $0.0, y: $0.1) }
    }
}
