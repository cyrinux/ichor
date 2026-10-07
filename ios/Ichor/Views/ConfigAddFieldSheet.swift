import SwiftUI
import IchorCore

/// Adds a field to an object of the draft (one the schema knows, or a free key where the
/// object takes any) or an item to a list.
struct ConfigAddFieldSheet: View {
    let doc: Int
    let parent: ConfigNode
    /// Applies one edit to the draft; returns why it was refused.
    let apply: (ConfigEdit) async -> String?

    @Environment(\.dismiss) private var dismiss
    /// Key of the schema field picked; nil with a custom key.
    @State private var selected: String?
    @State private var customKey = ""
    @State private var chosenType = ""
    @State private var value = ""
    @State private var busy = false
    @State private var failure: String?

    private var isList: Bool { parent.type == "array" }
    private var addable: [ConfigAddable] { parent.addable ?? [] }
    private var allowsCustomKey: Bool { !(parent.freeKeyType ?? "").isEmpty }
    private var candidate: ConfigAddable? { addable.first { $0.key == selected } }

    private var key: String {
        if isList { return "" }
        return candidate?.key ?? customKey.trimmingCharacters(in: .whitespaces)
    }

    private var types: [String] {
        let schemaType = isList ? (parent.itemType ?? "") : candidate?.type ?? parent.freeKeyType ?? ""
        let types = configValueTypes(for: schemaType.or("any"))
        return types.isEmpty ? ["string"] : types
    }

    private var type: String { types.contains(chosenType) ? chosenType : types.first ?? "string" }

    var body: some View {
        NavigationStack {
            Form {
                if !isList { fieldSection }
                Section("Value") {
                    ConfigTypePicker(types: types, selection: typeSelection)
                    ConfigValueInput(type: type, options: candidate?.enum, text: $value)
                }
                if let failure {
                    Section { Text(verbatim: failure).font(.footnote).foregroundStyle(.statusBad) }
                }
            }
            .themedBackground()
            .navigationTitle(Text(verbatim: parent.addLabel))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { Task { await submit() } }
                        .disabled(busy || (!isList && key.isEmpty) || !configValueIsComplete(type: type, text: value))
                }
            }
            .onAppear { resetValue() }
        }
    }

    private var fieldSection: some View {
        Section("Field") {
            ForEach(addable) { field in
                Button { select(field) } label: { AddableRow(field: field, selected: field.key == selected) }
                    .buttonStyle(.plain)
            }
            if allowsCustomKey {
                TextField("Custom key", text: customKeyBinding)
                    .font(.body.monospaced())
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
            }
        }
    }

    /// Typing a key gives up the schema field picked before.
    private var customKeyBinding: Binding<String> {
        Binding(get: { customKey }, set: { new in
            customKey = new
            if !new.isEmpty && selected != nil {
                selected = nil
                resetValue()
            }
        })
    }

    private var typeSelection: Binding<String> {
        Binding(get: { type }, set: { new in
            chosenType = new
            resetValue()
        })
    }

    private func select(_ field: ConfigAddable) {
        selected = field.key
        customKey = ""
        resetValue()
    }

    /// Another field or type starts from its own empty value.
    private func resetValue() {
        value = configInitialValue(type: type, options: candidate?.enum)
    }

    private func submit() async {
        busy = true
        failure = await apply(.add(doc: doc, path: parent.path, key: key, type: type,
                                   value: configSubmittedValue(type: type, text: value)))
        busy = false
        if failure == nil { dismiss() }
    }
}

private struct AddableRow: View {
    let field: ConfigAddable
    let selected: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                .foregroundStyle(selected ? Color.accentColor : Color.secondary)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: field.key).font(.callout.monospaced())
                if let description = field.description, !description.isEmpty {
                    Text(verbatim: description).font(.caption).foregroundStyle(.secondary).lineLimit(selected ? nil : 2)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}
