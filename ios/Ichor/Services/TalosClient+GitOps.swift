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

    /// The prefilled pull request for a change of path deployed by app, on cluster ("" for none).
    static func proposalDefaults(app: GitOpsAppRef, path: String, cluster: String) async throws -> GitProposalDefaults {
        try await json { IchorgoGitOpsProposalDefaults(app.tool, app.kind, app.namespace, app.name, path, cluster, $0) }
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
