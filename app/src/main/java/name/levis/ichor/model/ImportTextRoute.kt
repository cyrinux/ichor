package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** Metadata from Go ClassifyImportText; it never contains the scanned secret. */
@Serializable
data class ImportTextRoute(val kind: String = "unknown", val provider: String = "", val field: String = "") {
    val isGkeCredential: Boolean get() = kind == "credentials" && provider == "gke" &&
        this.field in setOf(GCP_USER_CREDENTIALS, "gcpServiceAccountJson")
}
