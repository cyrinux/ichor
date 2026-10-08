import SwiftUI
import IchorCore

/// The icon and the name of each screen a toolbar leads to, once for the three bars (the Talos
/// overview's, the Kubernetes home's and the Kubernetes screen's; Android's Destination): an
/// action looks the same wherever it is offered, and the tools card borrows them.
extension HomeDestination {
    var systemImage: String {
        switch self {
        case .health: "heart.text.square"
        case .events: "list.bullet.rectangle"
        case .workloads: "square.stack.3d.up"
        case .metrics: "chart.xyaxis.line"
        case .kubespan: "point.3.connected.trianglepath.dotted"
        case .etcd: "cylinder.split.1x2"
        case .settings: "gearshape"
        case .resources: "square.grid.3x3"
        case .helm: "shippingbox"
        case .dataServices: "externaldrive.connected.to.line.below"
        case .checkup: "stethoscope"
        case .apiHealth: "heart.text.square"
        case .networkPolicies: "shield.lefthalf.filled"
        case .share: "link"
        case .flows: "point.3.filled.connected.trianglepath.dotted"
        case .apiAddress: "server.rack"
        }
    }

    var title: Text {
        switch self {
        case .health: Text("Cluster health")
        case .events: Text("Events")
        case .workloads: Text("Kubernetes workloads")
        case .metrics: Text("Metrics")
        case .kubespan: Text(verbatim: "KubeSpan")
        case .etcd: Text(verbatim: "etcd")
        case .settings: Text("Settings")
        case .resources: Text("Resources")
        case .helm: Text("Helm releases")
        case .dataServices: Text("Data services")
        case .checkup: Text(CheckupText.checkupTitle)
        case .apiHealth: Text("API server")
        case .networkPolicies: Text("Network policies")
        case .share: Text("Share link")
        case .flows: Text("Live flows")
        case .apiAddress: Text("Kubernetes API address")
        }
    }
}

extension OverviewAction: BarActionLook {
    var systemImage: String { destination.systemImage }
    var title: Text { destination.title }
}

extension KubeHomeAction: BarActionLook {
    var systemImage: String { destination.systemImage }
    var title: Text { destination.title }
}

extension KubernetesAction: BarActionLook {
    var systemImage: String { destination.systemImage }
    var title: Text { destination.title }
}
