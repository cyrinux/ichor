import SwiftUI
import IchorCore

/// A directory of an Argo CD or Flux app's source, at the commit the cluster runs: the
/// repository and what it follows, a breadcrumb up to the root, then the folders and the files
/// (a folder opens its own page, a file SourceFileView). Opens at the directory the app
/// deploys; a Flux artifact holds the whole repository, so the breadcrumb goes up from there.
/// Without a token for an Argo CD source's Git host, says so with a way to add one.
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
    @State private var dir: String
    @State private var state: LoadState<GitTree> = .loading
    /// The directory state shows, so a breadcrumb jump shows the skeleton rather than the old one.
    @State private var loadedDir: String?
    /// The Git host has no token saved.
    @State private var missingHost: String?

    private var demo: Bool { model.activeSummary?.demo == true }

    var body: some View {
        Group {
            if let missingHost {
                GitHostMissingView(host: missingHost, page: source.webURL)
                    .themedBackground()
            } else {
                LoadStateView(state: state, retry: load) { tree in content(tree) }
            }
        }
        .task(id: dir) { await load() }
        // A token added in Settings (the shortcut below): read again on the way back.
        .onChange(of: hosts.hosts) { _, _ in Task { await load() } }
        .navigationTitle("Source")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                if let url = webURL(gitWebURL(source.webURL, kind: hosts.kind(of: source.host), ref: source.ref, path: dir, dir: true)) {
                    Link(destination: url) { Label("Open in browser", systemImage: "safari") }
                }
            }
        }
    }

    private func content(_ tree: GitTree) -> some View {
        let entries = tree.sorted
        return List {
            Section {
                SourceHeader(source: source, sha: tree.sha)
                breadcrumb
            }
            Section {
                if entries.isEmpty {
                    Text("This folder is empty.").foregroundStyle(.secondary)
                }
                ForEach(entries) { entry in
                    if entry.isDir {
                        NavigationLink { SourceBrowserView(source: source, dir: entry.path, app: app) } label: {
                            Label { Text(verbatim: entry.name).font(.callout.monospaced()) } icon: {
                                Image(systemName: "folder").foregroundStyle(.tint)
                            }
                        }
                    } else {
                        NavigationLink { SourceFileView(source: source, path: entry.path, app: app) } label: {
                            HStack {
                                Label { Text(verbatim: entry.name).font(.callout.monospaced()).lineLimit(1).truncationMode(.middle) } icon: {
                                    Image(systemName: "doc.text").foregroundStyle(.secondary)
                                }
                                Spacer(minLength: 8)
                                Text(verbatim: formatBytes(entry.size)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
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
                    Image(systemName: "chevron.right").font(.caption2).foregroundStyle(.tertiary).accessibilityHidden(true)
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
            Button { dir = path } label: { label }.buttonStyle(.borderless)
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

/// The repository (its page when it has one), what the app follows and the commit read.
struct SourceHeader: View {
    let source: GitSource
    /// The commit the host read, "" when it did not say (the source's revision then).
    let sha: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let url = webURL(source.webURL) {
                Link(destination: url) {
                    Label { Text(verbatim: source.repoURL).lineLimit(2).truncationMode(.middle) } icon: {
                        Image(systemName: "arrow.up.forward.square")
                    }
                    .font(.callout.monospaced())
                }
            } else {
                Text(verbatim: source.repoURL).font(.callout.monospaced()).lineLimit(2).truncationMode(.middle).textSelection(.enabled)
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

    var body: some View {
        ContentUnavailableView {
            Label { Text(verbatim: host) } icon: { Image(systemName: "key") }
        } description: {
            Text("Add a token for \(host) to browse this repository.")
        } actions: {
            NavigationLink { SettingsPageView(page: .gitHosts) } label: { Text("Add a token") }
                .buttonStyle(.borderedProminent)
            if let url = webURL(page) {
                Link(destination: url) { Text("Open in browser") }
            }
        }
    }
}
