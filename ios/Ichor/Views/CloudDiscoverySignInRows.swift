import SwiftUI

/// The browser wait and Web OAuth client's fallback code, shared by discovery's two sign-ins.
struct CloudDiscoverySignInRows: View {
    let flow: KubeSignInFlow
    let onCancel: () -> Void
    @State private var pastedCode = ""

    var body: some View {
        Group {
            if flow.isRunning {
                HStack(spacing: 12) {
                    ProgressView()
                    Text("Waiting for the sign-in in the browser…")
                }
                if flow.phase == .browser && flow.acceptsPastedCode {
                    TextField("Sign-in code", text: $pastedCode)
                        .font(.body.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Text("Browser did not come back to Ichor? Paste the sign-in code the page shows.")
                        .font(.footnote).foregroundStyle(.secondary)
                    Button("Continue") { flow.complete(code: pastedCode); pastedCode = "" }
                        .disabled(pastedCode.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
                Button("Cancel", role: .cancel) { pastedCode = ""; onCancel() }
            }
        }
        .onChange(of: flow.phase) { _, phase in
            if phase == .working || phase == .idle { pastedCode = "" }
        }
    }
}
