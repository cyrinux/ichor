package name.levis.ichor.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.descriptors.buildClassSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

// Mirrors go/plugin/tree.go (the render tree, docs/plugins.md "Render tree") and
// go/ichorgo/plugin_api.go / plugin_catalog.go. Everything is resolved and translated by Go:
// the app evaluates nothing and knows no CRD. A section type this build does not know decodes
// to [PluginSection.Unknown] and draws nothing, so a newer core never breaks an older app.

/** One plugin rendered over the cluster (or its samples in the demo). */
@Serializable
data class PluginTree(
    val plugin: PluginInfo = PluginInfo(),
    val active: Boolean = false,
    /** api, pods, services, inventory or demo; "" when the product is not there. */
    val detectedVia: String = "",
    val apiVersion: String = "",
    val health: String = "",
    /** A list stopped at its limit: counts are a lower bound. */
    val partial: Boolean = false,
    val summary: PluginSummary = PluginSummary(),
    val cards: List<PluginCard> = emptyList(),
    val findings: List<PluginFinding> = emptyList(),
    val errors: List<PluginError> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = pluginHealth(health)
}

@Serializable
data class PluginInfo(
    val id: String = "",
    val name: String = "",
    val icon: String = "",
    val category: String = PLUGIN_CATEGORY_DATA_SERVICES,
    val version: String = "",
    /** `native:<route>`: a native screen stands in for the generic one; "" otherwise. */
    val screen: String = "",
    val website: String = "",
)

/** The tile in the Data services card and the header of the plugin's tab. */
@Serializable
data class PluginSummary(
    val title: String = "",
    val health: String = "",
    val partial: Boolean = false,
    val badges: List<PluginBadge> = emptyList(),
) {
    val serviceHealth: ServiceHealth get() = pluginHealth(health)
}

@Serializable
data class PluginBadge(val label: String = "", val value: String = "", val tone: String = "")

/** A list card: one row per object (or data service element). */
@Serializable
data class PluginCard(
    val id: String = "",
    val type: String = "list",
    val empty: String = "",
    val rows: List<PluginRow> = emptyList(),
)

/** An object the app can open on its object screen. */
@Serializable
data class PluginRef(
    val group: String = "",
    val version: String = "",
    val resource: String = "",
    val namespace: String = "",
    val name: String = "",
) {
    val key: String get() = "$group/$resource/$namespace/$name"

    /** The object screen's ref; [kind] comes from the plugin catalog ("" when unknown: only the title misses it). */
    fun objectRef(kind: String): KubeObjectRef = KubeObjectRef(group, version, resource, kind, namespace, name, editable = false)
}

@Serializable
data class PluginRow(
    val ref: PluginRef? = null,
    val title: String = "",
    val subtitle: String = "",
    val status: PluginStatus? = null,
    val trailing: PluginTrailing? = null,
    val health: PluginHealth? = null,
    val detail: List<PluginSection> = emptyList(),
) {
    /** The row's tone: its status's, else its health's. */
    val serviceHealth: ServiceHealth get() = pluginHealth(status?.tone ?: health?.tone.orEmpty())
}

@Serializable
data class PluginStatus(val tone: String = "", val text: String = "")

@Serializable
data class PluginTrailing(val text: String = "")

@Serializable
data class PluginHealth(val tone: String = "", val reason: String = "", val message: String = "")

/** A detail primitive, discriminated on `type`. */
@Serializable(with = PluginSectionSerializer::class)
sealed interface PluginSection {
    @Serializable
    data class KeyValues(val items: List<PluginItem> = emptyList()) : PluginSection

    @Serializable
    data class Badges(val items: List<PluginItem> = emptyList()) : PluginSection

    @Serializable
    data class Gauge(
        val label: String = "",
        val value: Double? = null,
        val max: Double? = null,
        val unit: String = "",
        val tone: String = "",
        val text: String = "",
    ) : PluginSection {
        /** 0..1, null when there is no maximum to measure against. */
        val fraction: Float?
            get() {
                val v = value ?: return null
                val m = max?.takeIf { it > 0 } ?: return null
                return (v / m).coerceIn(0.0, 1.0).toFloat()
            }
    }

    @Serializable
    data class Conditions(val items: List<PluginCondition> = emptyList()) : PluginSection

    @Serializable
    data class Relations(val items: List<PluginItem> = emptyList()) : PluginSection

    @Serializable
    data class Table(val columns: List<PluginColumn> = emptyList(), val rows: List<List<String>> = emptyList()) : PluginSection

    @Serializable
    data class Text(val text: String = "", val tone: String = "", val mono: Boolean = false) : PluginSection

    @Serializable
    data class Actions(val items: List<PluginAction> = emptyList()) : PluginSection

    /** A section type this build does not know: drawn as nothing. */
    @Serializable
    data class Unknown(val type: String = "") : PluginSection
}

/**
 * Decodes a section by its `type`, never failing: an entry that is not an object, a type this
 * build does not know, or a known type whose fields do not decode (a newer or broken core)
 * is [PluginSection.Unknown], and the rest of the tree still renders. Decoding only.
 */
object PluginSectionSerializer : KSerializer<PluginSection> {
    override val descriptor: SerialDescriptor = buildClassSerialDescriptor("name.levis.ichor.model.PluginSection")

    override fun deserialize(decoder: Decoder): PluginSection {
        val input = decoder as? JsonDecoder ?: throw SerializationException("a plugin section decodes from JSON only")
        val obj = input.decodeJsonElement() as? JsonObject ?: return PluginSection.Unknown()
        val type = (obj["type"] as? JsonPrimitive)?.contentOrNull.orEmpty()
        val serializer = sectionSerializer(type) ?: return PluginSection.Unknown(type)
        return runCatching { input.json.decodeFromJsonElement(serializer, obj) }.getOrElse { PluginSection.Unknown(type) }
    }

    override fun serialize(encoder: Encoder, value: PluginSection) {
        throw SerializationException("plugin sections are only read")
    }

    private fun sectionSerializer(type: String): KSerializer<out PluginSection>? = when (type) {
        "keyValues" -> PluginSection.KeyValues.serializer()
        "badges" -> PluginSection.Badges.serializer()
        "gauge" -> PluginSection.Gauge.serializer()
        "conditions" -> PluginSection.Conditions.serializer()
        "relations" -> PluginSection.Relations.serializer()
        "table" -> PluginSection.Table.serializer()
        "text" -> PluginSection.Text.serializer()
        "actions" -> PluginSection.Actions.serializer()
        else -> null
    }
}

/** A keyValues, badges or relations entry. */
@Serializable
data class PluginItem(
    val label: String = "",
    val value: String = "",
    val tone: String = "",
    val mono: Boolean = false,
    val ref: PluginRef? = null,
    val text: String = "",
)

@Serializable
data class PluginColumn(val label: String = "")

@Serializable
data class PluginCondition(
    val type: String = "",
    val status: String = "",
    val reason: String = "",
    val message: String = "",
    val tone: String = "",
    /** Unix ms, 0 when unknown. */
    val lastTransition: Long = 0,
)

/** A declared action's button. */
@Serializable
data class PluginAction(
    val id: String = "",
    val label: String = "",
    val enabled: Boolean = false,
    /** none, plain or typedName. */
    val confirm: String = "",
    val disabledReason: String = "",
    /** The can-I answer; null until the actions step sets it. */
    val allowed: Boolean? = null,
    val params: List<PluginParam> = emptyList(),
) {
    /** Whether the button may be pressed, as far as the tree says. */
    val usable: Boolean get() = enabled && allowed != false
}

@Serializable
data class PluginParam(
    val name: String = "",
    val label: String = "",
    /** string, int, bool or enum. */
    val type: String = "",
    val values: List<String> = emptyList(),
)

@Serializable
data class PluginFinding(
    val id: String = "",
    val severity: String = "",
    val title: String = "",
    val detail: String = "",
    val ref: PluginRef? = null,
    val opens: String = "",
)

/** A resource that could not be read, or a value that could not be evaluated: the rest still renders. */
@Serializable
data class PluginError(val resource: String = "", val where: String = "", val message: String = "") {
    val subject: String get() = resource.ifEmpty { where }
}

/** KubePluginDetect: every plugin, active or not. */
@Serializable
data class PluginDetect(val items: List<PluginDetectItem> = emptyList()) {
    val active: List<PluginDetectItem> get() = items.filter { it.active }

    /** The active plugins of Data services: a tile in its card, a tab in its screen (unless a native screen stands in). */
    val dataServicePlugins: List<PluginDetectItem>
        get() = active.filter { it.category == PLUGIN_CATEGORY_DATA_SERVICES }

    /** The active plugins that open from the Kubernetes home's tools instead. */
    val homeTiles: List<PluginDetectItem>
        get() = active.filter { it.category != PLUGIN_CATEGORY_DATA_SERVICES }
}

@Serializable
data class PluginDetectItem(
    val id: String = "",
    val name: String = "",
    val icon: String = "",
    val category: String = PLUGIN_CATEGORY_DATA_SERVICES,
    val screen: String = "",
    val active: Boolean = false,
    val detectedVia: String = "",
    /** The tile of an active plugin; null otherwise. */
    val summary: PluginSummary? = null,
) {
    /** The `<route>` of `screen: native:<route>`, null for the generic screen. */
    val nativeScreen: String? get() = nativePluginScreen(screen)
}

/** PluginCatalog: what each built-in plugin reads, for the kinds of the objects its rows open. */
@Serializable
data class PluginCatalog(val items: List<PluginCatalogItem> = emptyList()) {
    /** The kind of [ref]'s resource as plugin [pluginId] declares it; "" when it does not. */
    fun kindOf(pluginId: String, ref: PluginRef): String =
        items.firstOrNull { it.id == pluginId }?.resources
            ?.firstOrNull { it.group == ref.group && it.resource == ref.resource }?.kind.orEmpty()
}

@Serializable
data class PluginCatalogItem(
    val id: String = "",
    val name: String = "",
    val category: String = "",
    val resources: List<PluginCatalogResource> = emptyList(),
)

@Serializable
data class PluginCatalogResource(val group: String = "", val version: String = "", val resource: String = "", val kind: String = "")

const val PLUGIN_CATEGORY_DATA_SERVICES = "data-services"

private const val NATIVE_PREFIX = "native:"

/** "argocd" for "native:argocd"; null for "" or anything else. */
fun nativePluginScreen(screen: String): String? =
    screen.trim().takeIf { it.startsWith(NATIVE_PREFIX) }?.removePrefix(NATIVE_PREFIX)?.trim()?.takeIf { it.isNotEmpty() }

/**
 * A plugin tone as a health level: ok, idle, warning and critical as such; info reads calm
 * (idle), unknown and anything else unknown.
 */
fun pluginHealth(tone: String): ServiceHealth = when (tone) {
    "ok" -> ServiceHealth.OK
    "idle", "info" -> ServiceHealth.IDLE
    "warning" -> ServiceHealth.WARNING
    "critical" -> ServiceHealth.CRITICAL
    else -> ServiceHealth.UNKNOWN
}

/** The summary as one line: "2 sleep schedules · 1 asleep · 1 warnings". */
fun PluginSummary.line(): String =
    (listOf(title) + badges.map { b -> listOf(b.value, b.label).filter { it.isNotEmpty() }.joinToString(" ") })
        .filter { it.isNotEmpty() }
        .joinToString(" · ")

/** The rows of [card] grouped by namespace when they are namespaced objects of several namespaces. */
fun PluginCard.groupsByNamespace(): Boolean {
    val namespaces = rows.mapNotNull { it.ref?.namespace }.toSet()
    return rows.all { it.ref != null } && namespaces.none { it.isEmpty() } && namespaces.size > 1
}
