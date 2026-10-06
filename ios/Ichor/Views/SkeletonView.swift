import SwiftUI

/// Grey shapes of the content to come, pulsing, in place of a blank page with a spinner.
/// More rows than any screen is tall are laid out and the overflow is clipped.
struct SkeletonView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(AppModel.self) private var model: AppModel?
    @State private var dimmed = false

    private static let rows = 14
    private static let titleWidths: [CGFloat] = [0.45, 0.6, 0.35, 0.5]
    private static let textWidths: [CGFloat] = [0.9, 0.7, 0.95, 0.55, 0.8]

    var body: some View {
        GeometryReader { proxy in
            let width = max(proxy.size.width - 64, 0)
            VStack(alignment: .leading, spacing: 12) {
                ForEach(0..<Self.rows, id: \.self) { index in
                    VStack(alignment: .leading, spacing: 10) {
                        bar(width * Self.titleWidths[index % Self.titleWidths.count], height: 14)
                        bar(width * Self.textWidths[index % Self.textWidths.count], height: 10)
                        bar(width * Self.textWidths[(index + 2) % Self.textWidths.count] * 0.7, height: 10)
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 12))
                }
            }
            .padding(16)
            .opacity(dimmed ? 0.45 : 1)
            // Scoped to the opacity: an animated transaction would also slide the layout in.
            .animation(reduceMotion ? nil : .easeInOut(duration: 0.9).repeatForever(autoreverses: true), value: dimmed)
        }
        .clipped()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        // The "true black" theme keeps its black page under the shapes.
        .background(model?.theme == .black ? Color.black : Color(.systemGroupedBackground))
        .onAppear { dimmed = !reduceMotion }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading…")
    }

    private func bar(_ width: CGFloat, height: CGFloat) -> some View {
        Capsule().fill(Color(.systemFill)).frame(width: width, height: height)
    }
}
