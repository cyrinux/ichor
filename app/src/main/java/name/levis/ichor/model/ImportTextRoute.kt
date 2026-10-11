package name.levis.ichor.model

import kotlinx.serialization.Serializable

/**
 * Metadata from Go ClassifyImportText; it never contains the scanned secret. [values] are the
 * fields of a secret-free file (an ~/.aws/config: start URL, regions, account, role) to prefill.
 */
@Serializable
data class ImportTextRoute(
    val kind: String = "unknown",
    val provider: String = "",
    val field: String = "",
    val values: Map<String, String> = emptyMap(),
) {
    /** An ~/.aws/config that signs in with IAM Identity Center: EKS discovery with [values]. */
    val isEksIdentityCenter: Boolean get() = kind == "credentials" && provider == "eks" && AWS_SSO_START_URL in values

    /** An ~/.aws/credentials file: access keys are typed on the EKS discovery screen, not scanned. */
    val isAwsAccessKeys: Boolean get() = kind == "refused" && provider == "eks"

    val isGkeCredential: Boolean get() = kind == "credentials" && provider == "gke" &&
        this.field in setOf(GCP_USER_CREDENTIALS, "gcpServiceAccountJson", GCP_LOGIN_CONFIG)
}
