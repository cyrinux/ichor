import SwiftUI

/// The reboot sheet's confirmation for other risky node actions: the button only works once
/// the hostname is typed.
struct HostnameConfirmationSheet: View {
    let title: String
    let message: String
    let hostname: String
    let actionTitle: String
    let onConfirm: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""

    // Explicit: the private @State makes the memberwise init private.
    init(title: String, message: String, hostname: String, actionTitle: String, onConfirm: @escaping () -> Void) {
        self.title = title
        self.message = message
        self.hostname = hostname
        self.actionTitle = actionTitle
        self.onConfirm = onConfirm
    }

    private var matches: Bool { typed.trimmingCharacters(in: .whitespaces) == hostname }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(message).foregroundStyle(.orange)
                }
                Section("Type \(hostname) to confirm") {
                    TextField(hostname, text: $typed)
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                }
                Section {
                    Button(role: .destructive) { onConfirm() } label: { Text(actionTitle) }
                        .disabled(!matches)
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
    }
}
