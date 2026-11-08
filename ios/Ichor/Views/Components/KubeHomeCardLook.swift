import SwiftUI
import IchorCore

/// How the Kubernetes home's sections are named in the editor.
extension KubeHomeCard: CardLook {
    var title: Text {
        switch self {
        case .pinned: Text("Pinned")
        case .summary: Text("Cluster summary")
        case .apps: Text("Apps")
        case .nodes: Text("Nodes")
        case .tools: Text("Kubernetes screens")
        case .dataServices: Text("Data services")
        case .argoCD: Text(verbatim: "Argo CD")
        case .flux: Text(verbatim: "Flux")
        case .alerts: Text("Alerts")
        }
    }

    /// One line on what the section shows, so the editor's names need no guessing.
    var detail: Text {
        switch self {
        case .pinned: Text("Nodes, workloads and GitOps apps pinned from their screens")
        case .summary: Text("API server, Kubernetes version, nodes ready and who the credentials are")
        case .apps: Text("Apps running on the cluster and those that need a look")
        case .nodes: Text("Every node as Kubernetes lists it, with cordons and pressure")
        case .tools: Text("Buttons to the Kubernetes screens these credentials can use")
        case .dataServices: Text("Health of Longhorn, Garage and CloudNativePG")
        case .argoCD: Text("Sync and health of the Argo CD apps, syncs in progress")
        case .flux: Text("Ready, reconciling and failing Kustomizations and HelmReleases")
        case .alerts: Text("Alertmanager alerts firing by severity, and those silenced")
        }
    }
}
