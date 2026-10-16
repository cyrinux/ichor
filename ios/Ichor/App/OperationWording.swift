import Foundation
import IchorCore

/// How the runs' steps are worded, on their screens and in their Live Activity.
extension ClusterUpgradeProgress {
    /// What the roll does now: the node's own upgrade phase, the gate, or the pause.
    var localizedPhase: String {
        switch rollPhase {
        case .gate: String(localized: "checking the cluster's health")
        case .paused: String(localized: "paused")
        case .done: String(localized: "Done")
        case .node: nodePhase.isEmpty ? String(localized: "Requested") : nodePhase
        }
    }
}

extension K8sUpgradeProgress {
    /// "Kubernetes upgrade 4/9: cp-2 kube-scheduler", or what it does between nodes.
    var localizedStep: String {
        switch phase {
        case "done": String(localized: "Done")
        case "proxy": String(localized: "kube-proxy DaemonSet")
        default:
            total > 0
                ? String(localized: "Kubernetes upgrade \(min(index + 1, total))/\(total): \(name) \(component.isEmpty ? phase : component)")
                : message
        }
    }
}

/// The scan's phase in words, on its screen and in its Live Activity.
func imageScanPhaseText(_ p: ImageScanProgress?, scanner: String) -> String {
    switch p?.phase {
    case ImageScanPhase.starting:
        guard let p, !p.message.isEmpty else { return String(localized: "Starting \(scanner)…") }
        return String(localized: "Starting \(scanner) (\(p.message))…")
    case ImageScanPhase.database:
        return String(localized: "Downloading the vulnerability database…")
    case ImageScanPhase.scanning:
        let p = p ?? ImageScanProgress()
        let image = String(p.image.split(separator: "/").last ?? "")
        return String(localized: "Scanning \(String(p.step)) of \(String(p.steps)): \(image)")
    case ImageScanPhase.cleaning:
        return String(localized: "Cleaning up…")
    default:
        return String(localized: "Reading the pods and their images…")
    }
}

extension OperationKind {
    /// What the Lock Screen calls the run with the app lock on: no node, version or cluster.
    var genericTitle: String {
        switch self {
        case .upgrade: String(localized: "Talos upgrade")
        case .maintenance: String(localized: "Node maintenance")
        case .configTry: String(localized: "Config try")
        case .configMulti: String(localized: "Config rollout")
        case .clusterUpgrade: String(localized: "Cluster upgrade")
        case .k8sUpgrade: String(localized: "Kubernetes upgrade")
        case .imagePull: String(localized: "Image pull")
        case .join: String(localized: "Node join")
        case .imageScan: String(localized: "Image scan")
        case .argoSync: String(localized: "Argo CD sync")
        case .fluxReconcile: String(localized: "Flux reconcile")
        case .rollout: String(localized: "Rollout restart")
        case .netPerf: String(localized: "Network test")
        }
    }
}
