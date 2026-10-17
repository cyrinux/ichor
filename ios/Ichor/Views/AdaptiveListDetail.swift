import SwiftUI

/// The list and the picked item's screen side by side on an iPad at full or wide split width;
/// the list alone everywhere else, its rows pushing as before. The screen stays one page of the
/// app's NavigationStack (a NavigationSplitView nested in that stack breaks back navigation):
/// one top bar, the list's title over both columns, back returns to where the list came from.
enum ListDetailLayout {
    /// Width of the list column when the screen is split.
    static let listWidth: CGFloat = 360

    /// Side by side only on an iPad's regular width: a large iPhone in landscape and an iPad at
    /// a third of the screen keep the stack.
    static func isSplit(_ sizeClass: UserInterfaceSizeClass?) -> Bool {
        sizeClass == .regular && UIDevice.current.userInterfaceIdiom == .pad
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
}

/// `list` alone, or `list` beside `detail` of the selected item (a placeholder before any pick).
struct AdaptiveListDetail<Item: Hashable, ListContent: View, Detail: View>: View {
    @Binding var selection: Item?
    let placeholder: LocalizedStringKey
    let systemImage: String
    let list: ListContent
    let detail: (Item) -> Detail

    @Environment(\.horizontalSizeClass) private var sizeClass

    init(selection: Binding<Item?>, placeholder: LocalizedStringKey, systemImage: String,
         @ViewBuilder list: () -> ListContent, @ViewBuilder detail: @escaping (Item) -> Detail) {
        _selection = selection
        self.placeholder = placeholder
        self.systemImage = systemImage
        self.list = list()
        self.detail = detail
    }

    var body: some View {
        if ListDetailLayout.isSplit(sizeClass) {
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
