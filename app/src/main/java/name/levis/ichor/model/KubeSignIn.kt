package name.levis.ichor.model

import androidx.annotation.StringRes
import kotlinx.serialization.Serializable
import name.levis.ichor.R

// Mirrors go/ichorgo/kube_auth.go (KubeSignInInfo), kube_auth_oidc.go (signInPrompt) and
// kube_discover.go (KubeDiscoverFields): signing kubeconfig clusters in, and finding them in a
// cloud account.

/** How a stored kubeconfig cluster signs in (KubeSignInInfo); none for static credentials. */
@Serializable
data class KubeSignInInfo(
    val method: String = "",
    /** [KIND_BROWSER] (OIDC, device code included) or [KIND_CREDENTIALS] ([fields] to ask for). */
    val kind: String = KIND_BROWSER,
    val fields: List<String> = emptyList(),
    /** Alternative field sets: EKS asks for IAM Identity Center or access keys. */
    val options: List<List<String>> = emptyList(),
    val signedIn: Boolean = false,
    val user: String = "",
    /** When a new sign-in will be needed, unix seconds; 0 when unknown. */
    val sessionExpires: Long = 0,
) {
    /** The field sets to choose from: [options], else [fields] alone. */
    val fieldSets: List<List<String>> get() = options.ifEmpty { listOf(fields) }

    companion object {
        const val KIND_BROWSER = "browser"
        const val KIND_CREDENTIALS = "credentials"
    }
}

/** What an interactive sign-in asks the user to do: open a page, or enter a code on one. */
@Serializable
data class SignInPrompt(
    /** [KIND_BROWSER]: open [url], it comes back by itself; [KIND_DEVICE]: show [userCode], open [url]. */
    val kind: String = "",
    val url: String = "",
    val userCode: String = "",
    val verificationUrl: String = "",
    val redirectPrefix: String = "",
    /** Seconds the device code stays valid; 0 when unknown. */
    val expiresIn: Int = 0,
) {
    val isDevice: Boolean get() = kind == KIND_DEVICE

    companion object {
        const val KIND_BROWSER = "browser"
        const val KIND_DEVICE = "device"
    }
}

/** The code a Go core error starts with when the cluster needs a sign-in (Ichorgo.KubeSignInRequired). */
const val KUBE_SIGN_IN_REQUIRED = "kube-sign-in-required"

/** A call refused for want of a sign-in: the [method] (may be "") and why, as the core said (may be ""). */
data class SignInNeeded(val method: String, val reason: String)

private val SIGN_IN_REQUIRED = Regex("""^$KUBE_SIGN_IN_REQUIRED: sign in to this cluster \(([^)]*)\)(?:: (.*))?$""", RegexOption.DOT_MATCHES_ALL)

/**
 * The sign-in a failed call asks for, from its error [message], null when it failed for
 * another reason. The code may follow what wrapped it ("kube nodes: kube-sign-in-required: …").
 */
fun signInNeeded(message: String?): SignInNeeded? {
    val at = message?.indexOf(KUBE_SIGN_IN_REQUIRED) ?: -1
    if (at < 0) return null
    val text = message!!.substring(at).trim()
    SIGN_IN_REQUIRED.matchEntire(text)?.let { return SignInNeeded(it.groupValues[1], it.groupValues[2].trim()) }
    return SignInNeeded("", text.removePrefix(KUBE_SIGN_IN_REQUIRED).trimStart(':', ' '))
}

/** The sign-in method's short name, for "Sign in with …"; null for one the app does not know. */
@StringRes
fun signInMethodName(method: String): Int? = when (method) {
    "oidc" -> R.string.kube_signin_method_oidc
    "eks" -> R.string.kube_signin_method_eks
    "gke" -> R.string.kube_signin_method_gke
    "azure" -> R.string.kube_signin_method_azure
    "digitalocean" -> R.string.kube_signin_method_digitalocean
    "rancher" -> R.string.kube_signin_method_rancher
    else -> null
}

/** How a sign-in or discovery field is entered. */
enum class FieldKind { TEXT, SECRET, JSON }

/** A credentials field: what it is called and how it is typed. */
data class CredentialField(val name: String, @StringRes val label: Int, val kind: FieldKind, val optional: Boolean = false)

/** The Go core's field names, with their label and kind; an unknown name shows as itself. */
private val FIELDS = listOf(
    CredentialField("awsAccessKeyId", R.string.kube_field_aws_access_key, FieldKind.TEXT),
    CredentialField("awsSecretAccessKey", R.string.kube_field_aws_secret_key, FieldKind.SECRET),
    CredentialField("awsSessionToken", R.string.kube_field_aws_session_token, FieldKind.SECRET, optional = true),
    CredentialField("awsSsoStartUrl", R.string.kube_field_aws_sso_start_url, FieldKind.TEXT),
    CredentialField("awsSsoRegion", R.string.kube_field_aws_sso_region, FieldKind.TEXT),
    CredentialField("awsAccountId", R.string.kube_field_aws_account_id, FieldKind.TEXT),
    CredentialField("awsRoleName", R.string.kube_field_aws_role_name, FieldKind.TEXT),
    CredentialField("awsRegion", R.string.kube_field_aws_region, FieldKind.TEXT),
    CredentialField("gcpServiceAccountJson", R.string.kube_field_gcp_service_account, FieldKind.JSON),
    CredentialField("azureTenantId", R.string.kube_field_azure_tenant, FieldKind.TEXT),
    CredentialField("azureSubscriptionId", R.string.kube_field_azure_subscription, FieldKind.TEXT),
    CredentialField("azureClientId", R.string.kube_field_azure_client_id, FieldKind.TEXT),
    CredentialField("azureClientSecret", R.string.kube_field_azure_client_secret, FieldKind.SECRET),
    CredentialField("doApiToken", R.string.kube_field_do_token, FieldKind.SECRET),
    CredentialField("rancherServer", R.string.kube_field_rancher_server, FieldKind.TEXT),
    CredentialField("rancherApiKey", R.string.kube_field_rancher_key, FieldKind.SECRET),
).associateBy { it.name }

/** The field [name], or null when the core asks for one the app does not know (shown by name, as a secret). */
fun credentialField(name: String): CredentialField? = FIELDS[name]

/** Whether every required field of [fields] has a value in [values]. */
fun credentialsComplete(fields: List<String>, values: Map<String, String>): Boolean =
    fields.all { credentialField(it)?.optional == true || values[it].orEmpty().isNotBlank() }

/** The values of [fields] to send, trimmed, empty ones left out. */
fun credentialsFor(fields: List<String>, values: Map<String, String>): Map<String, String> =
    fields.mapNotNull { name -> values[name]?.trim()?.takeIf { it.isNotEmpty() }?.let { name to it } }.toMap()

/** The label of an alternative field set: EKS asks for IAM Identity Center or access keys. */
@StringRes
fun fieldSetLabel(fields: List<String>): Int = when {
    "awsSsoStartUrl" in fields -> R.string.kube_signin_option_aws_sso
    "awsAccessKeyId" in fields -> R.string.kube_signin_option_aws_keys
    else -> R.string.kube_signin_option_other
}

/** A cloud whose clusters discovery lists (DiscoverClusters' provider). */
enum class DiscoveryProvider(val id: String, @StringRes val label: Int) {
    EKS("eks", R.string.kube_signin_method_eks),
    GKE("gke", R.string.kube_signin_method_gke),
    AKS("aks", R.string.kube_signin_method_azure),
    DIGITALOCEAN("digitalocean", R.string.kube_signin_method_digitalocean),
    RANCHER("rancher", R.string.kube_signin_method_rancher),
}

/** The fields each provider asks for (KubeDiscoverFields), only for the providers the app shows. */
fun discoveryFields(byProvider: Map<String, List<String>>): Map<DiscoveryProvider, List<String>> =
    DiscoveryProvider.entries.mapNotNull { p -> byProvider[p.id]?.takeIf { it.isNotEmpty() }?.let { p to it } }.toMap()

/**
 * The stored names of the contexts an import added: names that were not stored [before] and
 * are [after], plus the stored contexts the import replaced (see [ImportChoice.replace]).
 */
fun importedContextNames(
    before: Collection<String>,
    after: List<String>,
    conflicts: List<ImportConflict>,
    choices: List<ImportChoice>,
): List<String> {
    val replaced = choices.filter { it.replace && !it.skip }
        .mapNotNull { choice -> conflicts.firstOrNull { it.index == choice.index }?.sameAs }
        .toSet()
    return after.filter { it !in before || it in replaced }
}
