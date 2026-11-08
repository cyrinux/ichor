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
            Button {
                onJump(section, true)
            } label: {
                dot(filled: section.id == current)
            }
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
                    target.id != scrubbed
                else { return }
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

/// A list with a section rail over its trailing edge: the list's section headers mark
/// themselves with `.sectionTarget(id)` (an id of `sections`), wherever they are declared; the
/// rail fills the section in view and scrolls to the one picked. `showsRail` false hides it.
struct SectionRailScroll<Content: View>: View {
    let sections: [JumpSection]
    var showsRail = true
    @ViewBuilder let content: () -> Content

    @State private var tops: [String: Double] = [:]
    @State private var listTop: Double = 0
    @State private var current: String?

    var body: some View {
        ScrollViewReader { proxy in
            content()
                .environment(\.sectionTops, $tops)
                .onGeometryChange(for: Double.self) {
                    Double($0.frame(in: .global).minY + $0.safeAreaInsets.top)
                } action: {
                    listTop = $0
                }
                .onChange(of: tops) { updateCurrent() }
                .onChange(of: listTop) { updateCurrent() }
                .onChange(of: sections) { updateCurrent() }
                .overlay(alignment: .trailing) {
                    if showsRail {
                        SectionJumpRail(sections: sections, current: current) { section, animated in
                            withAnimation(animated ? .default : nil) { proxy.scrollTo(section.id, anchor: .top) }
                        }
                    }
                }
        }
    }

    private func updateCurrent() {
        current = SectionJump.current(sections: sections, visibleTops: tops, top: listTop, previous: current)
    }
}

private struct SectionTopsKey: EnvironmentKey {
    static var defaultValue: Binding<[String: Double]>? { nil }
}

extension EnvironmentValues {
    /// The on-screen tops of the section headers in view, by section id, set by
    /// `SectionRailScroll`; nil outside one.
    var sectionTops: Binding<[String: Double]>? {
        get { self[SectionTopsKey.self] }
        set { self[SectionTopsKey.self] = newValue }
    }
}

/// Makes a header the scroll target of section `id` and reports its on-screen top while it is
/// in view (dropped once it scrolls away): what `SectionJump.current` reads.
private struct SectionTarget: ViewModifier {
    let id: String
    @Environment(\.sectionTops) private var tops

    func body(content: Content) -> some View {
        content
            .id(id)
            .onGeometryChange(for: Double.self) {
                Double($0.frame(in: .global).minY)
            } action: {
                tops?.wrappedValue[id] = $0
            }
            .onDisappear { tops?.wrappedValue[id] = nil }
    }
}

extension View {
    /// The header (or, in a section without one, the first row) of section `id` of the
    /// enclosing `SectionRailScroll`.
    func sectionTarget(_ id: String) -> some View {
        modifier(SectionTarget(id: id))
    }
}
