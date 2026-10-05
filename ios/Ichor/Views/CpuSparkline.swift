import SwiftUI
import IchorCore

/// Recent cluster CPU as a small filled line on a fixed 0–100 % scale, newest on the right; it
/// fills in from the right as samples arrive. Coloured like UsageBar. Same as Android.
struct CpuSparkline: View {
    let history: [Double]
    var warnAt = 0.75

    var body: some View {
        let current = history.last ?? 0
        let color: Color = current >= 0.9 ? .red : current >= warnAt ? .orange : .green
        Canvas { context, size in
            let w = size.width
            let h = size.height
            let stroke = 1.5
            // Keep the line inside the canvas at 0 % and 100 %.
            func y(_ v: Double) -> Double { stroke / 2 + (h - stroke) * (1 - min(max(v, 0), 1)) }

            var track = Path()
            track.move(to: CGPoint(x: 0, y: h - stroke / 2))
            track.addLine(to: CGPoint(x: w, y: h - stroke / 2))
            context.stroke(track, with: .color(Color.secondary.opacity(0.3)), lineWidth: stroke)
            guard history.count >= 2 else { return }
            let step = w / Double(clusterHistoryPoints - 1)
            let startX = w - step * Double(history.count - 1)
            var line = Path()
            for (i, v) in history.enumerated() {
                let point = CGPoint(x: startX + step * Double(i), y: y(v))
                if i == 0 { line.move(to: point) } else { line.addLine(to: point) }
            }
            var area = line
            area.addLine(to: CGPoint(x: w, y: h))
            area.addLine(to: CGPoint(x: startX, y: h))
            area.closeSubpath()
            context.fill(area, with: .linearGradient(
                Gradient(colors: [color.opacity(0.35), color.opacity(0.04)]),
                startPoint: .zero, endPoint: CGPoint(x: 0, y: h)
            ))
            context.stroke(line, with: .color(color), style: StrokeStyle(lineWidth: stroke, lineCap: .round, lineJoin: .round))
        }
        .frame(height: 20)
        .accessibilityElement()
        .accessibilityLabel(Text("Cluster CPU usage over the last minutes"))
    }
}
