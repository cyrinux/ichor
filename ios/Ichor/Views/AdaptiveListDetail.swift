import IchorCore
import SwiftUI

/// The list and the picked item's screen side by side on an iPad at full or wide split width;
/// the list alone everywhere else (a narrow Split View, a form sheet), its rows pushing as before. The screen stays one page of the
/// app's NavigationStack (a NavigationSplitView nested in that stack breaks back navigation):
/// one top bar, the list's title over both columns, back returns to where the list came from.
enum ListDetailLayout {
    /// Width of the list column when the screen is split.
    static let listWidth: CGFloat = 360
    /// The narrowest screen split in two: the list column plus a detail column of usable width.
    static let minSplitWidth: CGFloat = 700
    /// The narrowest overview in two columns (OverviewColumns, tested in IchorCore).
    static let twoColumnMinWidth = CGFloat(OverviewColumns.twoColumnMinWidth)

    /// Side by side only on an iPad's regular width: a large iPhone in landscape and an iPad at
    /// a third of the screen keep the stack.
    @MainActor
    static func isSplit(_ sizeClass: UserInterfaceSizeClass?) -> Bool {
        sizeClass == .regular && UIDevice.current.userInterfaceIdiom == .pad
    }

    /// `isSplit` with the width the screen really has: a form sheet or a half-screen Split View
    /// is regular yet too narrow for two columns.
    @MainActor
    static func isSplit(_ sizeClass: UserInterfaceSizeClass?, width: CGFloat) -> Bool {
        isSplit(sizeClass) && width >= minSplitWidth
    }
}

/// What a split list's rows pick (`ListDetailLink`): nil when the list is alone and rows push.
struct ListDetailPicker {
    let selected: AnyHashable?
    let pick: (AnyHashable) -> Void
}

private struct ListDetailPickerKey: EnvironmentKey {
    static let defaultValue: ListDetailPicker? = nil
}

extension EnvironmentValues {
    var listDetailPicker: ListDetailPicker? {
        get { self[ListDetailPickerKey.self] }
        set { self[ListDetailPickerKey.self] = newValue }
    }

    /// The width the screen really has, when it measured it (the overview); nil elsewhere.
    var availableWidth: CGFloat? {
        get { self[AvailableWidthKey.self] }
        set { self[AvailableWidthKey.self] = newValue }
    }
}

private struct AvailableWidthKey: EnvironmentKey {
    static let defaultValue: CGFloat? = nil
}

/// `list` alone, or `list` beside `detail` of the selected item (a placeholder before any pick).
/// `split` follows which of the two is on screen, for the screen's polling and toolbar.
struct AdaptiveListDetail<Item: Hashable, ListContent: View, Detail: View>: View {
    @Binding var selection: Item?
    @Binding var split: Bool
    let placeholder: LocalizedStringKey
    let systemImage: String
    let list: ListContent
    let detail: (Item) -> Detail

    @Environment(\.horizontalSizeClass) private var sizeClass

    init(selection: Binding<Item?>, split: Binding<Bool> = .constant(false), placeholder: LocalizedStringKey,
         systemImage: String, @ViewBuilder list: () -> ListContent, @ViewBuilder detail: @escaping (Item) -> Detail) {
        _selection = selection
        _split = split
        self.placeholder = placeholder
        self.systemImage = systemImage
        self.list = list()
        self.detail = detail
    }

    var body: some View {
        GeometryReader { geometry in
            let isSplit = ListDetailLayout.isSplit(sizeClass, width: geometry.size.width)
            content(isSplit: isSplit)
                .onChange(of: isSplit, initial: true) { _, now in split = now }
                // Alone with an item picked (a link opened on an iPad too narrow to split, or a
                // split screen narrowed): the item pushed over the list. Back clears the pick.
                // On an iPhone nothing picks, so this never shows.
                .navigationDestination(isPresented: pushed(isSplit: isSplit)) {
                    if let selection { detail(selection).id(selection) }
                }
        }
    }

    /// Whether the picked item is pushed: only while not split. Widened back to a split, the
    /// push goes and the item shows beside the list, still picked.
    private func pushed(isSplit: Bool) -> Binding<Bool> {
        Binding(
            get: { !isSplit && selection != nil },
            set: { shown in if !shown, !isSplit { selection = nil } }
        )
    }

    /// The rows get the picker only when split: alone, they push.
    @ViewBuilder
    private func content(isSplit: Bool) -> some View {
        if isSplit {
            HStack(spacing: 0) {
                list
                    .frame(width: ListDetailLayout.listWidth)
                    .environment(\.listDetailPicker, picker)
                Divider().ignoresSafeArea(edges: .bottom)
                detailColumn.frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        } else {
            list
        }
    }

    @ViewBuilder
    private var detailColumn: some View {
        if let selection {
            // A fresh screen for each pick, not the previous item's state.
            detail(selection).id(selection)
        } else {
            ContentUnavailableView(placeholder, systemImage: systemImage).themedBackground()
        }
    }

    private var picker: ListDetailPicker {
        ListDetailPicker(selected: selection.map { AnyHashable($0) }) { picked in
            if let item = picked.base as? Item { selection = item }
        }
    }
}

/// A list row: a NavigationLink to `value` when the list is alone; in a split list
/// (`AdaptiveListDetail`), a button that shows `pick` (`value` when nil) beside the list,
/// highlighted while shown.
struct ListDetailLink<Value: Hashable, Label: View>: View {
    let value: Value
    var pick: AnyHashable?
    let label: Label

    @Environment(\.listDetailPicker) private var picker
    @Environment(\.editMode) private var editMode

    init(value: Value, pick: AnyHashable? = nil, @ViewBuilder label: () -> Label) {
        self.value = value
        self.pick = pick
        self.label = label()
    }

    var body: some View {
        if editMode?.wrappedValue.isEditing == true, picker != nil {
            // Selection mode: a tap ticks the row (the list's selection), it opens nothing.
            label
        } else if let picker {
            let key = pick ?? AnyHashable(value)
            let shown = picker.selected == key
            Button { picker.pick(key) } label: {
                label.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .listRowBackground(shown ? Color.accentColor.opacity(0.15) : nil)
            .accessibilityAddTraits(shown ? .isSelected : [])
        } else {
            NavigationLink(value: value) { label }
        }
    }
}
