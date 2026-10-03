import SwiftUI

/// An (i) next to a label: tapping it explains the feature or how to read a value.
struct InfoHint: View {
    let title: Text
    let text: Text
    @State private var open = false

    init(_ title: LocalizedStringKey, _ text: LocalizedStringKey) {
        self.init(title: Text(title), text: Text(text))
    }

    init(title: Text, text: Text) {
        self.title = title
        self.text = text
    }

    var body: some View {
        Button { open = true } label: {
            Image(systemName: "info.circle")
                .imageScale(.medium)
                .foregroundStyle(.secondary)
        }
        // Borderless, so a tap inside a list row or a section header only opens the hint.
        .buttonStyle(.borderless)
        .accessibilityLabel(Text("More information"))
        .alert(title, isPresented: $open) {
            Button("OK") {}
        } message: {
            text
        }
    }
}
