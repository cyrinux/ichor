import Foundation
import IchorCore

/// How sign-in and cloud discovery fields, options and providers are named on screen (the Go
/// core's field names, see KubeSignInInfo and KubeDiscoverFields). Same texts as Android.
enum KubeAuthWording {
    static func fieldLabel(_ field: String) -> String {
        switch field {
        case "awsAccessKeyId": String(localized: "AWS access key ID")
        case "awsSecretAccessKey": String(localized: "AWS secret access key")
        case "awsSessionToken": String(localized: "AWS session token (optional)")
        case "awsSsoStartUrl": String(localized: "IAM Identity Center start URL")
        case "awsSsoRegion": String(localized: "IAM Identity Center region")
        case "awsAccountId": String(localized: "AWS account ID")
        case "awsRoleName": String(localized: "Role name")
        case "awsRegion": String(localized: "AWS region")
        case "gcpServiceAccountJson": String(localized: "Service account key (JSON)")
        case "azureClientId": String(localized: "Client ID")
        case "azureClientSecret": String(localized: "Client secret")
        case "azureTenantId": String(localized: "Tenant ID")
        case "azureSubscriptionId": String(localized: "Subscription ID")
        case "doApiToken": String(localized: "DigitalOcean API token")
        case "rancherApiKey": String(localized: "Rancher API key")
        case "rancherServer": String(localized: "Rancher server URL")
        case "serviceAccountKey": String(localized: "Service account key (OMNI_SERVICE_ACCOUNT_KEY)")
        default: field
        }
    }

    /// A placeholder showing what a field looks like, nil when there is no useful one.
    static func fieldPrompt(_ field: String) -> String? {
        switch field {
        case "awsSsoStartUrl": "https://my-org.awsapps.com/start"
        case "awsSsoRegion", "awsRegion": "eu-west-1"
        case "awsAccountId": "123456789012"
        case "rancherApiKey": "token-xxxxx:secret"
        case "rancherServer": "https://rancher.example.com"
        default: nil
        }
    }

    /// The name of a credentials option by its first field (EKS: IAM Identity Center or keys).
    static func optionLabel(_ fields: [String]) -> String {
        switch fields.first {
        case "awsSsoStartUrl": String(localized: "IAM Identity Center")
        case "awsAccessKeyId": String(localized: "Access keys")
        default: fields.map(fieldLabel).joined(separator: ", ")
        }
    }

    /// A cloud discovery provider (Go KubeDiscoverFields keys); brand names, not translated.
    static func providerLabel(_ provider: String) -> String {
        switch provider {
        case "eks": "AWS EKS"
        case "gke": "Google GKE"
        case "aks": "Azure AKS"
        case "digitalocean": "DigitalOcean"
        case "rancher": "Rancher"
        default: provider
        }
    }

    /// A sign-in method (ContextSummary.signIn), as the preview and the cluster rows name it.
    static func methodLabel(_ method: String) -> String {
        ContextSummary(name: "", kind: ContextKind.kube, auth: method).localizedAuthLabel
    }
}
