import SwiftUI
import UIKit
import IchorCore

/// One file of an app's source, read only, at the commit the cluster runs: numbered monospace
/// lines, built lazily so a 20k-line values file scrolls, selectable, scrolling sideways rather
/// than wrapping. Copy, and the file's page in the browser; a file over 1 MiB only says so.
/// Edit opens SourceEditView, which turns a change into a pull request through the Git host
/// (with its token); without a token, or off a forge, a bar says why it is not offered.
/// Quick edit, when the file sets images or replicas, picks a tag or a count and opens the
/// editor straight at the review (same rules as Edit).
struct SourceFileView: View {
    let source: GitSource
    let path: String
    /// The app deploying it, for the defaults of the pull request.
    let app: GitOpsAppRef
    /// The line to scroll to and highlight (1-based), nil for the top: the metadata.name of the
    /// resource a diff opened the file for.
    let line: Int?
    /// False when opened from a node's machine config (Compare with Git): no Try on a node there,
    /// it would stack a second editor.
    let allowTry: Bool

    // Explicit: the private @State properties make the memberwise init private.
    init(source: GitSource, path: String, app: GitOpsAppRef, line: Int? = nil, allowTry: Bool = true) {
        self.source = source
        self.path = path
        self.app = app
        self.line = line
        self.allowTry = allowTry
    }

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<SourceFileText> = .loading
    @State private var missingHost: String?
    @State private var copied = false
    @State private var editing = false
    /// Why Edit is not offered, nil when it is.
    @State private var editBlock: SourceEditBlock?
    /// What the file offers to quick edits, nil when nothing (no menu).
    @State private var quickTargets: QuickEditTargets?
    @State private var quickSheet: QuickEditSheet?
    /// The edit a quick sheet chose: the editor opens with it once the sheet is gone.
    @State private var pendingQuickEdit: QuickEdit?
    /// The quick edit the editor opens with, nil for the plain editor.
    @State private var quickEdit: QuickEdit?
    /// The file is a Talos patch (on a Talos cluster): whether and why it can be tried on a node.
    @State private var talosPatch: TalosPatchInfo?
    @State private var pickingNode = false
    /// The node the picker chose: the machine config screen opens once the sheet is gone.
    @State private var pendingTry: PatchTarget?
    @State private var tryTarget: PatchTarget?
    /// The file is encrypted with SOPS: shown, never edited on the phone.
    @State private var sops: SopsInfo?

    private var demo: Bool { model.activeSummary?.demo == true }

    private var name: String { path.split(separator: "/").last.map(String.init) ?? path }

    private var page: URL? {
        hosts.browserURL(gitWebURL(source.webURL, kind: hosts.kind(of: source.host), ref: source.ref, path: path, dir: false))
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
            SourceEditView(source: source, path: path, app: app, quickEdit: quickEdit)
        }
        .sheet(item: $quickSheet, onDismiss: openPendingQuickEdit) { sheet in
            quickSheetView(sheet)
        }
        .sheet(isPresented: $pickingNode, onDismiss: openPendingTry) {
            if let talosPatch { TalosPatchNodeSheet(info: talosPatch) { node in pickNode(node) } }
        }
        .navigationDestination(item: $tryTarget) { target in
            MachineConfigView(node: target.node, hostname: target.hostname, patch: target.patch)
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if case .loaded(let file, _, _) = state, !file.file.truncated, sops == nil {
                    Button {
                        quickEdit = nil
                        editing = true
                    } label: { Label("Edit", systemImage: "pencil") }
                        .disabled(editBlock != nil)
                }
            }
            ToolbarItem(placement: .primaryAction) {
                if case .loaded(let file, _, _) = state, !file.file.truncated, sops == nil, let quickTargets {
                    quickMenu(quickTargets)
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
                VStack(spacing: 0) {
                    if let sops {
                        SopsBar(info: sops)
                    } else {
                        if let talosPatch { TalosPatchBar(info: talosPatch) { pickingNode = true } }
                        if let editBlock { SourceEditBlockBar(block: editBlock) }
                    }
                }
            }
        }
    }

    /// Change image tag and Set replicas, for what the file sets.
    private func quickMenu(_ targets: QuickEditTargets) -> some View {
        Menu {
            if !targets.images.isEmpty {
                Button { quickSheet = .image } label: { Label("Change image tag", systemImage: "shippingbox") }
            }
            if !targets.replicas.isEmpty {
                Button { quickSheet = .replicas } label: { Label("Set replicas", systemImage: "square.stack.3d.up") }
            }
        } label: {
            Label("Quick edit", systemImage: "wand.and.stars")
        }
        .disabled(editBlock != nil)
    }

    @ViewBuilder private func quickSheetView(_ sheet: QuickEditSheet) -> some View {
        if case .loaded(let file, _, _) = state, let quickTargets {
            switch sheet {
            case .image:
                QuickEditImageSheet(targets: quickTargets.images, content: file.file.content, app: app) { edit in
                    pendingQuickEdit = edit
                    quickSheet = nil
                }
            case .replicas:
                QuickEditReplicasSheet(targets: quickTargets.replicas, content: file.file.content) { edit in
                    pendingQuickEdit = edit
                    quickSheet = nil
                }
            }
        }
    }

    /// The quick sheet is gone: the editor opens with its edit, at the review.
    private func openPendingQuickEdit() {
        guard let edit = pendingQuickEdit else { return }
        pendingQuickEdit = nil
        quickEdit = edit
        editing = true
    }

    /// The node picked: the machine config screen of it opens with the file once the sheet is gone.
    private func pickNode(_ node: NodeOverview) {
        guard case .loaded(let file, _, _) = state else { return }
        let rev = shortRevision(source.ref)
        let from = rev.isEmpty ? path : "\(path) @ \(rev)"
        pendingTry = PatchTarget(node: node.node, hostname: node.hostname, patch: PatchRequest(content: file.file.content, from: from))
        pickingNode = false
    }

    private func openPendingTry() {
        guard let target = pendingTry else { return }
        pendingTry = nil
        tryTarget = target
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
            quickTargets = await Self.quickTargets(of: file)
            sops = file.truncated ? nil : (try? await TalosClient.sopsInspect(file.content)).flatMap { $0.encrypted ? $0 : nil }
            let encrypted = sops != nil
            talosPatch = model.activeIsKube || !allowTry ? nil : await Self.talosPatch(of: file, path: path)
            state = .loaded(SourceFileText(file: file, collapseSops: encrypted), at: Date())
        } catch let missing as GitHostMissing {
            missingHost = missing.host
        } catch {
            missingHost = nil
            state = state.refreshed(with: .failed(error.localizedDescription))
        }
    }

    /// What file offers to quick edits; nil when nothing, or the Go core cannot read it (not
    /// YAML): the menu is then hidden, Edit still works.
    private static func quickTargets(of file: GitFile) async -> QuickEditTargets? {
        guard !file.truncated, let targets = try? await TalosClient.quickEditTargets(file.content),
              !targets.isEmpty else { return nil }
        return targets
    }

    /// What file is to Talos when it is a patch; nil for any other file (no bar).
    private static func talosPatch(of file: GitFile, path: String) async -> TalosPatchInfo? {
        let yaml = path.hasSuffix(".yaml") || path.hasSuffix(".yml")
        guard yaml, !file.truncated, file.content.utf8.count <= PatchRequest.maxBytes,
              let info = try? await TalosClient.talosPatchDetect(file.content), info.talos else { return nil }
        return info
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

/// The quick edit sheet shown.
private enum QuickEditSheet: String, Identifiable {
    case image, replicas

    var id: String { rawValue }
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

    /// collapseSops: the file is SOPS-encrypted, its ciphertext tells nothing: each ENC[…] value
    /// shows as such.
    init(file: GitFile, collapseSops: Bool = false) {
        self.file = file
        let text = collapseSops ? collapseSopsValues(file.content) : file.content
        lines = file.truncated ? [] : filterLines(text.replacingOccurrences(of: "\r\n", with: "\n"), query: "")
        gutter = CGFloat(max(2, String(lines.count).count)) * 7
    }
}
