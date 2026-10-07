import SwiftUI
import IchorCore
import UniformTypeIdentifiers

/// Kubeconfig export, written only to the file the user picks: the admin one Talos issues
/// (os:admin), or the stored one of a cluster added from a kubeconfig.
struct KubeconfigSection: View {
    @Environment(AppModel.self) private var model
    @State private var document: YAMLDocument?
    @State private var exporting = false
    @State private var busy = false
    @State private var message: String?

    var body: some View {
        Section {
            Button(busy ? String(localized: "Exporting…") : String(localized: "Export kubeconfig…")) { Task { await export() } }
                .disabled(busy)
            if !model.activeIsKube {
                Link("Get kubenav", destination: URL(string: "https://apps.apple.com/app/kubenav/id1494512160")!)
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Kubernetes")
        } footer: {
            if model.activeIsKube {
                Text("The kubeconfig of this cluster, as it was imported. Anyone with this file has the access it grants: keep it private.")
            } else {
                Text("Anyone with this file has full access to the Kubernetes cluster: keep it private. In kubenav, add a cluster from the file.")
            }
        }
        .fileExporter(isPresented: $exporting, document: document, contentType: .yaml,
                      defaultFilename: "kubeconfig-\(model.activeContext).yaml") { result in
            switch result {
            case .success: message = String(localized: "Saved.")
            case .failure(let error): message = error.localizedDescription
            }
            document = nil // don't keep the credential around
        }
    }

    private func export() async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Export kubeconfig")) {
            message = failure
            return
        }
        busy = true
        defer { busy = false }
        do {
            // A cluster added from a kubeconfig: its own context, from the stored kubeconfig.
            if model.activeIsKube {
                document = YAMLDocument(text: try await TalosClient.exportKubeContext(stored: client.config, context: client.context))
            } else {
                document = YAMLDocument(text: try await client.kubeconfig())
            }
            exporting = true
        } catch {
            message = error.localizedDescription
        }
    }
}

struct YAMLDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.yaml] }
    var text: String

    init(text: String) { self.text = text }

    init(configuration: ReadConfiguration) throws {
        text = String(data: configuration.file.regularFileContents ?? Data(), encoding: .utf8) ?? ""
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: Data(text.utf8))
    }
}
