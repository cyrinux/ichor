package name.levis.ichor.model

import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import name.levis.ichor.data.TalosJson

// Mirrors go/ichorgo/oncall*.go (times in unix ms, 0 when unset).

/** An incident's status while it is open. */
object OnCallStatus {
    const val TRIGGERED = "triggered"
    const val ACKNOWLEDGED = "acknowledged"
}

/** What can be done to an incident. */
object OnCallVerb {
    const val ACKNOWLEDGE = "acknowledge"
    const val RESOLVE = "resolve"
}

/** How sure the match of an alert to an incident is. */
object OnCallConfidence {
    const val EXACT = "exact"
    const val ALERTNAME = "alertname"
}

/** A cluster's on-call integration. [token] stays in the sealed store: never logged, never in a backup. */
@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class OnCallConfig(
    @EncodeDefault val kind: String = KIND_PAGERDUTY,
    val token: String = "",
    /** The email of the PagerDuty user who acts. */
    val from: String = "",
    val serviceIds: List<String> = emptyList(),
    @EncodeDefault val region: String = REGION_US,
) {
    /** Enough to ask PagerDuty for incidents. */
    val isSet: Boolean get() = token.isNotBlank() && serviceIds.isNotEmpty()

    companion object {
        const val KIND_PAGERDUTY = "pagerduty"
        const val REGION_US = "us"
        const val REGION_EU = "eu"
    }
}

@Serializable
data class OnCallIncident(
    val id: String = "",
    val number: Int = 0,
    val title: String = "",
    val status: String = "",
    val urgency: String = "",
    val createdAt: Long = 0,
    val url: String = "",
    val service: String = "",
    val assignees: List<String> = emptyList(),
    val acknowledgedBy: String = "",
    val matchedFingerprints: List<String> = emptyList(),
)

@Serializable
data class OnCallMatch(
    val incidentId: String = "",
    val status: String = "",
    val confidence: String = OnCallConfidence.ALERTNAME,
)

/** The open incidents, and the one behind each alert by its fingerprint. */
@Serializable
data class OnCallIncidents(
    val incidents: List<OnCallIncident> = emptyList(),
    val matches: Map<String, OnCallMatch> = emptyMap(),
) {
    /** The incident behind the alert of [fingerprint], null when none. */
    fun incidentOf(fingerprint: String): OnCallIncident? =
        matches[fingerprint]?.let { match -> incidents.firstOrNull { it.id == match.incidentId } }
}

/** What Test answers: [user] is empty for an account-level token. */
@Serializable
data class OnCallIdentity(val user: String = "", val account: String = "")

/** What the matching reads of an alert. */
@Serializable
data class OnCallAlertInput(val fingerprint: String, val labels: Map<String, String>)

private val GoJson = Json(TalosJson) { encodeDefaults = true }

/** The settings for Go, every field included. Holds the token: never log it. */
fun OnCallConfig.toGoJson(): String = GoJson.encodeToString(OnCallConfig.serializer(), this)

/** Who acknowledged [this], whoever it is assigned to otherwise; "" when nobody is known. */
val OnCallIncident.acknowledgerName: String get() = acknowledgedBy.ifEmpty { assignees.firstOrNull().orEmpty() }
