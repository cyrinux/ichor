package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

/**
 * An object pinned to the overview's Pinned card: the screen it opens, as a share link names
 * it ([ShareTarget], so nothing a link would not carry), and the name it shows. The same JSON
 * as iOS's `Pin`: `{"target": {...}, "label": "..."}`.
 */
@Serializable
data class Pin(val target: ShareTarget, val label: String = "") {
    /** The object it names, whatever the cluster id or the tab it was pinned from. */
    val key: ShareTarget get() = target.copy(cluster = "", tab = "")
}

/** A cluster's pins, in the order pinned: their stored form and how one is added or removed. */
object Pins {
    /** At most this many per cluster: a card to glance at, not a second list. */
    const val MAX = 12

    /** What can be pinned: the cluster-wide screens are one tap from the overview already. */
    val PINNABLE = setOf(
        ShareTarget.NODE,
        ShareTarget.WORKLOAD,
        ShareTarget.POD,
        ShareTarget.CRONJOB,
        ShareTarget.ARGO_APP,
        ShareTarget.FLUX_APP,
    )

    private val json = Json { ignoreUnknownKeys = true }
    private val serializer = ListSerializer(Pin.serializer())

    /** Reads [encode]'s form; anything unreadable is no pins, repeats and extras are dropped. */
    fun parse(text: String?): List<Pin> {
        if (text.isNullOrBlank()) return emptyList()
        val pins = runCatching { json.decodeFromString(serializer, text) }.getOrNull().orEmpty()
        return pins.filter { it.target.target in PINNABLE }.distinctBy { it.key }.take(MAX)
    }

    /** The JSON array of [pins], without the share targets' empty fields (as iOS writes it). */
    fun encode(pins: List<Pin>): String = json.encodeToString(serializer, pins)

    fun contains(pins: List<Pin>, target: ShareTarget): Boolean {
        val key = Pin(target, "").key
        return pins.any { it.key == key }
    }

    /**
     * Removes [pin] when its object is pinned, else adds it last; null when it would be one
     * more than [MAX] (or cannot be pinned), the list then unchanged.
     */
    fun toggle(pins: List<Pin>, pin: Pin): List<Pin>? = when {
        contains(pins, pin.target) -> pins.filter { it.key != pin.key }
        pin.target.target !in PINNABLE || pins.size >= MAX -> null
        else -> pins + pin
    }
}
