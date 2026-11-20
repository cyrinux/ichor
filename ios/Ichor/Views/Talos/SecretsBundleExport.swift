import SwiftUI

/// Export of the cluster's secrets bundle: a warning, the app lock, then the system exporter with
/// secrets.yaml. The bundle lives in memory only until the exporter closes; no file is written by
/// the app. `fetch` reads it from a control plane.
struct SecretsBundleExport: ViewModifier {
    @Binding var isPresented: Bool
    let clusterName: String
    let fetch: @Sendable () async throws -> String

    @Environment(LockStore.self) private var lockStore
    @State private var document: YAMLDocument?
    @State private var exporting = false
    @State private var message: String?

    func body(content: Content) -> some View {
        content
            .alert(Text("Export the secrets bundle?"), isPresented: $isPresented) {
                Button("Choose where to save", role: .destructive) {
                    Task { await export() }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(SecretsBundleText.warning(clusterName))
            }
            .fileExporter(
                isPresented: $exporting, document: document, contentType: .yaml, defaultFilename: "secrets.yaml"
            ) { result in
                switch result {
                case .success:
                    message = String(localized: "secrets.yaml saved. Use it with talosctl gen config --with-secrets.")
                case .failure(let error):
                    message = SecretsBundleText.failed(error.localizedDescription)
                }
                document = nil
            }
            .onChange(of: exporting) { _, open in
                // Cancelled: don't keep the secrets around.
                if !open { document = nil }
            }
            .alert(message ?? "", isPresented: $message.isPresent()) {
                Button("OK") {}
            }
    }

    private func export() async {
        if lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(reason: SecretsBundleText.authReason(clusterName))
        {
            message = failure
            return
        }
        do {
            document = YAMLDocument(text: try await fetch())
            exporting = true
        } catch {
            message = SecretsBundleText.failed(error.localizedDescription)
        }
    }
}

/// The export's sentences with an argument, so the catalog keys stay the same everywhere.
enum SecretsBundleText {
    static func warning(_ cluster: String) -> String {
        String(
            localized:
                "secrets.yaml holds every CA, key and token of “\(cluster)”: whoever has it can take over the cluster, and only rebuilding the cluster revokes that. Save it only to an encrypted place (a password manager, a SOPS-encrypted repository), never to a shared folder."
        )
    }

    static func authReason(_ cluster: String) -> String {
        String(localized: "Export the secrets of \(cluster)")
    }

    static func failed(_ reason: String) -> String {
        String(localized: "The secrets bundle was not exported: \(reason)")
    }
}

extension View {
    /// Shows the secrets bundle export (warning first) while `isPresented` is true.
    func secretsBundleExport(
        isPresented: Binding<Bool>, clusterName: String, fetch: @escaping @Sendable () async throws -> String
    ) -> some View {
        modifier(SecretsBundleExport(isPresented: isPresented, clusterName: clusterName, fetch: fetch))
    }
}
