package name.levis.ichor.model

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The params form of a plugin action: starting values, checks before sending, and the JSON sent. */
class PluginActionFormTest {
    private val action = TalosJson.decodeFromString(
        PluginAction.serializer(),
        """
        {"id": "resize", "label": "Resize", "enabled": true, "confirm": "typedName",
         "params": [
           {"name": "size", "label": "Size", "type": "int"},
           {"name": "replicas", "label": "Replicas", "type": "int", "default": 3},
           {"name": "note", "label": "Note", "type": "string", "default": ""},
           {"name": "reason", "label": "Reason", "type": "string"},
           {"name": "force", "label": "Force", "type": "bool", "default": false},
           {"name": "wait", "label": "Wait", "type": "bool"},
           {"name": "mode", "label": "Mode", "type": "enum", "values": ["fast", "safe"], "default": "safe"},
           {"name": "tier", "label": "Tier", "type": "enum", "values": ["gold", "silver"]}
         ]}
        """.trimIndent(),
    )
    private val params = action.params

    private fun encoded(values: Map<String, String>): JsonObject = Json.parseToJsonElement(encodeParams(params, values)).jsonObject

    private fun filled(vararg changes: Pair<String, String>): Map<String, String> =
        initialParamValues(params) + mapOf("size" to "10", "reason" to "maintenance", "tier" to "gold") + changes

    @Test
    fun decodesDefaultsAndRequired() {
        assertEquals(JsonPrimitive(3), params[1].default)
        assertEquals(JsonPrimitive(false), params[4].default)
        assertEquals(listOf("size", "reason", "wait", "tier"), params.filter { it.required }.map { it.name })
        // An empty-string or false default is still a default: the param is optional.
        assertFalse(params[2].required)
        assertFalse(params[4].required)
    }

    @Test
    fun startsFromTheDefaults() {
        assertEquals(
            mapOf("size" to "", "replicas" to "3", "note" to "", "reason" to "", "force" to "false", "wait" to "false", "mode" to "safe", "tier" to ""),
            initialParamValues(params),
        )
    }

    @Test
    fun requiredFieldsBlockTheForm() {
        val problems = paramProblems(params, initialParamValues(params))
        assertEquals(
            mapOf("size" to PluginParamProblem.REQUIRED, "reason" to PluginParamProblem.REQUIRED, "tier" to PluginParamProblem.REQUIRED),
            problems,
        )
        assertTrue(paramProblems(params, filled()).isEmpty())
    }

    @Test
    fun checksTypes() {
        assertEquals(PluginParamProblem.NOT_INTEGER, paramProblems(params, filled("size" to "big"))["size"])
        assertEquals(PluginParamProblem.NOT_INTEGER, paramProblems(params, filled("size" to "1.5"))["size"])
        assertEquals(PluginParamProblem.NOT_INTEGER, paramProblems(params, filled("size" to "99999999999999999999"))["size"])
        assertTrue(paramProblems(params, filled("size" to " -4 ")).isEmpty())
        assertEquals(PluginParamProblem.NOT_A_VALUE, paramProblems(params, filled("tier" to "bronze"))["tier"])
        assertEquals(PluginParamProblem.REQUIRED, paramProblems(params, filled("wait" to ""))["wait"])
        // 1,024 bytes of UTF-8, not characters: 513 two-byte letters are too long.
        assertTrue(paramProblems(params, filled("reason" to "a".repeat(PLUGIN_PARAM_MAX_BYTES))).isEmpty())
        assertEquals(PluginParamProblem.TOO_LONG, paramProblems(params, filled("reason" to "é".repeat(513)))["reason"])
        val unknown = listOf(PluginParam(name = "x", type = "date"))
        assertEquals(mapOf("x" to PluginParamProblem.UNKNOWN_TYPE), paramProblems(unknown, mapOf("x" to "2026")))
    }

    @Test
    fun encodesTypedValues() {
        val json = encoded(filled("size" to " 42 ", "force" to "true"))
        assertEquals(JsonPrimitive(42L), json["size"])
        assertEquals(JsonPrimitive(3L), json["replicas"])
        assertEquals(JsonPrimitive(""), json["note"])
        assertEquals(JsonPrimitive("maintenance"), json["reason"])
        assertEquals(JsonPrimitive(true), json["force"])
        assertEquals(JsonPrimitive(false), json["wait"])
        assertEquals(JsonPrimitive("safe"), json["mode"])
        assertEquals(JsonPrimitive("gold"), json["tier"])
        // A number, never a string: Go wants a JSON integer.
        assertTrue(encodeParams(params, filled("size" to "42")).contains("\"size\":42"))
    }

    @Test
    fun leavesOptionalEmptyValuesToGo() {
        val json = encoded(filled("replicas" to "", "mode" to ""))
        assertFalse("replicas" in json)
        assertFalse("mode" in json)
    }

    @Test
    fun escapesWhatIsTyped() {
        val tricky = "a\"b\\c\n../../secrets\",\"admin\":true"
        val json = encoded(filled("reason" to tricky))
        assertEquals(JsonPrimitive(tricky), json["reason"])
        assertFalse("admin" in json)
    }

    @Test
    fun noParamsSendsNothing() {
        assertEquals("", encodeParams(emptyList(), emptyMap()))
        assertTrue(paramProblems(emptyList(), emptyMap()).isEmpty())
    }

    @Test
    fun usableFollowsEnabledAndAllowed() {
        assertTrue(PluginAction(enabled = true, allowed = null).usable)
        assertTrue(PluginAction(enabled = true, allowed = true).usable)
        assertFalse(PluginAction(enabled = true, allowed = false).usable)
        assertFalse(PluginAction(enabled = false, allowed = true).usable)
    }
}
