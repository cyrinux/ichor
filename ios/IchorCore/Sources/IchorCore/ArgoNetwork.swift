import Foundation

// An Argo CD app's network is a NetMap (go/ichorgo/kube_argocd_network.go): host → Gateway →
// route → Service → pod → node, left to right. What is Argo's own is how its problem banner
// words the root cause.

/// Why traffic does not get through, worded by the app.
public enum ArgoNetReason: Equatable, Sendable {
    case nodeNotReady(String)
    case nodeCordoned(String)
    case pod(String, String)
    case serviceWithoutPods(String)
    case noHealthyBackend(String, String)
    case other(String, String, String)
}

public extension NetMapProblem {
    /// What the Argo CD network's banner says, for the app to word.
    var reason: ArgoNetReason {
        switch kind {
        case .node: detail == "SchedulingDisabled" ? .nodeCordoned(name) : .nodeNotReady(name)
        case .pod: .pod(name, detail)
        case .service: .serviceWithoutPods(name)
        case .ingress, .httpRoute, .grpcRoute, .tlsRoute, .ingressRoute, .ingressRouteTCP, .gateway:
            .noHealthyBackend(kind.rawValue, name)
        case .host, .loadBalancer, .externalName, .proxyGroup, .proxy, .unknown: .other(kind.rawValue, name, detail)
        }
    }
}
