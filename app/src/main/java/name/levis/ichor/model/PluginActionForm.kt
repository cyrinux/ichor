package name.levis.ichor.model

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull

// A plugin action's params form (docs/plugins.md "Actions"): what the fields start with, what
// the app checks before sending, and the paramsJSON KubePluginAction takes. Go checks the
// params again; this only spares a round trip. The values are never logged: they reach the
// audit log only, through Go.

/** The longest string param Go accepts, in UTF-8 bytes (go/plugin maxParamLen). */
const val PLUGIN_PARAM_MAX_BYTES = 1024

const val PLUGIN_PARAM_STRING = "string"
const val PLUGIN_PARAM_INT = "int"
const val PLUGIN_PARAM_BOOL = "bool"
const val PLUGIN_PARAM_ENUM = "enum"

/** Why a field cannot be sent as typed. */
enum class PluginParamProblem { REQUIRED, NOT_INTEGER, TOO_LONG, NOT_A_VALUE, UNKNOWN_TYPE }

/** Whether [this] has no default: it must be given. */
val PluginParam.required: Boolean get() = default == null || default is JsonNull

/**
 * The form's starting values, one text per param: the default as text ("true"/"false" for a
 * bool), "" when there is none ("false" for a bool: a switch is always on or off).
 */
fun initialParamValues(params: List<PluginParam>): Map<String, String> = params.associate { p ->
    val text = (p.default as? JsonPrimitive)?.takeUnless { it is JsonNull }?.contentOrNull
    p.name to when (p.type) {
        PLUGIN_PARAM_BOOL -> ((p.default as? JsonPrimitive)?.booleanOrNull ?: false).toString()
        else -> text.orEmpty()
    }
}

/** The fields that cannot be sent as typed in [values], by param name; empty when the form is ready. */
fun paramProblems(params: List<PluginParam>, values: Map<String, String>): Map<String, PluginParamProblem> =
    params.mapNotNull { p -> paramProblem(p, values[p.name].orEmpty())?.let { p.name to it } }.toMap()

private fun paramProblem(p: PluginParam, value: String): PluginParamProblem? = when (p.type) {
    PLUGIN_PARAM_STRING -> when {
        value.isEmpty() && p.required -> PluginParamProblem.REQUIRED
        value.toByteArray(Charsets.UTF_8).size > PLUGIN_PARAM_MAX_BYTES -> PluginParamProblem.TOO_LONG
        else -> null
    }
    PLUGIN_PARAM_INT -> when {
        value.isBlank() -> if (p.required) PluginParamProblem.REQUIRED else null
        value.trim().toLongOrNull() == null -> PluginParamProblem.NOT_INTEGER
        else -> null
    }
    PLUGIN_PARAM_BOOL -> if (value == "true" || value == "false") null else PluginParamProblem.REQUIRED
    PLUGIN_PARAM_ENUM -> when {
        value.isEmpty() -> if (p.required) PluginParamProblem.REQUIRED else null
        value !in p.values -> PluginParamProblem.NOT_A_VALUE
        else -> null
    }
    else -> PluginParamProblem.UNKNOWN_TYPE
}

/**
 * The paramsJSON of a valid form (see [paramProblems]): a JSON object built with kotlinx
 * (never by concatenation), an int as a number, a bool as a boolean. An optional int or enum
 * left empty is not sent, so Go fills its default. "" when the action has no params.
 */
fun encodeParams(params: List<PluginParam>, values: Map<String, String>): String {
    if (params.isEmpty()) return ""
    val obj: JsonObject = buildJsonObject {
        params.forEach { p ->
            val value = values[p.name].orEmpty()
            when (p.type) {
                PLUGIN_PARAM_STRING -> put(p.name, JsonPrimitive(value))
                PLUGIN_PARAM_INT -> value.trim().toLongOrNull()?.let { put(p.name, JsonPrimitive(it)) }
                PLUGIN_PARAM_BOOL -> put(p.name, JsonPrimitive(value == "true"))
                PLUGIN_PARAM_ENUM -> if (value.isNotEmpty()) put(p.name, JsonPrimitive(value))
            }
        }
    }
    return obj.toString()
}
