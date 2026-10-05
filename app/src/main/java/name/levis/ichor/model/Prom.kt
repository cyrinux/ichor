package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import name.levis.ichor.data.TalosJson

/**
 * Where PromQL queries go (Ichorgo.normalizePromSource): mode "proxy" reaches a Service
 * through the Kubernetes API, "url" a server the phone reaches itself. [secret] is the bearer
 * token or basic password, kept with the rest in the cluster's encrypted metrics store.
 */
@Serializable
data class PromSource(
    val mode: String = MODE_PROXY,
    val kind: String = "",
    val namespace: String = "",
    val service: String = "",
    val port: Int = 0,
    val pathPrefix: String = "",
    val url: String = "",
    val auth: String = "",
    val username: String = "",
    val secret: String = "",
    val ca: String = "",
    val insecureSkipVerify: Boolean = false,
    val tenant: String = "",
) {
    /** Never prints [secret] (logs, crash reports). */
    override fun toString(): String = "PromSource($mode, $label, auth=$auth)"

    /** "monitoring/prometheus-operated:9090" or the URL. */
    val label: String get() = if (mode == MODE_PROXY) "$namespace/$service:$port$pathPrefix" else url

    companion object {
        const val MODE_PROXY = "proxy"
        const val MODE_URL = "url"
        const val AUTH_NONE = ""
        const val AUTH_BEARER = "bearer"
        const val AUTH_BASIC = "basic"
    }
}

private val GoJson = Json(TalosJson) { encodeDefaults = true }

/** The source for Go, every field included: TalosJson leaves defaults out (mode "proxy" among them). */
fun PromSource.toGoJson(): String = GoJson.encodeToString(PromSource.serializer(), this)

/** Part of the Go core's message for a query the backend refused, HTTP 401 or 403 (prom_parse.go promRefused). */
const val PROM_REFUSED = "refused (credentials or tenant)"

/** The backend turned the query down: the tenant (X-Scope-OrgID) or the credentials need changing. */
fun isPromRefused(error: String?): Boolean = error?.contains(PROM_REFUSED) == true

@Serializable
data class PromDiscovery(val sources: List<PromSource> = emptyList())

/** A query result: [times] in unix ms, each series' values aligned on them, null for a gap. */
@Serializable
data class PromResult(
    val resultType: String = "",
    val times: List<Long> = emptyList(),
    val series: List<PromSeries> = emptyList(),
    val warnings: List<String> = emptyList(),
    val truncated: Boolean = false,
    val total: Int = 0,
)

@Serializable
data class PromSeries(
    val name: String = "",
    val labels: Map<String, String> = emptyMap(),
    val values: List<Double?> = emptyList(),
)

/**
 * A saved chart. [unit]: percent, bytes, cores, persec, count or "" (plain). [legend] names
 * each series from its labels ("{{namespace}}/{{pod}}"), "" for the series name.
 */
@Serializable
data class PromPanel(
    val id: String,
    val title: String,
    val query: String,
    val unit: String = "",
    val legend: String = "",
)

/** A cluster's metrics setup; [source] null until one is chosen. */
@Serializable
data class MetricsConfig(
    val source: PromSource? = null,
    val panels: List<PromPanel> = emptyList(),
)

private val LEGEND_LABEL = Regex("""\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}""")

/** The series' name in a legend: [template] with {{label}} replaced, else its full name. */
fun PromSeries.legend(template: String): String {
    if (template.isBlank()) return name
    return LEGEND_LABEL.replace(template) { labels[it.groupValues[1]].orEmpty() }.trim().ifEmpty { name }
}

/** The last value that is not a gap, or null. */
fun PromSeries.latest(): Double? = values.lastOrNull { it != null }
