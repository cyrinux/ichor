import Foundation
import IchorCore

/// How clusters added from a kubeconfig are described: their sign-in method and why one of
/// their contexts cannot be added (the Go core's codes, see ParseKubeconfig). Same as Android.
extension ContextSummary {
    /// How the context signs in, with what it signs in to when the core names it.
    var localizedAuthLabel: String {
        let method = switch auth ?? "" {
        case "cert": String(localized: "Client certificate")
        case "token": String(localized: "Token")
        case "eks": "AWS EKS"
        case "gke": "Google GKE"
        case "oidc": "OIDC"
        case "azure": "Azure AKS"
        case "digitalocean": "DigitalOcean"
        case "rancher": "Rancher"
        case "exec": String(localized: "Exec plugin")
        case "auth-provider": String(localized: "Auth provider")
        case "basic": String(localized: "Username and password")
        default: String(localized: "None")
        }
        guard let detail = authDetail, !detail.isEmpty else { return method }
        return "\(method) · \(detail)"
    }

    /// Why the context cannot be added, nil when it can.
    var localizedKubeProblem: String? {
        guard let problem else { return nil }
        let detail = problemDetail ?? ""
        switch problem {
        case "kube-cluster-missing":
            return String(localized: "The context names a cluster the file does not have.")
        case "kube-user-missing":
            return String(localized: "The context names a user the file does not have.")
        case "kube-not-https":
            return String(localized: "The server is not https: Ichor only connects over TLS.")
        case "kube-file-path":
            return String(localized: "Certificates or keys are file paths. Run kubectl config view --flatten --minify and import its output.")
        case "kube-proxy-url":
            return String(localized: "Connecting through a proxy (proxy-url) is not supported.")
        case "kube-basic-auth":
            return String(localized: "Signing in with a username and password is not supported.")
        case "kube-sign-in-later":
            return String(localized: "Signing in with \(localizedAuthLabel) comes in a later version.")
        case "kube-exec-unsupported":
            return detail.isEmpty
                ? String(localized: "This exec plugin cannot run on a phone.")
                : String(localized: "This exec plugin cannot run on a phone: \(detail)")
        case "kube-no-credentials":
            return String(localized: "The user has no credentials: no client certificate and no token.")
        case "kube-invalid":
            return detail.isEmpty
                ? String(localized: "This context is not valid.")
                : String(localized: "This context is not valid: \(detail)")
        default:
            return detail.isEmpty ? problem : detail
        }
    }
}

extension CloudContext {
    /// The location and owner under a cloud context's cluster name: "EKS · eu-north-1 · account 12…12".
    var localizedDetail: String {
        let owner = provider == Self.eks ? String(localized: "account \(shortOwner)") : String(localized: "project \(shortOwner)")
        return [provider, location, owner].joined(separator: " · ")
    }
}
