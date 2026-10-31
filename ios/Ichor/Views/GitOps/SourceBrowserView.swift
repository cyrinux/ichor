import SwiftUI
import IchorCore

/// A directory of an Argo CD or Flux app's source, at the commit the cluster runs: the
/// repository and what it follows, a breadcrumb up to the root, then the folders and the files
/// (a folder opens its own page, a file SourceFileView). Opens at the directory the app
/// deploys; a Flux artifact holds the whole repository, so the breadcrumb goes up from there.
/// Without a token for an Argo CD source's Git host, says so with a way to add one. In the
/// cluster repository, files staged from the editor carry a dot and Staged changes opens them
/// as one pull request.
struct SourceBrowserView: View {
    let source: GitSource
    /// The app deploying it, for the defaults of a pull request opened from a file.
    let app: GitOpsAppRef

    // Explicit: the private @State properties make the memberwise init private.
    init(source: GitSource, dir: String, app: GitOpsAppRef) {
        self.source = source
        self.app = app
        _dir = State(initialValue: cleanSourcePath(dir))
    }

    @Environment(AppModel.self) private var model
    @Environment(GitHostStore.self) private var hosts
    @Environment(StagedEdits.self) private var staged
    @State private var dir: String
    @State private var showingStaged = false
    @State private var state: LoadState<GitTree> = .loading
    /// The directory state shows, so a breadcrumb jump shows the skeleton rather than the old one.
    @State private var loadedDir: String?
    /// The Git host has no token saved.
    @State private var missingHost: String?
    /// Find in repository: the text typed, and the answer for the text submitted (nil: the listing).
    @State private var query = ""
    @State private var searched: LoadState<GitRepoSearch>?
    @State private var searchedText = ""

    private var demo: Bool { model.activeSummary?.demo == true }

    /// The files staged for one pull request: the cluster repository's only.
    private var stagedFiles: [StagedFile] { app.isRepository ? staged.of(StagedKey(source: source)) : [] }

    var body: some View {
        Group {
            if let missingHost {
                GitHostMissingView(host: missingHost, page: source.webURL)
                    .themedBackground()
            } else if let searched {
                searchResults(searched)
            } else {
                LoadStateView(state: state, retry: load) { tree in
                    content(tree).accessibilityIdentifier(SmokeID.screen("gitops"))
                }
            }
        }
        .modifier(
            SourceSearchable(enabled: source.read != .unreadable, query: $query) {
                Task { await search() }
            }
        )
        .onChange(of: query) { _, text in
            if text.isEmpty { searched = nil }
        }
        .task(id: dir) { await load() }
        // A token added in Settings (the shortcut below): read again on the way back.
        .onChange(of: hosts.hosts) { _, _ in Task { await load() } }
        .sheet(isPresented: $showingStaged) { StagedChangesView(source: source) }
        .navigationTitle(app.isRepository ? String(localized: "Repository") : String(localized: "Source"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if let url = hosts.browserURL(
                    gitWebURL(source.webURL, kind: hosts.kind(of: source.host), ref: source.ref, path: dir, dir: true))
                {
                    Link(destination: url) { Label("Open in browser", systemImage: "safari") }
                }
            }
        }
    }

    private func content(_ tree: GitTree) -> some View {
        let entries = tree.sorted
        let stagedPaths = Set(stagedFiles.map(\.path))
        return List {
            Section {
                SourceHeader(source: source, sha: tree.sha)
                breadcrumb
            }
            if !stagedPaths.isEmpty {
                Section {
                    Button {
                        showingStaged = true
                    } label: {
                        Label("Staged changes (\(String(stagedPaths.count)))", systemImage: "square.and.pencil")
                    }
                }
            }
            // The cluster repository's root lists the pull requests opened from the phone on it.
            if app.isRepository && dir.isEmpty {
                ProposalsSection(app: .repository, refresh: tree.sha)
            }
            Section {
                if entries.isEmpty {
                    Text("This folder is empty.").foregroundStyle(.secondary)
                }
                ForEach(entries) { entry in
                    if entry.isDir {
                        NavigationLink {
                            SourceBrowserView(source: source, dir: entry.path, app: app)
                        } label: {
                            Label {
                                Text(verbatim: entry.name).font(.callout.monospaced())
                            } icon: {
                                Image(systemName: "folder").foregroundStyle(.tint)
                            }
                        }
                    } else {
                        NavigationLink {
                            SourceFileView(source: source, path: entry.path, app: app)
                        } label: {
                            HStack {
                                Label {
                                    Text(verbatim: entry.name).font(.callout.monospaced()).lineLimit(1).truncationMode(
                                        .middle)
                                } icon: {
                                    Image(systemName: "doc.text").foregroundStyle(.secondary)
                                }
                                Spacer(minLength: 8)
                                if stagedPaths.contains(entry.path) {
                                    Circle().fill(.tint).frame(width: 8, height: 8)
                                        .accessibilityLabel(Text("Staged"))
                                }
                                if entry.sops {
                                    Image(systemName: "lock.fill").font(.caption).foregroundStyle(.secondary)
                                        .accessibilityLabel(Text("Encrypted with SOPS"))
                                }
                                Text(verbatim: formatBytes(entry.size)).font(.caption.monospacedDigit())
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    /// The root, then each directory down to this one; every step but the last jumps there.
    private var breadcrumb: some View {
        let crumbs = sourceBreadcrumb(dir)
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                crumb(Text("Repository root"), path: "", current: crumbs.isEmpty)
                ForEach(crumbs, id: \.path) { step in
                    Image(systemName: "chevron.right").font(.caption2).foregroundStyle(.tertiary).accessibilityHidden(
                        true)
                    crumb(Text(verbatim: step.name), path: step.path, current: step.path == crumbs.last?.path)
                }
            }
            .font(.caption.monospaced())
        }
    }

    @ViewBuilder private func crumb(_ label: Text, path: String, current: Bool) -> some View {
        if current {
            label.fontWeight(.semibold).accessibilityAddTraits(.isHeader)
        } else {
            Button {
                dir = path
            } label: {
                label
            }.buttonStyle(.borderless)
        }
    }

    @ViewBuilder
    private func searchResults(_ state: LoadState<GitRepoSearch>) -> some View {
        switch state {
        case .loading:
            VStack(spacing: 12) {
                ProgressView()
                Text("Searching… The first search of a commit downloads the repository (up to 48 MiB).")
                    .font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .themedBackground()
        case .failed(let message):
            ContentUnavailableView {
                Label("Search failed", systemImage: "exclamationmark.magnifyingglass")
            } description: {
                Text(verbatim: message)
            } actions: {
                Button("Retry") { Task { await search() } }
            }
            .themedBackground()
        case .loaded(let result, _, _):
            if result.hits.isEmpty {
                ContentUnavailableView.search(text: searchedText).themedBackground()
            } else {
                List {
                    Section {
                        ForEach(result.byFile, id: \.path) { group in
                            ForEach(group.hits) { hit in
                                NavigationLink {
                                    SourceFileView(source: source, path: hit.path, app: app, line: hit.line)
                                } label: {
                                    SourceHitRow(hit: hit)
                                }
                            }
                        }
                    } footer: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("Matches: \(result.hits.count) · files searched: \(result.searched)")
                            if result.truncated { Text("More lines match: refine the search.") }
                        }
                    }
                }
                .themedBackground()
            }
        }
    }

    /// Finds the text typed across the whole repository.
    private func search() async {
        let text = query.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty, let client = model.client else { return }
        searchedText = text
        searched = .loading
        do {
            let result = try await client.repoSearch(source, query: text, hosts: hosts, demo: demo)
            guard searchedText == text else { return }
            searched = .loaded(result, at: Date())
        } catch {
            guard searchedText == text else { return }
            searched = .failed(error.localizedDescription)
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let wanted = dir
        if loadedDir != wanted { state = .loading }
        do {
            let tree = try await client.sourceTree(source, dir: wanted, hosts: hosts, demo: demo)
            guard wanted == dir else { return }
            missingHost = nil
            loadedDir = wanted
            state = .loaded(tree, at: Date())
        } catch let missing as GitHostMissing {
            missingHost = missing.host
        } catch {
            guard wanted == dir else { return }
            missingHost = nil
            state = state.refreshed(with: .failed(error.localizedDescription))
        }
    }
}

/// One Find in repository hit: its file and line, then the line's text.
private struct SourceHitRow: View {
    let hit: GitSearchHit

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: "\(hit.path):\(hit.line)")
                .font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            Text(verbatim: hit.text).font(.callout.monospaced()).lineLimit(2)
        }
        .accessibilityElement(children: .combine)
    }
}

/// .searchable for the sources whose whole repository can be read, nothing for the others.
private struct SourceSearchable: ViewModifier {
    let enabled: Bool
    @Binding var query: String
    let submit: () -> Void

    func body(content: Content) -> some View {
        if enabled {
            content
                .searchable(
                    text: $query, placement: .navigationBarDrawer(displayMode: .always),
                    prompt: Text("Find in repository")
                )
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .onSubmit(of: .search, submit)
        } else {
            content
        }
    }
}

/// The repository (its page when it has one), what the app follows and the commit read.
struct SourceHeader: View {
    let source: GitSource
    /// The commit the host read, "" when it did not say (the source's revision then).
    let sha: String
    @Environment(GitHostStore.self) private var hosts

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let url = hosts.browserURL(source.webURL) {
                Link(destination: url) {
                    Label {
                        Text(verbatim: source.repoURL).lineLimit(2).truncationMode(.middle)
                    } icon: {
                        Image(systemName: "arrow.up.forward.square")
                    }
                    .font(.callout.monospaced())
                }
            } else {
                Text(verbatim: source.repoURL).font(.callout.monospaced()).lineLimit(2).truncationMode(.middle)
                    .textSelection(.enabled)
            }
            let commit = shortRevision(sha.isEmpty ? source.revision : sha)
            HStack(spacing: 8) {
                if !source.tracked.isEmpty { Text("Follows \(source.tracked)") }
                if !commit.isEmpty { Text(verbatim: commit).font(.caption.monospaced()) }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }
}

/// No token is saved for the Git host: a shortcut to Settings → Git hosts, and the repository's
/// page in the browser.
struct GitHostMissingView: View {
    let host: String
    /// The repository's page, "" without one.
    let page: String
    @Environment(GitHostStore.self) private var hosts

    var body: some View {
        ContentUnavailableView {
            Label {
                Text(verbatim: host)
            } icon: {
                Image(systemName: "key")
            }
        } description: {
            Text("Add a token for \(host) to browse this repository.")
        } actions: {
            NavigationLink {
                SettingsPageView(page: .gitHosts)
            } label: {
                Text("Add a token")
            }
            .buttonStyle(.borderedProminent)
            if let url = hosts.browserURL(page) {
                Link(destination: url) { Text("Open in browser") }
            }
        }
    }
}
