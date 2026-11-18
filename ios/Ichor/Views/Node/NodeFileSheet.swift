import IchorCore
import SwiftUI
import UniformTypeIdentifiers

/// One file of the node: its text in pinch-to-resize monospace, with Copy and Share.
struct NodeFileSheet: View {
    let node: String
    let path: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, path: String) {
        self.node = node
        self.path = path
    }

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<NodeFileText> = .loading

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: load) { file in
                content(file)
            }
            .navigationTitle(Text(verbatim: path.split(separator: "/").last.map(String.init) ?? path))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
                if case .loaded(let file, _, _) = state, !file.binary, !file.text.isEmpty {
                    ToolbarItem(placement: .primaryAction) {
                        Menu {
                            Button {
                                UIPasteboard.general.setItems(
                                    [[UTType.utf8PlainText.identifier: file.text]], options: [.localOnly: true])
                            } label: {
                                Label("Copy", systemImage: "doc.on.doc")
                            }
                            ShareLink(item: file.text) { Label("Share", systemImage: "square.and.arrow.up") }
                        } label: {
                            Image(systemName: "ellipsis.circle").accessibilityLabel(Text("More actions"))
                        }
                    }
                }
            }
        }
        .task { await load() }
    }

    @ViewBuilder
    private func content(_ file: NodeFileText) -> some View {
        let shown = file.text.hasSuffix("\n") ? String(file.text.dropLast()) : file.text
        let lines = Array(shown.split(separator: "\n", omittingEmptySubsequences: false).enumerated())
        List {
            Section {
                Text(verbatim: path).font(.footnote.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
                Text(notes(file)).font(.footnote).foregroundStyle(.secondary)
            }
            if file.binary {
                Text("Binary file: its bytes are not shown.")
            } else if file.text.isEmpty {
                Text("This file is empty")
            } else {
                Section {
                    ForEach(lines, id: \.offset) { line in
                        Text(verbatim: String(line.element))
                            .monoFont()
                            .textSelection(.enabled)
                            .listRowInsets(EdgeInsets(top: 0, leading: 16, bottom: 0, trailing: 16))
                            .listRowSeparator(.hidden)
                    }
                }
            }
        }
        .listStyle(.plain)
        .scalableMonoText(.files)
    }

    private func notes(_ file: NodeFileText) -> String {
        let size = ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file)
        var parts = [String(localized: "\(size) read")]
        if file.truncated {
            parts.append(String(localized: "only the first 512 KiB"))
        }
        if file.redacted {
            parts.append(String(localized: "secrets redacted"))
        }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        guard let client = model.client else { return }
        state = .loading
        state = await .from { try await client.nodeFileRead(node: node, path: path) }
    }
}
