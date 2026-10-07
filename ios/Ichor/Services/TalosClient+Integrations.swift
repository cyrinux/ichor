import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// The API groups the cluster serves that Ichor does not read yet, by operator, with their
    /// kinds (os:admin). Built-in Kubernetes groups are left out.
    func integrations() async throws -> IntegrationReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeIntegrations(config, context, kubeServer, $0) }
    }

    /// The GitHub page opening a pre-filled integration request on repo ("owner/name") for
    /// family (only the groups picked); app names the app and platform. Only group, version
    /// and kind names go in.
    static func integrationIssueURL(repo: String, family: IntegrationFamily, app: String) async throws -> URL? {
        let json = family.json
        return URL(string: try await run { IchorgoIntegrationIssueURL(repo, json, app, $0) })
    }

    /// The GitHub search for the integration requests of repo naming family.
    static func integrationSearchURL(repo: String, family: String) -> URL? {
        URL(string: IchorgoIntegrationSearchURL(repo, family))
    }
}
