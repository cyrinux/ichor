import SwiftUI
import IchorCore

/// The reboot sheet's confirmation for other risky node actions: the button only works once
/// the hostname is typed and every acknowledgment is switched on.
struct HostnameConfirmationSheet: View {
    let title: String
    let message: String
    let hostname: String
    let actionTitle: String
    /// Risks the user confirms one by one (none by default).
    let acknowledgments: [String]
    let onConfirm: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""
    @State private var acknowledged: Set<String> = []

    // Explicit: the private @State makes the memberwise init private.
    init(title: String, message: String, hostname: String, actionTitle: String, acknowledgments: [String] = [],
         onConfirm: @escaping () -> Void) {
        self.title = title
        self.message = message
        self.hostname = hostname
        self.actionTitle = actionTitle
        self.acknowledgments = acknowledgments
        self.onConfirm = onConfirm
    }

    /// A blank hostname is never confirmed, least of all by an empty field.
    private var matches: Bool { typedConfirmationMatches(typed, token: hostname) }

    private var allAcknowledged: Bool { Set(acknowledgments).isSubset(of: acknowledged) }

    private func acknowledgment(_ risk: String) -> Binding<Bool> {
        Binding(
            get: { acknowledged.contains(risk) },
            set: { on in acknowledged = on ? acknowledged.union([risk]) : acknowledged.subtracting([risk]) }
        )
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(message).foregroundStyle(.statusWarn)
                }
                if !acknowledgments.isEmpty {
                    Section("I understand") {
                        ForEach(acknowledgments, id: \.self) { risk in
                            Toggle(isOn: acknowledgment(risk)) { Text(verbatim: risk).font(.callout) }
                                .tint(.orange)
                        }
                    }
                }
                Section("Type \(hostname) to confirm") {
                    TextField(hostname, text: $typed)
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                }
                Section {
                    Button(role: .destructive) { onConfirm() } label: { Text(actionTitle) }
                        .disabled(!matches || !allAcknowledged)
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
