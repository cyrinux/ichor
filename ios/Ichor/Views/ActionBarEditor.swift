import SwiftUI
import IchorCore

/// How a toolbar action shows, in the bar and its editor.
protocol BarActionLook {
    var title: Text { get }
    var systemImage: String { get }
}

/// The toolbar section of a screen's editor (Android's actionBarItems): its icons, a line, then
/// the actions in its ⋯ menu. Drag an action across the line, or use its arrow, to move it
/// between the two. Every change is saved at once into `text`, the bar's `@AppStorage`.
struct ActionBarSection<Action: BarAction & BarActionLook>: View {
    @Binding var text: String

    private var bar: ActionBar<Action> { .parse(text) }

    var body: some View {
        Section {
            ForEach(bar.slots) { slot in
                switch slot {
                case .menuLine:
                    Label("In the ⋯ menu", systemImage: "ellipsis.circle")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .moveDisabled(true)
                case .action(let action):
                    BarRow(action: action, inMenu: bar.menu.contains(action)) { save(bar.toBar(action)) } toMenu: { save(bar.toMenu(action)) }
                }
            }
            .onMove { source, destination in save(bar.moving(fromOffsets: source, toOffset: destination)) }
            if !bar.isDefault {
                Button("Reset to default") { save(ActionBar()) }
            }
        } header: {
            Text("Top bar")
        } footer: {
            Text("Drag an action above the line to show it as an icon, or below to put it in the ⋯ menu.")
        }
    }

    private func save(_ bar: ActionBar<Action>) {
        withAnimation { text = bar.encoded }
    }
}

/// A screen's toolbar to arrange, alone in a sheet (the overview's sheet has its cards too).
struct ActionBarEditorSheet<Action: BarAction & BarActionLook>: View {
    @AppStorage private var text: String
    @Environment(\.dismiss) private var dismiss

    init(_ action: Action.Type) {
        _text = AppStorage(wrappedValue: "", Action.barStorageKey)
    }

    var body: some View {
        NavigationStack {
            List { ActionBarSection<Action>(text: $text) }
                // Always arranging: the handles are what this sheet is for.
                .environment(\.editMode, .constant(.active))
                .navigationTitle("Customize top bar")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                }
        }
    }
}

private struct BarRow<Action: BarActionLook>: View {
    let action: Action
    let inMenu: Bool
    let toBar: () -> Void
    let toMenu: () -> Void

    var body: some View {
        HStack {
            Label { action.title } icon: { Image(systemName: action.systemImage) }
            Spacer()
            if inMenu {
                Button(action: toBar) { Image(systemName: "arrow.up.circle") }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("Show as an icon"))
            } else {
                Button(action: toMenu) { Image(systemName: "arrow.down.circle") }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(Text("Move to the menu"))
            }
        }
    }
}
