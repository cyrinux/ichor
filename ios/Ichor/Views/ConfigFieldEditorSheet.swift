import SwiftUI
import IchorCore

/// Changes or removes one value of the draft. Nothing is sent to the node.
struct ConfigFieldEditorSheet: View {
    let doc: Int
    let node: ConfigNode
    /// Applies one edit to the draft; returns why it was refused.
    let apply: (ConfigEdit) async -> String?

    @Environment(\.dismiss) private var dismiss
    @State private var type: String
    @State private var value: String
    @State private var busy = false
    @State private var failure: String?
    @State private var confirmingRemove = false

    init(doc: Int, node: ConfigNode, apply: @escaping (ConfigEdit) async -> String?) {
        self.doc = doc
        self.node = node
        self.apply = apply
        let types = Self.types(for: node)
        let type = types.first ?? node.type
        _type = State(initialValue: type)
        _value = State(initialValue: node.value ?? configInitialValue(type: type, options: node.enum))
    }

    /// A null says nothing about what it should hold: any scalar can replace it.
    private static func types(for node: ConfigNode) -> [String] {
        guard node.type == "null" else { return [node.type] }
        return configValueTypes(for: "any").filter { !["object", "array", "null"].contains($0) }
    }

    var body: some View {
        NavigationStack {
            Form {
                if let description = node.description, !description.isEmpty {
                    Section { Text(verbatim: description).font(.callout).foregroundStyle(.secondary) }
                }
                Section("Value") {
                    ConfigTypePicker(types: Self.types(for: node), selection: typeSelection)
                    ConfigValueInput(type: type, options: node.enum, text: $value)
                }
                if let failure {
                    Section { Text(verbatim: failure).font(.footnote).foregroundStyle(.statusBad) }
                }
                Section {
                    Button("Remove field", role: .destructive) { confirmingRemove = true }
                        .disabled(busy)
                }
            }
            .themedBackground()
            .navigationTitle(Text(verbatim: node.key))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await submit(.set(doc: doc, path: node.path, type: type, value: sentValue)) } }
                        .disabled(busy || !configValueIsComplete(type: type, text: value))
                }
            }
            .confirmationDialog(Text("Remove \(node.key)?"), isPresented: $confirmingRemove, titleVisibility: .visible) {
                Button("Remove", role: .destructive) { Task { await submit(.remove(doc: doc, path: node.path)) } }
                Button("Cancel", role: .cancel) {}
            }
        }
    }

    private var sentValue: String { configSubmittedValue(type: type, text: value) }

    /// Another type starts from its own empty value.
    private var typeSelection: Binding<String> {
        Binding(get: { type }, set: { new in
            type = new
            value = configInitialValue(type: new, options: node.enum)
        })
    }

    private func submit(_ edit: ConfigEdit) async {
        busy = true
        failure = await apply(edit)
        busy = false
        if failure == nil { dismiss() }
    }
}
