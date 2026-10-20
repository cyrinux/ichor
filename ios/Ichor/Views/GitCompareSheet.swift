import SwiftUI
import IchorCore

/// The node compared with the cluster repository: the files that configure it there (the rule
/// that matched each), the ones to apply picked, and Compare applies them onto the node's live
/// config. "Same as Git", or the diff with Edit from this diff, which hands Git's version to the
/// editor (`onEditDraft`) to bring it onto the node through the review.
struct GitCompareSheet: View {
    let repo: ClusterRepo
    let node: String
    let hostname: String
    let role: String
    /// The node's config as shown (redacted), for proposing its values into the repository.
    var live: String?
    let onEditDraft: (String) -> Void

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @Environment(\.dismiss) private var dismiss
    @State private var files: LoadState<TalosRepoFiles> = .loading
    @State private var selected: Set<String> = []
    @State private var comparing = false
    @State private var compare: TalosRepoCompare?
    @State private var compareError: String?
    @State private var expanded = true

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        NavigationStack {
            LoadStateView(state: files, retry: load) { found in
                List {
                    filesSection(found)
                    if comparing {
                        Section { ProgressView().frame(maxWidth: .infinity) }
                    } else if let compareError {
                        Section { SectionError(message: compareError) }
                    } else if let compare {
                        resultSection(compare)
                    }
                }
            }
            .themedBackground()
            .navigationTitle("Node and Git")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } }
            }
        }
        .task { await load() }
    }

    @ViewBuilder
    private func filesSection(_ found: TalosRepoFiles) -> some View {
        Section {
            if found.files.isEmpty {
                Text("No file of the repository configures this node.").foregroundStyle(.secondary)
            }
            ForEach(found.files) { file in
                fileRow(file)
            }
            if found.files.contains(where: \.patch) {
                Button("Compare") { Task { await runCompare(found) } }
                    .disabled(selected.isEmpty || comparing)
            }
        } header: {
            Text("Repository at \(shortRevision(found.sha))")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if found.generator == "talhelper" || found.generator == "talosctl" {
                    Text("Laid out for \(found.generator)")
                }
                if !found.note.isEmpty { Text(verbatim: found.note) }
                if found.files.contains(where: \.patch) {
                    Text("The files checked are applied onto the node's config in this order, as your generator does.")
                }
            }
        }
    }

    private func fileRow(_ file: TalosRepoFile) -> some View {
        HStack(alignment: .top, spacing: 10) {
            Button {
                if selected.contains(file.path) { selected.remove(file.path) } else { selected.insert(file.path) }
            } label: {
                Image(systemName: file.patch && selected.contains(file.path) ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(file.patch ? Color.accentColor : .secondary)
            }
            .buttonStyle(.borderless)
            .disabled(!file.patch)
            .accessibilityLabel(Text(verbatim: file.path))
            .accessibilityValue(selected.contains(file.path) ? Text("Selected") : Text("Not selected"))
            NavigationLink {
                RepoFileView(repo: repo, path: file.file, line: file.line)
            } label: {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Tag(kindLabel(file.kind))
                        Text(verbatim: file.path).font(.callout.monospaced()).lineLimit(2)
                    }
                    Text(verbatim: file.why).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .opacity(file.patch || file.kind == .talconfig ? 1 : 0.6)
    }

    @ViewBuilder
    private func resultSection(_ result: TalosRepoCompare) -> some View {
        Section {
            ForEach(result.skipped, id: \.path) { skip in
                SectionError(message: String(localized: "\(skip.path) skipped: \(skip.why)"))
            }
            if result.same {
                StatusPill(label: String(localized: "Same as Git"), color: .statusOK)
                Text("Applying these files onto the node's config changes nothing: the node matches the repository.")
                    .font(.callout).foregroundStyle(.secondary)
            } else {
                if result.needsReboot {
                    StatusPill(label: String(localized: "Needs a reboot"), color: .statusWarn)
                }
                ForEach(result.diff.resources) { r in
                    DiffResourceRow(resource: r, expanded: $expanded)
                }
                Button("Edit from this diff") {
                    onEditDraft(result.draft)
                    dismiss()
                }
                // The node's live values into Git: from Git's version to the node's.
                if let live {
                    ProposeInGitButton(base: result.draft, draft: live, hostname: hostname, label: "Propose the node's values in Git")
                }
            }
        } header: {
            if !result.same { Text("What Git would change on the node") }
        } footer: {
            if !result.same { Text("Opens the editor with Git's version as the draft: review it, then try or apply it.") }
        }
    }

    private func kindLabel(_ kind: TalosRepoFileKind) -> String {
        switch kind {
        case .node: String(localized: "Node")
        case .shared: String(localized: "Shared")
        case .talconfig: "talconfig"
        case .secrets: String(localized: "Secrets")
        }
    }

    private func load() async {
        do {
            let found = try await TalosClient.talosRepoFiles(repo, hostname: hostname, role: role, hosts: hosts, demo: demo)
            selected = found.defaultSelection
            files = .loaded(found, at: Date())
        } catch {
            files = .failed(error.localizedDescription)
        }
    }

    private func runCompare(_ found: TalosRepoFiles) async {
        guard let client = model.client else { return }
        comparing = true
        compareError = nil
        defer { comparing = false }
        do {
            compare = try await client.talosRepoCompare(repo, node: node, hostname: hostname, sha: found.sha,
                                                        paths: found.ordered(selected), hosts: hosts, demo: demo)
        } catch {
            compare = nil
            compareError = error.localizedDescription
        }
    }
}

/// A file of the cluster repository at its branch head, opened at a line.
struct RepoFileView: View {
    let repo: ClusterRepo
    let path: String
    let line: Int

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @State private var state: LoadState<GitSource> = .loading

    var body: some View {
        Group {
            if case .loaded(let source, _, _) = state {
                SourceFileView(source: source, path: path, app: .repository, line: line > 0 ? line : nil)
            } else {
                LoadStateView(state: state, retry: load) { _ in EmptyView() }
            }
        }
        .task { await load() }
    }

    private func load() async {
        do {
            let info = try await TalosClient.repoInfo(repo, hosts: hosts, demo: model.activeSummary?.demo == true)
            state = .loaded(repoSource(repo, info: info), at: Date())
        } catch {
            state = .failed(error.localizedDescription)
        }
    }
}
