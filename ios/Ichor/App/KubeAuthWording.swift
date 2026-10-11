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
        case "awsRegions": String(localized: "AWS regions (optional, comma separated)")
        case "gcpServiceAccountJson": String(localized: "Service account key (JSON)")
        case "gcpUserCredentialsJson": String(localized: "gcloud user credentials (application_default_credentials.json)")
        case "gcpProjects": String(localized: "Project IDs (optional)")
        case "gcpOAuthClientId": String(localized: "OAuth client ID")
        case "gcpOAuthClientSecret": String(localized: "OAuth client secret")
        case "gcpOAuthRedirectUrl": String(localized: "Redirect URL (optional)")
        case kubeLoginConfigField: String(localized: "Workforce login config (JSON)")
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
        case "awsRegions": "eu-west-1, us-east-1"
        case "awsAccountId": "123456789012"
        case "rancherApiKey": "token-xxxxx:secret"
        case "rancherServer": "https://rancher.example.com"
        case "gcpProjects": "sample-proj-1, sample-proj-2"
        case "gcpOAuthClientId": "123-abc.apps.googleusercontent.com"
        case "gcpOAuthRedirectUrl": "https://cyrinux.github.io/ichor/auth/google/"
        default: nil
        }
    }

    /// How to get what a field asks for, nil when its label says enough.
    static func fieldHint(_ field: String) -> String? {
        switch field {
        case "gcpUserCredentialsJson":
            String(localized: "Bring your gcloud session: on your computer run gcloud auth application-default login, then import ~/.config/gcloud/application_default_credentials.json. When Google ends the session, run it again and import the new file.")
                + "\n\n"
                + String(localized: "A workforce identity session (gcloud auth application-default login --login-config=…) is imported the same way.")
        case kubeAWSStartURLField:
            String(localized: "Enter the start URL from your AWS access portal and its region; the account and role are chosen after the sign-in. Leave them empty unless you know them.")
        case kubeAWSRegionsField:
            String(localized: "Signs in at your AWS access portal in the browser, then lists the EKS clusters of the accounts and roles you choose in the regions given (all common regions when empty).")
        case "gcpGcloudSignIn":
            String(localized: "Opens Google's sign-in in the browser, as gcloud auth application-default login does on a computer. Works on every build; your organisation's SSO applies. The consent page names Google Cloud SDK.")
        case kubeLoginConfigField:
            String(localized: "Import the login config your administrator gave you; the sign-in opens your organisation's identity provider in the browser.")
        case "gcpGoogleSignIn":
            String(localized: "Google lists the projects your account can see; Ichor reads their clusters.")
        case "gcpProjects":
            String(localized: "Leave empty to search every project your account can see; list project IDs to go faster in a large organisation.")
        case "gcpOAuthClientId":
            String(localized: "Ask your Google Cloud admin for a Desktop app OAuth client (APIs & Services → Credentials) with an Internal consent screen. Sign in then opens your Google account in the browser.")
                + "\n\n"
                + String(localized: "With a Web application client instead, enter https://cyrinux.github.io/ichor/auth/google/ (or your organisation's own page) as redirect URL and add it to the client's authorised redirect URIs. Leave it empty for a Desktop app client.")
        default: nil
        }
    }

    /// The name of a credentials option by its first field (EKS: IAM Identity Center or keys,
    /// GKE: a service account key or gcloud user credentials).
    static func optionLabel(_ fields: [String]) -> String {
        switch fields.first {
        case "awsSsoStartUrl": String(localized: "IAM Identity Center")
        case "awsAccessKeyId": String(localized: "Access keys")
        case "gcpServiceAccountJson": String(localized: "Service account key")
        case "gcpUserCredentialsJson": String(localized: "gcloud user credentials")
        case "gcpOAuthClientId": String(localized: "Your organisation's OAuth client")
        case "gcpGcloudSignIn": String(localized: "Your Google account (like gcloud)")
        case kubeLoginConfigField: String(localized: "Workforce identity (login config)")
        case "gcpGoogleSignIn": String(localized: "Sign in with Google")
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
        case "omni": "Sidero Omni"
        default: provider
        }
    }

    /// A sign-in method (ContextSummary.signIn), as the preview and the cluster rows name it.
    static func methodLabel(_ method: String) -> String {
        ContextSummary(name: "", kind: ContextKind.kube, auth: method).localizedAuthLabel
    }
}
