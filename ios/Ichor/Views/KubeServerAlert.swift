import SwiftUI
import IchorCore

extension View {
    /// The Kubernetes API address of `context` to use instead of the kubeconfig's, on this
    /// device (Android's KubeServerDialog): checked by the core, then saved; an open Kubernetes
    /// screen reloads its lists through it. Closed when screenshot mode turns on.
    func kubeServerAlert(for context: Binding<ContextSummary?>) -> some View {
        modifier(KubeServerAlert(context: context))
    }
}

private struct KubeServerAlert: ViewModifier {
    @Binding var context: ContextSummary?

    @Environment(AppModel.self) private var model
    @State private var input = ""
    @State private var error: String?

    func body(content: Content) -> some View {
        content
            .alert("Kubernetes API address", isPresented: $context.isPresent()) {
                TextField(text: $input, prompt: Text(verbatim: "k8s.example.com:6443")) { Text("Address") }
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                Button("OK") {
                    // The alert clears the binding as it closes: keep the cluster for the save.
                    let editing = context
                    let typed = input
                    Task { await save(typed, for: editing) }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Used instead of the address in the kubeconfig Talos issues, e.g. a port forward, a load balancer or a public name: host, host:port or an https URL. Without a port, the kubeconfig’s is used. The certificate is still checked against the cluster’s own address. Leave empty to use the kubeconfig’s again. Only on this device.")
            }
            .messageAlert($error)
            .onChange(of: context) { _, opened in
                input = opened.flatMap { model.kubeServers[$0.fingerprint] } ?? ""
            }
            .onChange(of: model.privacyMask) { _, masked in if masked { context = nil } }
    }

    private func save(_ typed: String, for context: ContextSummary?) async {
        guard let context else { return }
        do {
            model.setKubeServer(try await TalosClient.normalizeKubeServer(typed), for: context)
        } catch {
            self.error = error.localizedDescription
        }
    }
}
