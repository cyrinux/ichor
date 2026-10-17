import SwiftUI
import UIKit
import IchorCore

/// One file of an app's source, read only, at the commit the cluster runs: numbered monospace
/// lines, built lazily so a 20k-line values file scrolls, selectable, scrolling sideways rather
/// than wrapping. Copy, and the file's page in the browser; a file over 1 MiB only says so.
struct SourceFileView: View {
    let source: GitSource
    let path: String

    // Explicit: the private @State properties make the memberwise init private.
    init(source: GitSource, path: String) {
        self.source = source
        self.path = path
    }

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<SourceFileText> = .loading
    @State private var missingHost: String?
    @State private var copied = false

    private var demo: Bool { model.activeSummary?.demo == true }

    private var name: String { path.split(separator: "/").last.map(String.init) ?? path }

    private var page: URL? {
        webURL(gitWebURL(source.webURL, kind: hosts.kind(of: source.host), ref: source.ref, path: path, dir: false))
    }

    var body: some View {
        Group {
            if let missingHost {
                GitHostMissingView(host: missingHost, page: source.webURL)
                    .themedBackground()
            } else {
                LoadStateView(state: state, retry: load) { file in content(file) }
            }
        }
        .task { await load() }
        .onChange(of: hosts.hosts) { _, _ in Task { await load() } }
        .navigationTitle(Text(verbatim: name))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if case .loaded(let file, _, _) = state, !file.file.truncated {
                    Button { copy(file.file.content) } label: {
                        Label(copied ? LocalizedStringKey("Copied") : LocalizedStringKey("Copy"), systemImage: copied ? "checkmark" : "doc.on.doc")
                    }
                }
            }
            ToolbarItem(placement: .primaryAction) {
                if let page {
                    Link(destination: page) { Label("Open in browser", systemImage: "safari") }
                }
            }
        }
    }

    @ViewBuilder private func content(_ text: SourceFileText) -> some View {
        if text.file.truncated {
            ContentUnavailableView {
                Label { Text(verbatim: name) } icon: { Image(systemName: "doc.text") }
            } description: {
                Text("This file is larger than 1 MiB: open it in the browser to read it.")
            } actions: {
                if let page { Link(destination: page) { Text("Open in browser") } }
            }
            .themedBackground()
        } else {
            ScrollView([.horizontal, .vertical]) {
                LazyVStack(alignment: .leading, spacing: 1) {
                    ForEach(text.lines) { line in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(verbatim: "\(line.number)")
                                .font(.caption2.monospacedDigit())
                                .foregroundStyle(.tertiary)
                                .frame(minWidth: text.gutter, alignment: .trailing)
                                .accessibilityHidden(true)
                            Text(verbatim: line.text)
                                .font(.caption.monospaced())
                                .lineLimit(1)
                                .fixedSize(horizontal: true, vertical: false)
                                .textSelection(.enabled)
                        }
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 6)
            }
            .refreshable { await load() }
            .themedBackground()
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        do {
            let file = try await client.sourceFile(source, path: path, hosts: hosts, demo: demo)
            missingHost = nil
            state = .loaded(SourceFileText(file: file), at: Date())
        } catch let missing as GitHostMissing {
            missingHost = missing.host
        } catch {
            missingHost = nil
            state = state.refreshed(with: .failed(error.localizedDescription))
        }
    }

    private func copy(_ content: String) {
        UIPasteboard.general.string = content
        copied = true
        announce(String(localized: "Copied"))
        Task {
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }
}

/// A file with its lines split once, not on every render.
struct SourceFileText {
    let file: GitFile
    let lines: [NumberedLine]
    /// The line-number column's width, for the widest number.
    let gutter: CGFloat

    init(file: GitFile) {
        self.file = file
        lines = file.truncated ? [] : filterLines(file.content, query: "")
        gutter = CGFloat(max(2, String(lines.count).count)) * 7
    }
}
