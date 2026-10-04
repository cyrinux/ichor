import SwiftUI

/// A rollout's progress: one segment per desired pod, green once a new pod is ready, pulsing
/// while a new one starts (red when the rollout stalled), the track for the rest. Above
/// maxSegments pods, segments get too thin: a continuous bar instead.
struct RolloutProgressBar: View {
    let desired: Int
    let newReady: Int
    let newStarting: Int
    let done: Bool
    let failed: Bool

    private static let maxSegments = 24
    @State private var pulse = false

    private var total: Int { max(desired, 1) }
    private var ready: Int { done ? total : min(max(newReady, 0), total) }
    private var starting: Int { done ? 0 : min(max(newStarting, 0), total - ready) }
    private var startingColor: Color { (failed ? Color.red : Color.orange).opacity(pulse ? 1 : 0.35) }

    var body: some View {
        Group {
            if total > Self.maxSegments {
                continuous
            } else {
                HStack(spacing: 4) {
                    ForEach(0..<total, id: \.self) { i in
                        Capsule().fill(color(at: i))
                    }
                }
            }
        }
        .frame(height: 10)
        .animation(.easeInOut(duration: 0.45), value: ready)
        .animation(.easeInOut(duration: 0.45), value: starting)
        .onAppear {
            withAnimation(.easeInOut(duration: 0.7).repeatForever(autoreverses: true)) { pulse = true }
        }
        .accessibilityElement()
        .accessibilityValue(Text(verbatim: "\(ready)/\(total)"))
    }

    private func color(at i: Int) -> Color {
        if i < ready { return .green }
        if i < ready + starting { return startingColor }
        return Color(.tertiarySystemFill)
    }

    private var continuous: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Color(.tertiarySystemFill))
                Capsule().fill(startingColor)
                    .frame(width: geo.size.width * CGFloat(ready + starting) / CGFloat(total))
                Capsule().fill(LinearGradient(colors: [.green.opacity(0.7), .green], startPoint: .leading, endPoint: .trailing))
                    .frame(width: geo.size.width * CGFloat(ready) / CGFloat(total))
            }
        }
    }
}
