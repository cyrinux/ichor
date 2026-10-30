import SwiftUI

/// The content under a tab picker, swiped sideways from one tab to the next: the twin of
/// Android's SwipeTabPager. `selection` stays the screen's own tab state, and its picker stays
/// where the screen has it: picking a tab slides the pager there (straight to it when it is
/// not a neighbour, so the tabs in between do not start), and a swipe that settles on a page
/// selects its tab. Pages are lazy: one is built when it scrolls in, and its tasks (streams)
/// end when it scrolls out. A row's swipe actions and the back-swipe from the edge keep
/// priority (UIKit's own arbitration); on the tabs in `fixed`, whose content scrubs sideways
/// (a chart), the page does not swipe.
struct SwipeTabPager<Tab: Hashable, Page: View>: View {
    let tabs: [Tab]
    @Binding var selection: Tab
    let fixed: Set<Tab>
    let page: (Tab) -> Page

    /// The page the scroll view shows; follows `selection` both ways.
    @State private var position: Tab?

    init(tabs: [Tab], selection: Binding<Tab>, fixed: Set<Tab> = [], @ViewBuilder page: @escaping (Tab) -> Page) {
        self.tabs = tabs
        _selection = selection
        self.fixed = fixed
        self.page = page
        _position = State(initialValue: selection.wrappedValue)
    }

    var body: some View {
        ScrollView(.horizontal) {
            LazyHStack(spacing: 0) {
                ForEach(tabs, id: \.self) { tab in
                    page(tab)
                        // scrollDisabled is inherited: a fixed tab's own lists still scroll.
                        .scrollDisabled(false)
                        .environment(\.swipeTabIsCurrent, tab == selection)
                        .containerRelativeFrame([.horizontal, .vertical])
                        .id(tab)
                }
            }
            .scrollTargetLayout()
        }
        .scrollTargetBehavior(.paging)
        .scrollIndicators(.hidden)
        .scrollPosition(id: $position)
        .scrollDisabled(fixed.contains(selection))
        .onAppear { position = selection }
        .onChange(of: selection) { old, new in
            guard position != new else { return }
            if swipeDistance(from: old, to: new) > 1 {
                position = new
            } else {
                withAnimation(.snappy) { position = new }
            }
        }
        .onChange(of: position) { _, new in
            if let new, new != selection, tabs.contains(new) { selection = new }
        }
    }

    private func swipeDistance(from old: Tab, to new: Tab) -> Int {
        guard let from = tabs.firstIndex(of: old), let to = tabs.firstIndex(of: new) else { return Int.max }
        return abs(to - from)
    }
}

private struct SwipeTabIsCurrentKey: EnvironmentKey {
    static let defaultValue = true
}

extension EnvironmentValues {
    /// Whether the page is the tab on screen in its SwipeTabPager (true outside one): a page
    /// kept by the pager off screen must not put its search field in the bar.
    var swipeTabIsCurrent: Bool {
        get { self[SwipeTabIsCurrentKey.self] }
        set { self[SwipeTabIsCurrentKey.self] = newValue }
    }
}

/// `.searchable` for a view that can be a page of a SwipeTabPager: only the tab on screen
/// adds its search field to the navigation bar.
private struct CurrentTabSearch: ViewModifier {
    @Environment(\.swipeTabIsCurrent) private var current
    @Binding var text: String
    let prompt: Text

    @ViewBuilder
    func body(content: Content) -> some View {
        if current {
            content.searchable(text: $text, prompt: prompt)
        } else {
            content
        }
    }
}

extension View {
    func searchableOnCurrentTab(text: Binding<String>, prompt: Text) -> some View {
        modifier(CurrentTabSearch(text: text, prompt: prompt))
    }
}
