import IchorCore
import SwiftUI

/// A read-only file browser on the node, like `talosctl ls` and `talosctl read`: starting
/// points, a path bar, one directory at a time; a file opens in `NodeFileSheet`. The Back
/// button retraces the directories visited before leaving the screen.
struct NodeFilesView: View {
    let node: String
    let hostname: String

    // Explicit: the private @State properties make the memberwise init private.
    init(node: String, hostname: String) {
        self.node = node
        self.hostname = hostname
    }

    private static let startDir = "/var/log"

    @Environment(AppModel.self) private var model
    @State private var history = [NodeFilesView.startDir]
    // Directories reached through a link, which may be a file: their empty state offers a read.
    @State private var viaLink: Set<String> = []
    @State private var state: LoadState<NodeDirListing> = .loading
    @State private var opened: OpenedFile?
    @State private var goTo = false
    @State private var typed = ""

    /// A file shown in the sheet (Identifiable for `sheet(item:)`).
    private struct OpenedFile: Identifiable {
        let path: String
        var id: String { path }
    }

    private var dir: String { history.last ?? Self.startDir }

    var body: some View {
        LoadStateView(state: state, retry: load) { listing in
            List {
                Section {
                    RootChips(dir: dir, roots: listing.roots) { open($0) }
                    PathBar(dir: dir) { open($0) }
                }
                if listing.truncated {
                    Text("Only the first \(String(listing.entries.count)) entries are shown")
                        .font(.footnote).foregroundStyle(.secondary)
                }
                Section {
                    ForEach(listing.entries) { entry in
                        Button {
                            if entry.isDir || entry.isLink {
                                open(entry.path, link: entry.isLink)
                            } else {
                                opened = OpenedFile(path: entry.path)
                            }
                        } label: {
                            NodeFileRow(entry: entry)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .overlay {
                if listing.entries.isEmpty {
                    ContentUnavailableView {
                        Label("This directory is empty", systemImage: "folder")
                    } actions: {
                        if viaLink.contains(dir) {
                            Button("Read it as a file") { opened = OpenedFile(path: dir) }
                        }
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(String(localized: "Node files"))
        .navigationBarTitleDisplayMode(.inline)
        .navigationBarBackButtonHidden(history.count > 1)
        .toolbar {
            if history.count > 1 {
                ToolbarItem(placement: .topBarLeading) {
                    Button {
                        history.removeLast()
                    } label: {
                        Image(systemName: "chevron.backward").accessibilityLabel(Text("Back"))
                    }
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Button {
                    typed = dir + "/"
                    goTo = true
                } label: {
                    Image(systemName: "arrow.right.doc.on.clipboard").accessibilityLabel(Text("Go to path"))
                }
            }
        }
        .alert("Go to path", isPresented: $goTo) {
            TextField("Absolute path", text: $typed)
                .font(.body.monospaced())
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("List") {
                if let path = NodePaths.typed(typed) {
                    open(trimmed(path))
                }
            }
            Button("Read") {
                if let path = NodePaths.typed(typed), !path.hasSuffix("/") {
                    opened = OpenedFile(path: path)
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Type an absolute path, without ..")
        }
        .sheet(item: $opened) { file in
            NodeFileSheet(node: node, path: file.path)
        }
        .task(id: dir) { await load() }
    }

    private func trimmed(_ path: String) -> String {
        let cut = path.hasSuffix("/") && path.count > 1 ? String(path.dropLast()) : path
        return cut.isEmpty ? "/" : cut
    }

    private func open(_ path: String, link: Bool = false) {
        if path != dir { history.append(path) }
        if link { viaLink.insert(path) }
    }

    private func load() async {
        guard let client = model.client else { return }
        let target = dir
        state = .loading
        state = await .from { try await client.nodeFiles(node: node, dir: target) }
    }
}

/// The starting points, the one holding the directory selected.
private struct RootChips: View {
    let dir: String
    let roots: [String]
    let onRoot: (String) -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(roots, id: \.self) { root in
                    let selected = dir == root || dir.hasPrefix(root + "/")
                    Button {
                        onRoot(root)
                    } label: {
                        Text(verbatim: root).font(.footnote.monospaced())
                    }
                    .buttonStyle(.bordered)
                    .tint(selected ? .accentColor : .secondary)
                    .accessibilityAddTraits(selected ? .isSelected : [])
                }
            }
        }
    }
}

/// The path as tappable steps; the last one is where the list is.
private struct PathBar: View {
    let dir: String
    let onCrumb: (String) -> Void

    var body: some View {
        let crumbs = NodePaths.crumbs(dir)
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 2) {
                ForEach(Array(crumbs.enumerated()), id: \.element.path) { index, crumb in
                    if index > 1 {
                        Text(verbatim: "/").foregroundStyle(.secondary)
                    }
                    Button {
                        onCrumb(crumb.path)
                    } label: {
                        Text(verbatim: crumb.name).font(.callout.monospaced()).lineLimit(1)
                    }
                    .buttonStyle(.borderless)
                    .disabled(index == crumbs.count - 1)
                }
            }
        }
    }
}

private struct NodeFileRow: View {
    let entry: NodeFileEntry

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: entry.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle)
                detail.font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
        } icon: {
            Image(systemName: entry.isDir ? "folder" : entry.isLink ? "link" : "doc")
        }
        .contentShape(Rectangle())
    }

    @ViewBuilder
    private var detail: some View {
        if entry.isLink {
            Text("Link to \(entry.target)")
        } else if entry.isDir {
            if let date = entry.modifiedDate {
                Text(date, format: .relative(presentation: .named))
            }
        } else {
            HStack(spacing: 8) {
                Text(verbatim: ByteCountFormatter.string(fromByteCount: entry.size, countStyle: .file))
                if let date = entry.modifiedDate {
                    Text(date, format: .relative(presentation: .named))
                }
            }
        }
    }
}
