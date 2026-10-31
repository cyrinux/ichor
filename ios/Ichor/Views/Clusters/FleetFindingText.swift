import Foundation
import IchorCore

extension FleetFinding {
    /// "Alert: KubePodCrashLooping · shop/checkout": the track in the app's language, the subject
    /// as the core wrote it; in screenshot mode (`masked`) the track only, but for the subjects
    /// that name no user object. The fleet screen's rows and the fleet widget's.
    func text(masked: Bool) -> String {
        let track = trackLabel
        guard subjectShown(masked: masked) else { return track }
        return String(format: String(localized: "%@: %@"), track, subject)
    }

    private var trackLabel: String {
        switch track {
        case "nodes": String(localized: "Nodes")
        case "reach": String(localized: "Connection")
        case "etcd": "etcd"
        case "alertmanager": String(localized: "Alert")
        case "checkup": String(localized: "Cluster checkup")
        case "data": String(localized: "Data services")
        case "gitops": detail == "argocd" ? "Argo CD" : detail == "flux" ? "Flux" : "GitOps"
        case "storage": String(localized: "Storage")
        case "sensor": String(localized: "Temperature")
        case "kubespan": "KubeSpan"
        case "plugin": String(localized: "Plugins")
        default: String(localized: "Finding")
        }
    }
}
