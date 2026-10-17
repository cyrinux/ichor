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

    /// The host argument with the token saved for host; the demo's needs none.
    private static func forgeHost(_ host: String, hosts: GitHostStore, demo: Bool) async throws -> String {
        if demo { return gitHostDemoJSON }
        guard let json = await hosts.hostJSON(for: host) else { throw GitHostMissing(host: host) }
        return json
    }
}
