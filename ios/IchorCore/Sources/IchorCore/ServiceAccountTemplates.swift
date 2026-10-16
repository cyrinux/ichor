import Foundation

/// A least-privilege ServiceAccount for the phone, listed by the Go core (both apps show the
/// same): the `yaml` to apply with `apply`, then `kubeconfig`, the commands that turn its token
/// into a kubeconfig.
public struct ServiceAccountTemplate: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let yaml: String
    public let apply: String
    public let kubeconfig: String

    public init(id: String, yaml: String, apply: String, kubeconfig: String) {
        self.id = id
        self.yaml = yaml
        self.apply = apply
        self.kubeconfig = kubeconfig
    }
}

/// The templates the help sheet knows, by id: an unknown one (from a newer core) is left out,
/// since the sheet has no title for it.
public enum ServiceAccountRole: String, CaseIterable, Sendable {
    case read, operate, namespace
}

/// The templates in the core's order, each with its role; unknown ids dropped.
public func knownServiceAccountTemplates(_ templates: [ServiceAccountTemplate]) -> [(role: ServiceAccountRole, template: ServiceAccountTemplate)] {
    templates.compactMap { template in ServiceAccountRole(rawValue: template.id).map { ($0, template) } }
}

extension ContextSummary {
    /// A kubeconfig cluster added with a bearer token (no sign-in method), outside the demo:
    /// the one the ServiceAccount help is for.
    public var usesKubeToken: Bool { isKube && !demo && auth == "token" && signIn == nil }
}
