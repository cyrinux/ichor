import Foundation

// The logo the cluster selector shows for a cluster: a bundled icon (appicons, see
// scripts/sync-app-icons.py CLUSTER_ICONS). Same rules on Android (model/ClusterLogo.kt).

private let logoTalos = "talos"
private let logoKubernetes = "kubernetes"
private let logoAWS = "aws"
private let logoGCP = "google-cloud"

/// The cloud a kubeconfig context signs in to, by its sign-in method (the Go core's auth).
private let authLogos = [
    "eks": logoAWS,
    "gke": logoGCP,
    "azure": "azure",
    "digitalocean": "digital-ocean",
    "rancher": "rancher",
]

/// The cloud of a discovery provider (kubeDiscoverProviders): AKS signs in as "azure".
public func discoveryLogo(_ provider: String) -> String {
    authLogos[provider == "aks" ? "azure" : provider] ?? logoKubernetes
}

/// The managed services' API server hosts (GKE's is a bare IP).
private let hostLogos = [
    (".eks.amazonaws.com", logoAWS),
    (".azmk8s.io", "azure"),
    (".k8s.ondigitalocean.com", "digital-ocean"),
]

/// The bundled logo of `context`'s cluster: Talos for a talosconfig context, else its cloud when
/// the sign-in method, the context name (EKS ARN, gke_…) or the API server host tells, else Kubernetes.
public func clusterLogo(_ context: ContextSummary) -> String {
    guard context.isKube else { return logoTalos }
    if let logo = authLogos[context.auth ?? ""] { return logo }
    switch parseCloudContext(context.name)?.provider {
    case .some(CloudContext.eks): return logoAWS
    case .some(CloudContext.gke): return logoGCP
    default: break
    }
    var host = Substring(context.endpoints.first ?? "")
    if let scheme = host.range(of: "://") { host = host[scheme.upperBound...] }
    host = host.prefix { $0 != "/" && $0 != ":" }
    let lower = host.lowercased()
    return hostLogos.first { lower.hasSuffix($0.0) }?.1 ?? logoKubernetes
}
