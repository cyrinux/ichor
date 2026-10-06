import SwiftUI
import UniformTypeIdentifiers

/// Share, save to Files or delete a file kept on the phone.
struct LocalFileActions: View {
    let url: URL
    let contentType: UTType
    var wrapperOptions: FileWrapper.ReadingOptions = []
    let deleteTitle: Text
    let onDelete: () -> Void

    @State private var exporting = false
    @State private var confirmDelete = false
    @State private var message: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                ShareLink(item: url) { Label("Share", systemImage: "square.and.arrow.up") }
                Spacer()
                Button { exporting = true } label: { Label("Save to Files", systemImage: "folder") }
                Spacer()
                Button(role: .destructive) { confirmDelete = true } label: { Label("Delete", systemImage: "trash") }
            }
            .buttonStyle(.bordered)
            .labelStyle(.iconOnly)
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
        }
        .fileExporter(isPresented: $exporting, document: ExportFileDocument(url: url, options: wrapperOptions), contentType: contentType,
                      defaultFilename: url.lastPathComponent) { result in
            switch result {
            case .success: message = String(localized: "Saved.")
            case .failure(let error): message = error.localizedDescription
            }
        }
        .confirmationDialog(deleteTitle, isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) {
                do {
                    try LocalFileStore.delete(url)
                    onDelete()
                } catch {
                    message = error.localizedDescription
                }
            }
        }
    }
}
