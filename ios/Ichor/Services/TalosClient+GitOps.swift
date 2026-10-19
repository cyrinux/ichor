import Foundation
import Ichorgo
import IchorCore

/// No token is saved for the Git host of a source: the browser offers to add one.
struct GitHostMissing: LocalizedError {
    let host: String
    var errorDescription: String? { String(localized: "Add a token for \(host) to browse this repository.") }
}

/// The files an Argo CD or Flux app deploys (see TalosClient for the conventions): a Flux
/// source's from the source-controller's artifact (no token), any other Git source's through
/// its Git host with the token saved in Settings (GitHostStore), the demo's without network.
extension TalosClient {
    /// Where the files of the app are (os:admin): tool "argocd" (kind Application) or "flux"
    /// (Kustomization or HelmRelease).
    func gitOpsSource(tool: String, kind: String, namespace: String, name: String) async throws -> GitOpsSource {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoGitOpsSource(config, context, kubeServer, tool, kind, namespace, name, $0)
        }
    }

    /// The directory dir ("" for the root) of source, at the commit the cluster runs.
    func sourceTree(_ source: GitSource, dir: String, hosts: GitHostStore, demo: Bool) async throws -> GitTree {
        switch source.read {
        case .flux(let ref):
            return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
                IchorgoFluxSourceTree(config, context, kubeServer, ref.kind, ref.namespace, ref.name, dir, $0)
            }
        case .forge(let host, let ref):
            let json = try await Self.forgeHost(host, hosts: hosts, demo: demo)
            let repo = source.repoURL
            return try await Self.json { IchorgoGitForgeTree(json, repo, ref, dir, $0) }
        case .unreadable:
            throw TalosError(message: String(localized: "This source is not a Git repository the app can read."))
        }
    }

    /// The file path (from the repository root) of source, at the commit the cluster runs.
    func sourceFile(_ source: GitSource, path: String, hosts: GitHostStore, demo: Bool) async throws -> GitFile {
        switch source.read {
        case .flux(let ref):
            return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
                IchorgoFluxSourceFile(config, context, kubeServer, ref.kind, ref.namespace, ref.name, path, $0)
            }
        case .forge(let host, let ref):
            let json = try await Self.forgeHost(host, hosts: hosts, demo: demo)
            let repo = source.repoURL
            return try await Self.json { IchorgoGitForgeFile(json, repo, ref, path, $0) }
        case .unreadable:
            throw TalosError(message: String(localized: "This source is not a Git repository the app can read."))
        }
    }

    /// The lines of source's whole repository that hold query (case-insensitive): from the Flux
    /// artifact (no token), else from a snapshot of the repository at the source's revision,
    /// downloaded once per commit (up to 48 MiB) with the Git host's token (GitHostMissing
    /// without one).
    func repoSearch(_ source: GitSource, query: String, hosts: GitHostStore, demo: Bool) async throws -> GitRepoSearch {
        switch source.read {
        case .flux(let ref):
            return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
                IchorgoFluxSourceSearch(config, context, kubeServer, ref.kind, ref.namespace, ref.name, query, 0, $0)
            }
        case .forge(let host, let ref):
            let json = try await Self.forgeHost(host, hosts: hosts, demo: demo)
            let repo = source.repoURL
            return try await Self.json { IchorgoGitRepoSearch(json, repo, ref, query, 0, $0) }
        case .unreadable:
            throw TalosError(message: String(localized: "This source is not a Git repository the app can read."))
        }
    }

    /// The files of app's source number sourceIndex (in GitOpsSource.sources) that declare the
    /// resource resKind resNamespace/resName, with the line of its metadata.name: from the
    /// Flux artifact (no token), else through the Git host with its token (GitHostMissing
    /// without one).
    func findSource(app: GitOpsAppRef, source: GitSource, sourceIndex: Int, resKind: String, resNamespace: String,
                    resName: String, hosts: GitHostStore, demo: Bool) async throws -> GitOpsSourceFind {
        let json: String
        switch source.read {
        case .flux:
            json = ""
        case .forge(let host, _):
            json = try await Self.forgeHost(host, hosts: hosts, demo: demo)
        case .unreadable:
            throw TalosError(message: String(localized: "This source is not a Git repository the app can read."))
        }
        let index = String(sourceIndex)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoGitOpsSourceFind(config, context, kubeServer, json, app.tool, app.kind, app.namespace, app.name,
                                    index, resKind, resNamespace, resName, $0)
        }
    }

    /// The file path of source as its Git host has it at ref (a branch, or what the app
    /// follows), with the sha a proposal names: what the editor starts from, also for a Flux
    /// source read from its artifact. Throws GitHostMissing without a token.
    func forgeFile(_ source: GitSource, path: String, ref: String, hosts: GitHostStore, demo: Bool) async throws -> GitFile {
        let json = try await Self.proposalHost(source, hosts: hosts, demo: demo)
        let repo = source.repoURL
        return try await Self.json { IchorgoGitForgeFile(json, repo, ref, path, $0) }
    }

    /// Commits the proposal's file on a new branch and opens a pull (merge) request, with the
    /// token saved for source's Git host. Recorded in the audit log; the demo and screenshot
    /// mode refuse it (the Go core's message).
    func propose(_ proposal: GitProposal, on source: GitSource, hosts: GitHostStore, demo: Bool) async throws -> GitProposalResult {
        let json = try await Self.proposalHost(source, hosts: hosts, demo: demo)
        let options = try proposal.optionsJSON()
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext] in
            IchorgoGitForgePropose(config, context, json, options, $0)
        }
    }

    /// The unified diff of a file from before to after (one "file" resource), for the review.
    static func textDiff(before: String, after: String, name: String) async throws -> FluxDiff {
        try await json { IchorgoTextDiff(before, after, name, $0) }
    }

    /// The images and replicas content sets, for the quick edits (pure, no cluster).
    static func quickEditTargets(_ content: String) async throws -> QuickEditTargets {
        try await json { IchorgoGitOpsQuickEditTargets(content, $0) }
    }

    /// content with edit applied, comments and formatting kept (pure, no cluster).
    static func quickEdit(_ content: String, _ edit: QuickEdit) async throws -> QuickEditResult {
        let editJSON = try edit.json()
        return try await json { IchorgoGitOpsQuickEdit(content, editJSON, $0) }
    }

    /// The prefilled pull request for a change of path deployed by app, on cluster ("" for none).
    static func proposalDefaults(app: GitOpsAppRef, path: String, cluster: String) async throws -> GitProposalDefaults {
        try await json { IchorgoGitOpsProposalDefaults(app.tool, app.kind, app.namespace, app.name, path, cluster, $0) }
    }

    /// What the forge tells of repo with its host's saved token: the default branch, the head
    /// of the branch read, whether the token can push. Throws GitHostMissing without a token.
    static func repoInfo(_ repo: ClusterRepo, hosts: GitHostStore, demo: Bool) async throws -> GitRepoInfo {
        let json = try await forgeHost(repo.host, hosts: hosts, demo: demo)
        let url = repo.repoURL
        let branch = repo.branch
        return try await Self.json { IchorgoGitForgeRepo(json, url, branch, $0) }
    }

    /// The files of repo (under its Talos directory) that configure the node hostname of role,
    /// at its branch head. Throws GitHostMissing without a token.
    static func talosRepoFiles(_ repo: ClusterRepo, hostname: String, role: String, hosts: GitHostStore, demo: Bool) async throws -> TalosRepoFiles {
        let json = try await forgeHost(repo.host, hosts: hosts, demo: demo)
        let url = repo.repoURL, branch = repo.branch, dir = repo.talosDir
        return try await Self.json { IchorgoTalosRepoFiles(json, url, branch, dir, hostname, role, $0) }
    }

    /// Applies repo's files paths (in that order) at commit sha onto node's live config and says
    /// what differs (os:admin; only a dry run reaches the node).
    func talosRepoCompare(_ repo: ClusterRepo, node: String, hostname: String, sha: String, paths: [String],
                          hosts: GitHostStore, demo: Bool) async throws -> TalosRepoCompare {
        let json = try await Self.forgeHost(repo.host, hosts: hosts, demo: demo)
        let pathsJSON = (try? TalosJSON.encode(paths)) ?? "[]"
        let url = repo.repoURL
        return try await Self.json { [config, context] in
            IchorgoTalosRepoCompare(config, context, json, url, sha, node, hostname, pathsJSON, $0)
        }
    }

    /// The prefilled pull request for a change of path in the cluster repository repoURL, on
    /// cluster ("" for none).
    static func repoProposalDefaults(repoURL: String, path: String, cluster: String) async throws -> GitProposalDefaults {
        try await json { IchorgoGitForgeRepoProposalDefaults(repoURL, path, cluster, $0) }
    }

    /// The host argument of the forge a change of source is proposed through.
    private static func proposalHost(_ source: GitSource, hosts: GitHostStore, demo: Bool) async throws -> String {
        guard let host = source.proposalHost else {
            throw TalosError(message: String(localized: "This file cannot be edited here: open it in the browser."))
        }
        return try await forgeHost(host, hosts: hosts, demo: demo)
    }

    /// The host argument with the token saved for host; the demo's needs none.
    private static func forgeHost(_ host: String, hosts: GitHostStore, demo: Bool) async throws -> String {
        if demo { return gitHostDemoJSON }
        guard let json = await hosts.hostJSON(for: host) else { throw GitHostMissing(host: host) }
        return json
    }
}
