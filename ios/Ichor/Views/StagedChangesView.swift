import SwiftUI
import IchorCore

/// The files of the cluster repository staged for one pull request: each with its lines added
/// and removed, opened again in the editor or removed; then Open pull request (ProposeSheet)
/// for all of them. Kept for the session only, which the footer says.
struct StagedChangesView: View {
    let source: GitSource

    @Environment(\.dismiss) private var dismiss
    @Environment(StagedEdits.self) private var staged
    /// Lines added and removed per staged draft, once counted.
    @State private var counts: [StagedFile: LineCounts] = [:]
    @State private var editing: StagedPath?
    @State private var proposing = false

    private var key: StagedKey { StagedKey(source: source) }
    private var files: [StagedFile] { staged.of(key) }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(files, id: \.path) { file in row(file) }
                        .onDelete { offsets in
                            let paths = offsets.map { files[$0].path }
                            paths.forEach { staged.remove(key, path: $0) }
                        }
                } footer: {
                    Text("Kept until the app is closed. Open one pull request for all of them.")
                }
                Section {
                    Button { proposing = true } label: {
                        Text("Open pull request").frame(maxWidth: .infinity)
                    }
                    .disabled(files.isEmpty)
                }
            }
            .themedBackground()
            .navigationTitle(Text("Staged changes"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
            }
            .task(id: files) { await count() }
            .sheet(item: $editing) { item in
                SourceEditView(source: source, path: item.path, app: .repository)
            }
            .sheet(isPresented: $proposing) {
                ProposeSheet(source: source, path: "", app: .repository,
                             base: SourceEditBase(file: GitFile(path: ""), ref: key.baseRef), content: "",
                             target: key.baseRef, files: files,
                             onReload: { _ in }, onDone: {
                                 proposing = false
                                 dismiss()
                             })
            }
        }
    }

    private func row(_ file: StagedFile) -> some View {
        HStack(spacing: 8) {
            Button { editing = StagedPath(path: file.path) } label: {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: file.path.split(separator: "/").last.map(String.init) ?? file.path)
                        .font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                    Text(verbatim: file.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                        .lineLimit(1).truncationMode(.middle)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityHint(Text("Edit"))
            if let c = counts[file] {
                HStack(spacing: 6) {
                    Text(verbatim: "+\(c.added)").foregroundStyle(.statusOK)
                    Text(verbatim: "−\(c.removed)").foregroundStyle(.statusBad)
                }
                .font(.caption.monospacedDigit().weight(.semibold))
            }
            Button { staged.remove(key, path: file.path) } label: {
                Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Remove from the pull request"))
        }
    }

    /// Counts each staged file's lines, the ones not counted yet.
    private func count() async {
        for file in files where counts[file] == nil {
            let diff = try? await TalosClient.textDiff(before: file.original, after: file.draft, name: file.path)
            if let lines = diff?.resources.first?.counts { counts[file] = LineCounts(added: lines.added, removed: lines.removed) }
        }
    }
}

/// A staged file's lines added and removed.
private struct LineCounts {
    let added: Int
    let removed: Int
}

/// A staged file opened again in the editor.
private struct StagedPath: Identifiable {
    let path: String
    var id: String { path }
}
