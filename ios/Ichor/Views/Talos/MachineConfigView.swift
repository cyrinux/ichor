import SwiftUI
import IchorCore
import UniformTypeIdentifiers

private enum ConfigTab: String {
    case pretty, yaml, meta
}

/// Machine config (os:admin) as a schema-described tree or as YAML, secrets masked unless
/// the user reveals them. Editing works on a draft that never holds the secrets; the draft
/// is reviewed, then tried on the node, which reverts by itself unless the change is kept.
struct MachineConfigView: View {
    let node: String
    let hostname: String
    /// A drift difference from Insights: the screen opens a draft that undoes it, then the review.
    let fix: DriftFix?
    /// A snippet to add (a pod's pull problem: the registry mirror): the screen opens a draft
    /// and the snippet's form on it.
    let snippet: SnippetRequest?
    /// A repository's Talos patch file: the screen opens a draft with it applied, then the review.
    let patch: PatchRequest?

    init(
        node: String, hostname: String, fix: DriftFix? = nil, snippet: SnippetRequest? = nil, patch: PatchRequest? = nil
    ) {
        self.node = node
        self.hostname = hostname
        self.fix = fix
        self.snippet = snippet
        self.patch = patch
    }

    @Environment(AppModel.self) private var model
    @Environment(ClusterGitStore.self) private var git
    @Environment(LockStore.self) private var lockStore
    @AppStorage("machineConfig.tab") private var tab = ConfigTab.pretty
    @State private var state: LoadState<String> = .loading
    @State private var reveal = false
    @State private var query = ""
    @State private var message: String?
    /// Talos version whose schema describes the tree; empty without a schema.
    @State private var schemaVersion = ""
    @State private var tree: ConfigTree?
    @State private var treeFailure: String?
    /// The YAML being edited; nil when only reading.
    @State private var draft: String?
    @State private var describing: Task<Void, Never>?
    @State private var confirmingDiscard = false
    @State private var reviewing = false
    /// The try running on this node, shown again (the screen was left meanwhile).
    @State private var followingTry = false
    /// The field edits made to the draft, in order: what other nodes can be given.
    @State private var edits: [ConfigEdit] = []
    /// False once the YAML was typed into: that change is text, it cannot be replayed elsewhere.
    @State private var replayable = true
    @State private var pickingSnippet = false
    @State private var fixApplied = false
    @State private var patchApplied = false
    /// The snippet the picker opens on, until it closes once.
    @State private var pendingSnippet: SnippetRequest?
    @State private var snippetOpened = false
    @State private var comparingGit = false

    var body: some View {
        VStack(spacing: 0) {
            header
            if showingMeta {
                MetaKeysView(node: node, hostname: hostname)
            } else {
                LoadStateView(state: state, retry: load) { yaml in content(yaml) }
            }
        }
        .navigationTitle("Machine config")
        .clusterScreenTitle(Text("Machine config"))
        .navigationBarTitleDisplayMode(.inline)
        // Leaving would drop the draft: Discard is the way out.
        .navigationBarBackButtonHidden(draft != nil)
        .searchable(text: $query, prompt: tab == .pretty ? Text("Filter fields") : Text("Filter lines"))
        .toolbar { toolbar }
        .confirmationDialog("Discard your changes?", isPresented: $confirmingDiscard, titleVisibility: .visible) {
            Button("Discard changes", role: .destructive) { stopEditing() }
            Button("Keep editing", role: .cancel) {}
        }
        .sheet(isPresented: $reviewing) {
            if let base = loadedYAML, let draft {
                ConfigReviewView(
                    node: node, hostname: hostname, base: base, draft: draft, edits: replayable ? edits : []
                ) {
                    Task { await finishTry() }
                }
            }
        }
        .fullScreenCover(isPresented: $followingTry, onDismiss: { Task { await finishTry() } }) {
            ConfigTryView(node: node, hostname: hostname, request: nil) { _ in }
        }
        .sheet(isPresented: $comparingGit) {
            if let repo = git.repo(for: model.activeSummary) {
                GitCompareSheet(
                    repo: repo, node: node, hostname: hostname, role: configRole(loadedYAML ?? ""), live: loadedYAML
                ) { git in
                    Task { await editFrom(git) }
                }
            }
        }
        .sheet(isPresented: $pickingSnippet, onDismiss: { pendingSnippet = nil }) {
            SnippetPickerSheet(initial: pendingSnippet) { id, params in
                Task { await applySnippet(id: id, params: params) }
            }
        }
        .task {
            await load()
            await applyFixOnce()
            await applyPatchOnce()
            await openSnippetOnce()
        }
        .allowsLandscape()
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Picker("Machine config", selection: $tab) {
                Text("Fields").tag(ConfigTab.pretty)
                Text(verbatim: "YAML").tag(ConfigTab.yaml)
                // META is not part of the draft: out of the way while editing.
                if draft == nil {
                    Text(verbatim: "META").tag(ConfigTab.meta)
                }
            }
            .pickerStyle(.segmented)
            if showingMeta {
                EmptyView()  // the META keys are not the config: nothing to reveal
            } else if draft != nil {
                Label(
                    "Editing a draft: nothing changes on the node until you review and try it.", systemImage: "pencil"
                )
                .font(.footnote)
                .foregroundStyle(.secondary)
                // The repository file the draft comes from.
                if let patch {
                    Label {
                        Text(verbatim: patch.from).lineLimit(1).truncationMode(.middle)
                    } icon: {
                        Image(systemName: "doc.text")
                    }
                    .font(.footnote.monospaced())
                    .foregroundStyle(.secondary)
                }
            } else {
                Toggle(isOn: Binding(get: { reveal }, set: { on in Task { await setReveal(on) } })) {
                    Label("Reveal secrets", systemImage: reveal ? "eye" : "eye.slash")
                }
                if reveal {
                    Label(
                        "Secrets are shown in clear: CA keys and tokens give full control of the cluster. Don't share screenshots.",
                        systemImage: "exclamationmark.triangle.fill"
                    )
                    .font(.footnote)
                    .foregroundStyle(.statusWarn)
                }
            }
            if ConfigTryJob.shared.target?.node == node,
                ConfigTryJob.shared.isActive || ConfigTryJob.shared.outcome != nil
            {
                Button {
                    followingTry = true
                } label: {
                    if ConfigTryJob.shared.isActive {
                        Label("A config change is being tried: show the countdown", systemImage: "timer")
                    } else {
                        Label("Show how the try ended", systemImage: "timer")
                    }
                }
                .font(.footnote)
            }
            if let message {
                Text(message).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    /// The META tab, only while reading: editing shows the fields instead.
    private var showingMeta: Bool { tab == .meta && draft == nil }

    @ViewBuilder private func content(_ yaml: String) -> some View {
        switch tab {
        case .yaml:
            if draft != nil {
                ConfigYamlEditor(text: draftText, error: tree?.error)
            } else {
                ConfigYamlLines(yaml: yaml, query: query, refresh: load)
            }
        case .pretty, .meta:
            if let error = tree?.error {
                ContentUnavailableView {
                    Label("The YAML is not valid", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(verbatim: configSyntaxText(error))
                }
            } else if let tree {
                ConfigTreeView(
                    tree: tree, query: query, editing: draft != nil, revealed: reveal,
                    apply: applyEdit, refresh: load)
            } else if let treeFailure {
                ContentUnavailableView {
                    Label("Request failed", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(verbatim: treeFailure)
                } actions: {
                    Button("Retry") { Task { await describe() } }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if draft != nil {
            ToolbarItem(placement: .cancellationAction) {
                Button("Discard", action: requestDiscard)
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Review") { reviewing = true }
                    .disabled(!canReview)
            }
            ToolbarItem(placement: .bottomBar) {
                Button {
                    pickingSnippet = true
                } label: {
                    Label("Snippets", systemImage: "wand.and.stars")
                }
            }
        } else if !showingMeta {
            ToolbarItemGroup(placement: .primaryAction) {
                Button(action: copy) {
                    Label("Copy", systemImage: "doc.on.doc")
                }
                .disabled(loadedYAML == nil)
                Button {
                    Task { await startEditing() }
                } label: {
                    Label("Edit", systemImage: "pencil")
                }
                .disabled(loadedYAML == nil)
                Button {
                    if git.repo(for: model.activeSummary) != nil {
                        comparingGit = true
                    } else {
                        message = String(
                            localized:
                                "No repository is saved for this cluster: add one from its menu in the cluster list (Cluster repository)."
                        )
                    }
                } label: {
                    Label("Compare with Git", systemImage: "arrow.triangle.branch")
                }
                .disabled(loadedYAML == nil)
            }
        }
    }

    private var loadedYAML: String? {
        if case .loaded(let yaml, _, _) = state { return yaml }
        return nil
    }

    /// A changed draft that parses; the node validates the rest in the review.
    private var canReview: Bool {
        guard let draft, let tree else { return false }
        return draft != loadedYAML && tree.error == nil
    }

    /// Typing re-describes the draft after a pause.
    private var draftText: Binding<String> {
        Binding(
            get: { draft ?? "" },
            set: { new in
                guard draft != nil, new != draft else { return }
                draft = new
                replayable = false
                describing?.cancel()
                describing = Task {
                    try? await Task.sleep(for: .milliseconds(400))
                    guard !Task.isCancelled else { return }
                    await describe()
                }
            })
    }

    /// Revealing needs a fresh Face ID / passcode when the app lock is on; hiding re-fetches
    /// the redacted config so the secrets leave memory and screen.
    private func setReveal(_ on: Bool) async {
        if on, lockStore.lock.enabled,
            let failure = await Authenticator.authenticate(
                reason: String(localized: "Reveal the secrets of \(hostname)"))
        {
            message = failure
            return
        }
        reveal = on
        message = nil
        state = .loading
        tree = nil
        await load()
    }

    /// The config and, alongside, the schema of the node's Talos version (downloaded once);
    /// without a schema the tree only lacks its descriptions. Not while editing: the draft
    /// was made from the config on screen.
    private func load() async {
        guard let client = model.client, draft == nil else { return }
        let wanted = reveal
        async let schema = try? client.machineConfigSchema(node: node)
        let result: LoadState<String> = await .from {
            try await client.machineConfig(node: node, revealSecrets: wanted)
        }
        let version = await schema?.version ?? ""
        guard wanted == reveal, draft == nil else { return }  // a newer toggle wins
        schemaVersion = version
        state = result
        await describe()
    }

    /// The tree of what is on screen: the draft when editing, else the loaded config.
    private func describe() async {
        guard let client = model.client, let text = draft ?? loadedYAML else { return }
        do {
            let described = try await client.describeMachineConfig(text, talosVersion: schemaVersion)
            guard text == (draft ?? loadedYAML) else { return }  // a newer text wins
            tree = described
            treeFailure = nil
        } catch {
            guard text == (draft ?? loadedYAML) else { return }
            tree = nil
            treeFailure = error.localizedDescription
        }
    }

    /// One field edit on the draft; returns why it was refused.
    private func applyEdit(_ edit: ConfigEdit) async -> String? {
        guard let client = model.client, let current = draft else { return nil }
        do {
            let edited = try await client.editMachineConfig(current, edit: edit)
            guard draft == current else { return nil }
            describing?.cancel()
            draft = edited
            edits.append(edit)
            await describe()
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    /// Snippet `id` with `params`, as edits of the draft.
    private func applySnippet(id: String, params: [String: String]) async {
        guard let client = model.client, let current = draft else { return }
        do {
            await applyEdits(try await client.configSnippetEdits(current, id: id, params: params))
        } catch {
            message = error.localizedDescription
        }
    }

    /// Opened with a snippet to add: a draft, then the snippet's form on it.
    private func openSnippetOnce() async {
        guard let snippet, !snippetOpened, loadedYAML != nil else { return }
        snippetOpened = true
        await startEditing()
        guard draft != nil else { return }
        pendingSnippet = snippet
        pickingSnippet = true
    }

    /// Opened from an Insights drift difference: a draft that undoes it, then the review.
    private func applyFixOnce() async {
        guard let fix, !fixApplied, let client = model.client, loadedYAML != nil else { return }
        fixApplied = true
        await startEditing()
        guard let current = draft else { return }
        do {
            await applyEdits(try await client.driftFixEdits(current, fix: fix))
        } catch {
            message = error.localizedDescription
            return
        }
        if canReview { reviewing = true }
    }

    /// Opened with a repository's Talos patch file: a draft with it applied, then the review. A
    /// patch that only sets values comes in as field edits, which the review can take to other
    /// nodes too; any other is applied by Talos's own patcher, for this node only, and the
    /// message says why.
    private func applyPatchOnce() async {
        guard let patch, !patchApplied, let client = model.client, loadedYAML != nil else { return }
        patchApplied = true
        await startEditing()
        guard let current = draft else { return }
        let asEdits = try? await client.talosPatchEdits(current, patch: patch.content)
        if let asEdits, asEdits.fixable {
            await applyEdits(asEdits)
            if canReview { reviewing = true }
            return
        }
        do {
            let result = try await client.talosPatchDraft(node: node, patch: patch.content)
            guard draft == current else { return }
            if result.same {
                message = String(localized: "The node already has what this patch sets.")
                return
            }
            describing?.cancel()
            draft = result.draft
            edits = []
            replayable = false
            await describe()
            if let reason = asEdits?.reason, !reason.isEmpty {
                message = String(localized: "For this node only: \(reason)")
            }
            if canReview { reviewing = true }
        } catch {
            message = error.localizedDescription
        }
    }

    /// The edits of a snippet or a fix, in order; a refusal leaves the draft as it was and says why.
    private func applyEdits(_ result: SnippetEdits) async {
        guard result.fixable else {
            message = result.reason
            return
        }
        guard let client = model.client, let current = draft else { return }
        do {
            var edited = current
            for edit in result.edits {
                edited = try await client.editMachineConfig(edited, edit: edit)
            }
            guard draft == current else { return }
            describing?.cancel()
            draft = edited
            edits.append(contentsOf: result.edits)
            message =
                result.staged
                ? String(localized: "Takes effect at the next boot: apply it staged or with a reboot.") : nil
            await describe()
        } catch {
            message = error.localizedDescription
        }
    }

    /// A draft never holds real secrets: revealed ones are hidden and the config re-fetched first.
    private func startEditing() async {
        if reveal { await setReveal(false) }
        guard !reveal, let yaml = loadedYAML else { return }
        message = nil
        draft = yaml
        edits = []
        replayable = true
    }

    /// A draft that is Git's version of this node's config (redacted): the review then shows
    /// what applying it changes.
    private func editFrom(_ git: String) async {
        await startEditing()
        guard draft != nil else { return }
        draft = git
        edits = []
        replayable = false
        await describe()
    }

    private func requestDiscard() {
        if draft == loadedYAML {
            stopEditing()
        } else {
            confirmingDiscard = true
        }
    }

    private func stopEditing() {
        describing?.cancel()
        draft = nil
        Task { await describe() }
    }

    /// After a try, whatever its result: what the node runs now is read again.
    private func finishTry() async {
        reviewing = false
        describing?.cancel()
        draft = nil
        tree = nil
        state = .loading
        await load()
    }

    /// Device-only pasteboard; revealed secrets expire from it after two minutes.
    private func copy() {
        guard let yaml = loadedYAML else { return }
        let options: [UIPasteboard.OptionsKey: Any] =
            reveal
            ? [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)]
            : [.localOnly: true]
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: yaml]], options: options)
        message =
            reveal ? String(localized: "Copied. The clipboard is cleared in 2 minutes.") : String(localized: "Copied.")
    }
}
