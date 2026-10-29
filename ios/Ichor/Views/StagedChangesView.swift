import SwiftUI
import IchorCore

/// The files of the cluster repository staged for one pull request: each with its lines added
/// and removed, opened again in the editor or removed; then Open pull request (ProposeSheet)
/// for all of them. Kept for the session only, which the footer says. From the Updates screen,
/// the plan's title and body prefill the pull request and the rows left out are listed.
struct StagedChangesView: View {
    let source: GitSource
    /// The branch the request goes to, nil for the staging key's.
    let target: String?
    /// The pull request's title and description in place of the Go core's (an update plan's).
    let defaultTitle: String?
    let defaultBody: String?
    /// What was left out of the pull request and why.
    let notes: [String]
    /// The files open again in the editor from here (the repository browser's staging).
    let editable: Bool

    // Explicit: the private @State properties make the memberwise init private.
    init(
        source: GitSource, target: String? = nil, defaultTitle: String? = nil, defaultBody: String? = nil,
        notes: [String] = [], editable: Bool = true
    ) {
        self.source = source
        self.target = target
        self.defaultTitle = defaultTitle
        self.defaultBody = defaultBody
        self.notes = notes
        self.editable = editable
    }

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
                if !notes.isEmpty {
                    Section("Not in this pull request") {
                        ForEach(notes, id: \.self) { note in
                            Text(verbatim: note).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                Section {
                    Button {
                        proposing = true
                    } label: {
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
                ProposeSheet(
                    source: source, path: "", app: .repository,
                    base: SourceEditBase(file: GitFile(path: ""), ref: target ?? key.baseRef), content: "",
                    target: target ?? key.baseRef, defaultTitle: defaultTitle, defaultBody: defaultBody, files: files,
                    onReload: { _ in },
                    onDone: {
                        proposing = false
                        dismiss()
                    })
            }
        }
    }

    private func row(_ file: StagedFile) -> some View {
        HStack(spacing: 8) {
            if editable {
                Button {
                    editing = StagedPath(path: file.path)
                } label: {
                    fileLabel(file)
                }
                .buttonStyle(.plain)
                .accessibilityHint(Text("Edit"))
            } else {
                fileLabel(file)
            }
            if let c = counts[file] {
                HStack(spacing: 6) {
                    Text(verbatim: "+\(c.added)").foregroundStyle(.statusOK)
                    Text(verbatim: "−\(c.removed)").foregroundStyle(.statusBad)
                }
                .font(.caption.monospacedDigit().weight(.semibold))
            }
            Button {
                staged.remove(key, path: file.path)
            } label: {
                Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Remove from the pull request"))
        }
    }

    private func fileLabel(_ file: StagedFile) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: file.path.split(separator: "/").last.map(String.init) ?? file.path)
                .font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
            Text(verbatim: file.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                .lineLimit(1).truncationMode(.middle)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
    }

    /// Counts each staged file's lines, the ones not counted yet.
    private func count() async {
        for file in files where counts[file] == nil {
            let diff = try? await TalosClient.textDiff(before: file.original, after: file.draft, name: file.path)
            if let lines = diff?.resources.first?.counts {
                counts[file] = LineCounts(added: lines.added, removed: lines.removed)
            }
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
