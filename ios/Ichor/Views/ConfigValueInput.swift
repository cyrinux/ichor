import SwiftUI
import IchorCore

/// The input of one config value, by type: a switch, a choice among the schema's values, a
/// number or free text. Objects and lists are created empty.
struct ConfigValueInput: View {
    let type: String
    /// The values the schema allows, when it lists them.
    let options: [String]?
    @Binding var text: String

    var body: some View {
        switch type {
        case "boolean":
            Toggle("Enabled", isOn: Binding(get: { text == "true" }, set: { text = $0 ? "true" : "false" }))
        case "object", "array":
            Text("Created empty: add its content afterwards.").note()
        case "null":
            Text("The field is set to null.").note()
        default:
            if let options, !options.isEmpty {
                Picker("Value", selection: $text) {
                    // A value outside the schema's list stays selectable until it is changed.
                    if !options.contains(text) { Text(verbatim: text).tag(text) }
                    ForEach(options, id: \.self) { option in
                        Text(verbatim: option).tag(option)
                    }
                }
            } else {
                TextField("Value", text: $text, axis: .vertical)
                    .font(.body.monospaced())
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .keyboardType(isNumber ? .numbersAndPunctuation : .default)
            }
        }
    }

    private var isNumber: Bool { type == "integer" || type == "number" }
}

/// Picks the type of a new value when the schema leaves the choice.
struct ConfigTypePicker: View {
    let types: [String]
    @Binding var selection: String

    var body: some View {
        if types.count > 1 {
            Picker("Type", selection: $selection) {
                ForEach(types, id: \.self) { type in
                    Text(verbatim: configTypeLabel(type)).tag(type)
                }
            }
        }
    }
}

func configTypeLabel(_ type: String) -> String {
    switch type {
    case "string": String(localized: "Text")
    case "integer": String(localized: "Integer")
    case "number": String(localized: "Number")
    case "boolean": String(localized: "On / off")
    case "object": String(localized: "Object")
    case "array": String(localized: "List")
    case "null": String(localized: "Null")
    default: type
    }
}

/// What the input of a type starts with.
func configInitialValue(type: String, options: [String]?) -> String {
    if type == "boolean" { return "false" }
    if type == "string" || type == "integer" || type == "number" { return options?.first ?? "" }
    return ""
}

/// The text sent for a value: containers and null carry none, a switch is true or false.
func configSubmittedValue(type: String, text: String) -> String {
    switch type {
    case "boolean": text == "true" ? "true" : "false"
    case "integer", "number": text.trimmingCharacters(in: .whitespaces)
    case "string": text
    default: ""
    }
}

/// A number needs digits; everything else can be sent as it is.
func configValueIsComplete(type: String, text: String) -> Bool {
    guard type == "integer" || type == "number" else { return true }
    return !text.trimmingCharacters(in: .whitespaces).isEmpty
}
