import Foundation
import Ichorgo
import IchorCore

/// The cluster's update candidates (see TalosClient for the conventions): its images and charts
/// with their newer versions, asked of the registries, and where one is edited in Git.
extension TalosClient {
    /// The images and charts with their newer versions (os:admin, read only): the cluster first,
    /// then the registries (10 to 20 s for 100 images the first time). With the saved cluster
    /// repository and its token, a dependency bot found there marks what it bumps.
    func clusterUpdates(repo: ClusterRepo?, hosts: GitHostStore, demo: Bool) async throws -> ClusterUpdates {
        let (host, options) = await Self.updatesRepoArgs(repo, hosts: hosts, demo: demo)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoClusterUpdates(config, context, kubeServer, host, options, $0)
        }
    }

    /// Where row is edited in Git for version (UpdateEditTarget), read only.
    func updateEditTarget(_ row: UpdateRow, version: String, repo: ClusterRepo?, hosts: GitHostStore, demo: Bool) async throws -> UpdateEditTarget {
        let (host, _) = await Self.updatesRepoArgs(repo, hosts: hosts, demo: demo)
        let item = updateItemJSON(row, version: version)
        return try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoUpdateEditTarget(config, context, kubeServer, host, item, $0)
        }
    }

    /// The saved cluster repository's host (with its token) and ClusterUpdates' options naming
    /// it; blank without them (the lookups then read no repository).
    private static func updatesRepoArgs(_ repo: ClusterRepo?, hosts: GitHostStore, demo: Bool) async -> (String, String) {
        guard let repo, !demo, let host = await hosts.hostJSON(for: repo.host) else { return ("", "") }
        let options = (try? TalosJSON.encode(["repoURL": repo.repoURL, "ref": repo.branch])) ?? ""
        return (host, options)
    }
}
