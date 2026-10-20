import SwiftUI
import IchorCore

/// What the editor started from: the file as the Git host has it at the branch the pull
/// request goes to (or, for an app following a tag or a commit, at what it follows), and its
/// sha, the optimistic lock of the proposal. Reload replaces it.
struct SourceEditBase {
    let file: GitFile
    /// The branch, tag or commit the file was read at.
    let ref: String
}

/// Edits one file of an app's source, then shows the diff, then opens a pull request with it
/// (ProposeSheet): the change lands in Git and the controller applies it after merge, the
/// tracked branch is never pushed to. Always through the Git host with its token, also for a
/// Flux source read from its artifact. The draft lives here, never in a route.
/// A quick edit (an image tag, the replicas) is applied to the file as read here, at the
/// branch, and opens straight at the review with its own title.
struct SourceEditView: View {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    /// The quick edit to apply once the file is read, nil for the plain editor.
    let quickEdit: QuickEdit?

    // Explicit: the private @State properties make the memberwise init private.
    init(source: GitSource, path: String, app: GitOpsAppRef, quickEdit: QuickEdit? = nil) {
        self.source = source
        self.path = path
        self.app = app
        self.quickEdit = quickEdit
        _target = State(initialValue: proposalBaseRef(for: source) ?? "main")
    }

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<SourceEditBase> = .loading
    @State private var draft = ""
    /// The caret in the draft (UTF-16), for the schema help.
    @State private var cursor = 0
    @State private var help: KubeSchemaHelpRequest?
    /// The Kubernetes type the file declares, for the schema help; nil hides it.
    @State private var manifestType: SourceManifestType?
    @State private var reviewing = false
    @State private var confirmingDiscard = false
    @State private var missingHost: String?
    /// The branch the pull request goes to, as last named in the sheet.
    @State private var target: String
    /// The file as the quick edit rewrote it: its title holds while the draft is that.
    @State private var quickContent: String?
    /// Why the quick edit was not applied: the editor stays, with the file as read.
    @State private var quickFailure: String?
    /// The live values a keep edit left, because the file does not set them; named on the review.
    @State private var quickSkipped: String?

    private var demo: Bool { model.activeSummary?.demo == true }

    private var name: String { path.split(separator: "/").last.map(String.init) ?? path }

    private var base: SourceEditBase? {
        if case .loaded(let base, _, _) = state { return base }
        return nil
    }

    private var changed: Bool { base.map { !sameSourceText($0.file.content, draft) } ?? false }

    /// The quick edit's pull request title, while the draft is what it wrote.
    private var quickTitle: String? {
        guard let quickEdit, let quickContent, sameSourceText(quickContent, draft) else { return nil }
        return quickEditTitle(quickEdit)
    }

    var body: some View {
        NavigationStack {
            Group {
                if let missingHost {
                    GitHostMissingView(host: missingHost, page: source.webURL)
                        .themedBackground()
                } else {
                    LoadStateView(state: state, retry: load) { _ in editor }
                }
            }
            .navigationTitle(Text(verbatim: name))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        if changed { confirmingDiscard = true } else { dismiss() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Review") { reviewing = true }
                        .disabled(!changed)
                }
            }
            .navigationDestination(isPresented: $reviewing) {
                if let base {
                    SourceEditReview(source: source, path: path, app: app, base: base, edited: draft, target: target,
                                     defaultTitle: quickTitle, notice: quickTitle == nil ? nil : quickSkipped,
                                     onReload: reload(at:), onDone: { dismiss() })
                }
            }
            .sheet(item: $help) { request in
                if let manifestType {
                    let resource = KubeAPIResource(group: manifestType.group, version: manifestType.version, resource: "",
                                                   kind: manifestType.kind)
                    KubeSchemaHelpSheet(resource: resource, request: request) { child, listItem in
                        let inserted = insertYamlField(draft, utf16Offset: request.offset, name: child.name, listItem: listItem)
                        cursor = inserted.cursor
                        draft = inserted.text
                    }
                }
            }
            .confirmationDialog("Discard your changes?", isPresented: $confirmingDiscard, titleVisibility: .visible) {
                Button("Discard", role: .destructive) { dismiss() }
                Button("Keep editing", role: .cancel) {}
            } message: {
                Text("Your edits to this file are lost.")
            }
            .alert(quickFailure ?? "", isPresented: $quickFailure.isPresent()) {
                Button("OK") {}
            }
        }
        .interactiveDismissDisabled(changed)
        // Only the first time: coming back from the review keeps the draft.
        .task { if base == nil { await load() } }
        // A token added through the shortcut: read again on the way back.
        .onChange(of: hosts.hosts) { _, _ in
            if base == nil { Task { await load() } }
        }
    }

    private var editor: some View {
        ConfigYamlEditor(text: $draft, error: nil, cursor: $cursor)
            .safeAreaInset(edge: .top, spacing: 0) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: path)
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    if manifestType != nil { YamlSchemaHelpButtons(open: openHelp) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal)
                .padding(.vertical, 6)
                .background(.bar)
            }
    }

    /// Opens the schema help for the field at the caret.
    private func openHelp(_ mode: KubeSchemaHelpRequest.Mode) {
        help = KubeSchemaHelpRequest(mode: mode, cursor: yamlCursorAt(draft, utf16Offset: cursor), offset: cursor)
    }

    /// The first read: at the branch the app follows, else at what it follows (a tag, a commit).
    private func load() async {
        await read(at: proposalBaseRef(for: source) ?? source.ref)
        await applyQuickEdit()
    }

    /// The quick edit on the file as read at the branch, then the review; once.
    private func applyQuickEdit() async {
        guard let quickEdit, quickContent == nil, let base else { return }
        do {
            let result = try await TalosClient.quickEdit(base.file.content, quickEdit)
            guard result.changed else {
                quickFailure = String(localized: "This value is already set.")
                return
            }
            quickContent = result.content
            draft = result.content
            if !result.skipped.isEmpty {
                quickSkipped = String(localized: "Not kept, set elsewhere: \(result.skipped.joined(separator: "; "))")
            }
            reviewing = true
        } catch {
            quickFailure = error.localizedDescription
        }
    }

    /// The file changed on branch since it was read: read it there again, the edits dropped
    /// (they were made on the old version), back to the editor.
    private func reload(at branch: String) {
        target = branch
        reviewing = false
        Task { await read(at: branch) }
    }

    private func read(at ref: String) async {
        guard let client = model.client else { return }
        state = .loading
        do {
            // A node file the write-back creates: proposed as a new file (no sha).
            let creating = quickEdit?.op == .talos && quickEdit?.create == true
            let file = creating ? GitFile(path: path) : try await client.forgeFile(source, path: path, ref: ref, hosts: hosts, demo: demo)
            missingHost = nil
            guard !file.truncated else {
                state = .failed(String(localized: "This file is larger than 1 MiB: open it in the browser to read it."))
                return
            }
            draft = file.content
            cursor = 0
            manifestType = sourceManifestType(file.content)
            state = .loaded(SourceEditBase(file: file, ref: ref), at: Date())
        } catch let missing as GitHostMissing {
            missingHost = missing.host
        } catch {
            missingHost = nil
            state = .failed(error.localizedDescription)
        }
    }
}

/// The unified diff of the edit, red and green, then Continue to the pull request.
private struct SourceEditReview: View {
    let source: GitSource
    let path: String
    let app: GitOpsAppRef
    let base: SourceEditBase
    let edited: String
    let target: String
    /// The quick edit's pull request title, nil for the Go core's.
    let defaultTitle: String?
    /// What the quick edit left out, shown above the diff.
    var notice: String?
    let onReload: (String) -> Void
    let onDone: () -> Void

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<FluxDiff> = .loading
    /// The build check run with the review: the kustomization built with the edit.
    @State private var check: LoadState<GitOpsEditCheck> = .loading
    @State private var proposing = false
    /// A clear-text secret where .sops.yaml encrypts: asked once more before the pull request.
    @State private var confirmingSops = false
    /// The .sops.yaml read at the branch: a quick call of its own, Continue waits for it.
    @State private var sopsCheck: LoadState<SopsWarning>?

    private var sopsWarning: SopsWarning? {
        if case .loaded(let w, _, _) = sopsCheck, w.warn { return w }
        return nil
    }

    private var sopsPending: Bool {
        if case .loading = sopsCheck { return true }
        return false
    }

    var body: some View {
        LoadStateView(state: state, retry: load) { diff in
            let file = diff.resources.first
            List {
                if let notice {
                    Section { Text(verbatim: notice).font(.caption).foregroundStyle(.secondary) }
                }
                if let file, file.change == .changed {
                    Section {
                        DiffLinesView(lines: file.lines)
                            .listRowInsets(EdgeInsets(top: 6, leading: 8, bottom: 6, trailing: 8))
                        if file.truncated {
                            Text("Diff cut at 64 KB.").font(.caption2).foregroundStyle(.secondary)
                        }
                    } header: {
                        let counts = file.counts
                        HStack(spacing: 10) {
                            Text(verbatim: path).lineLimit(1).truncationMode(.middle)
                            Text(verbatim: "+\(counts.added)").foregroundStyle(.statusOK)
                            Text(verbatim: "−\(counts.removed)").foregroundStyle(.statusBad)
                        }
                        .font(.caption.monospacedDigit().weight(.semibold))
                    }
                    if let sopsWarning {
                        Section { SopsWarningText(warning: sopsWarning) }
                    }
                    EditCheckSection(state: check) { Task { await runCheck() } }
                } else {
                    Section {
                        Label("Nothing changed yet.", systemImage: "checkmark.circle")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .themedBackground()
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if file?.change == .changed { continueBar }
            }
        }
        .navigationTitle(Text("Review"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .sheet(isPresented: $proposing) {
            ProposeSheet(source: source, path: path, app: app, base: base, content: edited, target: target,
                         defaultTitle: defaultTitle, onReload: onReload, onDone: onDone)
        }
    }

    private var continueBar: some View {
        VStack(spacing: 6) {
            // The pull request can still be opened: the person decides.
            if case .loaded(let c, _, _) = check, c.fails {
                Label("Build fails", systemImage: "xmark.octagon").font(.footnote).foregroundStyle(.statusBad)
            }
            Button {
                if sopsWarning != nil { confirmingSops = true } else { proposing = true }
            } label: {
                Text("Continue").frame(maxWidth: .infinity)
            }
            .disabled(sopsPending)
            .confirmationDialog("Commit a clear-text secret?", isPresented: $confirmingSops, titleVisibility: .visible) {
                Button("Continue", role: .destructive) { proposing = true }
                Button("Cancel", role: .cancel) {}
            } message: {
                if let w = sopsWarning {
                    Text("\(sopsWhat(w)) would be committed in clear text at a path the rule \(w.rule?.pathRegex ?? "") encrypts. Encrypt it with sops on a computer first, unless you mean it.")
                }
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
        }
        .padding()
        .background(.bar)
    }

    private func load() async {
        let before = base.file.content
        let after = edited
        let path = path
        state = .loading
        state = await .from { try await TalosClient.textDiff(before: before, after: after, name: path) }
        async let sops: Void = runSops()
        await runCheck()
        await sops
    }

    /// Reads .sops.yaml at the branch: whether the edit commits a clear-text secret where it encrypts.
    private func runSops() async {
        guard edited != base.file.content, let client = model.client else {
            sopsCheck = nil
            return
        }
        sopsCheck = .loading
        sopsCheck = await .from {
            try await client.sopsEditWarning(source, ref: base.ref, path: path, content: edited,
                                             hosts: hosts, demo: model.activeSummary?.demo == true)
        }
    }

    /// Builds the edit's kustomization on the repository snapshot: whether it still builds and
    /// which rendered objects change. A Flux app's source is built from its path.
    private func runCheck() async {
        guard edited != base.file.content else { return }
        let flux = app.tool == "flux" && app.kind == "Kustomization" ? GitOpsEditCheck.fluxSpec(path: source.path) : ""
        check = .loading
        check = await .from {
            try await TalosClient.editCheck(source, ref: base.ref, path: path, content: edited, fluxSpec: flux,
                                       hosts: hosts, demo: model.activeSummary?.demo == true)
        }
    }
}
