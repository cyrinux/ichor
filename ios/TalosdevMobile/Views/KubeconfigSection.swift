import SwiftUI
import TalosdevMobileCore
import UniformTypeIdentifiers

/// Admin kubeconfig export (os:admin), written only to the file the user picks.
struct KubeconfigSection: View {
    @Environment(AppModel.self) private var model
    @State private var document: YAMLDocument?
    @State private var exporting = false
    @State private var busy = false
    @State private var message: String?

    var body: some View {
        Section {
            Button(busy ? "Exporting…" : "Export kubeconfig…") { Task { await export() } }
                .disabled(busy)
            Link("Get kubenav", destination: URL(string: "https://apps.apple.com/app/kubenav/id1494512160")!)
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Kubernetes")
        } footer: {
            Text("Anyone with this file has full access to the Kubernetes cluster: keep it private. In kubenav, add a cluster from the file.")
        }
        .fileExporter(isPresented: $exporting, document: document, contentType: .yaml,
                      defaultFilename: "kubeconfig-\(model.activeContext).yaml") { result in
            switch result {
            case .success: message = "Saved."
            case .failure(let error): message = error.localizedDescription
            }
            document = nil // don't keep the credential around
        }
    }

    private func export() async {
        guard let client = model.client else { return }
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: "Export kubeconfig") {
            message = failure
            return
        }
        busy = true
        defer { busy = false }
        do {
            document = YAMLDocument(text: try await client.kubeconfig())
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
