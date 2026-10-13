import SwiftUI
import IchorCore

/// A rail of dots at the trailing edge of a long list, one per section: the filled dot is
/// `current`, a tap jumps to that section (animated), a drag along the rail shows the labels
/// and scrubs through the sections with a selection tick on each change. Nothing is drawn with
/// fewer than two sections. Put it in an `.overlay(alignment: .trailing)` of the list.
struct SectionJumpRail: View {
    let sections: [JumpSection]
    let current: String?
    let onJump: (JumpSection, _ animated: Bool) -> Void

    @State private var labelsShown = false
    @State private var scrubbed: String?
    @State private var ticks = 0
    @State private var hideLabels: Task<Void, Never>?

    /// Height of one dot's row: the drag maps the finger to a section by this slice.
    private let rowHeight: CGFloat = 28

    var body: some View {
        if sections.count >= 2 {
            VStack(alignment: .trailing, spacing: 0) {
                ForEach(sections) { row($0) }
            }
            .padding(.trailing, 2)
            .contentShape(Rectangle())
            .simultaneousGesture(scrub)
            .sensoryFeedback(.selection, trigger: ticks)
            .accessibilityElement(children: .contain)
        }
    }

    private func row(_ section: JumpSection) -> some View {
        HStack(spacing: 4) {
            if labelsShown {
                Text(verbatim: section.label)
                    .font(.caption2)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(.regularMaterial, in: Capsule())
                    .transition(.opacity)
                    .accessibilityHidden(true)
            }
            Button { onJump(section, true) } label: { dot(filled: section.id == current) }
                .buttonStyle(.plain)
                .accessibilityLabel(Text(verbatim: CapacityText.sectionRailJumpTo(section.label)))
                .accessibilityAddTraits(section.id == current ? .isSelected : [])
        }
        .frame(height: rowHeight)
    }

    private func dot(filled: Bool) -> some View {
        Circle()
            .fill(filled ? Color.accentColor : Color.clear)
            .overlay(Circle().strokeBorder(filled ? Color.clear : Color.secondary, lineWidth: 1))
            .frame(width: 8, height: 8)
            .frame(width: 24, height: rowHeight)
            .contentShape(Rectangle())
    }

    private var scrub: some Gesture {
        DragGesture(minimumDistance: 4)
            .onChanged { value in
                hideLabels?.cancel()
                if !labelsShown { withAnimation(.easeOut(duration: 0.15)) { labelsShown = true } }
                let height = rowHeight * CGFloat(sections.count)
                guard let target = SectionJump.section(at: Double(value.location.y / height), in: sections),
                      target.id != scrubbed else { return }
                scrubbed = target.id
                ticks += 1
                onJump(target, false)
            }
            .onEnded { _ in
                scrubbed = nil
                hideLabels = Task {
                    try? await Task.sleep(for: .milliseconds(600))
                    guard !Task.isCancelled else { return }
                    withAnimation(.easeOut(duration: 0.2)) { labelsShown = false }
                }
            }
    }
}

extension View {
    /// Reports this header's on-screen top into `tops` (by section `id`) while it is in view,
    /// and drops it once it scrolls away: what `SectionJump.current` reads.
    func trackSection(_ id: String, tops: Binding<[String: Double]>) -> some View {
        onGeometryChange(for: Double.self) { Double($0.frame(in: .global).minY) } action: { tops.wrappedValue[id] = $0 }
            .onDisappear { tops.wrappedValue[id] = nil }
    }
}
