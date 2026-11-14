package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** Ichorgo.historyAlertFlaps: the alerts that opened more than once in [windowHours], most opens first. */
@Serializable
data class HistoryFlaps(val windowHours: Int = 24, val alerts: List<HistoryFlap> = emptyList()) {
    /** The opens of each key, for the alert rules' flapping check. */
    val opensByKey: Map<String, Int> get() = alerts.associate { it.key to it.opens }
}

/** One alert key of the history ring: how many times it opened and closed, whether it is open now. */
@Serializable
data class HistoryFlap(
    val key: String = "",
    val opens: Int = 0,
    val closes: Int = 0,
    val open: Boolean = false,
    val lastOpenedAt: Long = 0,
)
