import SwiftUI
import UIKit
import IchorCore

/// One file of an app's source, read only, at the commit the cluster runs: numbered monospace
/// lines, built lazily so a 20k-line values file scrolls, selectable, scrolling sideways rather
/// than wrapping. Copy, and the file's page in the browser; a file over 1 MiB only says so.
/// Edit opens SourceEditView, which turns a change into a pull request through the Git host
/// (with its token); without a token, or off a forge, a bar says why it is not offered.
struct SourceFileView: View {
    let source: GitSource
    let path: String
    /// The app deploying it, for the defaults of the pull request.
    let app: GitOpsAppRef
    /// The line to scroll to and highlight (1-based), nil for the top: the metadata.name of the
    /// resource a diff opened the file for.
    let line: Int?

    // Explicit: the private @State properties make the memberwise init private.
    init(source: GitSource, path: String, app: GitOpsAppRef, line: Int? = nil) {
        self.source = source
        self.path = path
        self.app = app
        self.line = line
    }

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<SourceFileText> = .loading
    @State private var missingHost: String?
    @State private var copied = false
    @State private var editing = false
    /// Why Edit is not offered, nil when it is.
    @State private var editBlock: SourceEditBlock?

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
        .sheet(isPresented: $editing) {
            SourceEditView(source: source, path: path, app: app)
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if case .loaded(let file, _, _) = state, !file.file.truncated {
                    Button { editing = true } label: { Label("Edit", systemImage: "pencil") }
                        .disabled(editBlock != nil)
                }
            }
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
            ScrollViewReader { proxy in
                ScrollView([.horizontal, .vertical]) {
                    LazyVStack(alignment: .leading, spacing: 1) {
                        ForEach(text.lines) { numbered in
                            row(numbered, gutter: text.gutter)
                        }
                    }
                    .padding(.horizontal, 8)
                    .padding(.vertical, 6)
                }
                // The line a diff opened the file at, kept at the left edge.
                .onAppear {
                    if let line { proxy.scrollTo(line, anchor: UnitPoint(x: 0, y: 0.3)) }
                }
            }
            .refreshable { await load() }
            .themedBackground()
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if let editBlock { SourceEditBlockBar(block: editBlock) }
            }
        }
    }

    /// One numbered line; the line asked for is tinted and read out as such.
    private func row(_ numbered: NumberedLine, gutter: CGFloat) -> some View {
        let marked = numbered.number == line
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verbatim: "\(numbered.number)")
                .font(.caption2.monospacedDigit())
                .foregroundStyle(marked ? Color.accentColor : Color(.tertiaryLabel))
                .frame(minWidth: gutter, alignment: .trailing)
                .accessibilityHidden(true)
            Text(verbatim: numbered.text)
                .font(.caption.monospaced())
                .lineLimit(1)
                .fixedSize(horizontal: true, vertical: false)
                .textSelection(.enabled)
        }
        .background(marked ? Color.accentColor.opacity(0.18) : Color.clear)
        .id(numbered.number)
        .accessibilityAddTraits(marked ? .isSelected : [])
    }

    private func load() async {
        guard let client = model.client else { return }
        do {
            let file = try await client.sourceFile(source, path: path, hosts: hosts, demo: demo)
            missingHost = nil
            let saved = source.proposalHost.map { hosts.hasToken($0) } ?? false
            editBlock = sourceEditBlock(source, demo: demo, hasToken: saved)
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

/// Why the file cannot be edited into a pull request, with the shortcut to add the Git host's
/// token when that is what is missing.
private struct SourceEditBlockBar: View {
    let block: SourceEditBlock

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            switch block {
            case .noForge:
                Label("This file cannot be edited here: open it in the browser.", systemImage: "pencil.slash")
            case .noToken(let host):
                Label("Add a token for \(host) to open pull requests.", systemImage: "key")
                NavigationLink { SettingsPageView(page: .gitHosts) } label: { Text("Add a token") }
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
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
        lines = file.truncated ? [] : filterLines(file.content.replacingOccurrences(of: "\r\n", with: "\n"), query: "")
        gutter = CGFloat(max(2, String(lines.count).count)) * 7
    }
}
