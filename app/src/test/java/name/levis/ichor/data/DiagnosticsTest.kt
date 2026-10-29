package name.levis.ichor.data

import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import name.levis.ichor.model.DiagEntry
import name.levis.ichor.model.DiagExportSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class DiagnosticsTest {

    private val appKeys = setOf("versionName", "versionCode", "flavor", "sdkInt", "model", "manufacturer", "locale", "dataStore")

    @Test
    fun everyAppFactIsWrittenAndNoneIsNull() {
        // What a JVM stub or an odd device gives: nulls everywhere, the core not answering.
        val facts = Diagnostics.appFacts(null, 0, null, 0, null, null, null, null)
        val json = TalosJson.parseToJsonElement(Diagnostics.appJson(facts)).jsonObject
        assertEquals(appKeys, json.keys)
        assertNoNull(json, "")
        assertEquals(setOf("persisting", "dir", "reason"), json.getValue("dataStore").jsonObject.keys)
    }

    @Test
    fun appFactsCarryTheDeviceAndTheCoreStatus() {
        val facts = Diagnostics.appFacts(
            "1.42.0", 142, "release", 36, "Pixel 9", "Google", "fr-FR",
            """{"persisting":true,"dir":"no_backup","reason":""}""",
        )
        val json = TalosJson.parseToJsonElement(Diagnostics.appJson(facts)).jsonObject
        assertEquals(JsonPrimitive("1.42.0"), json["versionName"])
        assertEquals(JsonPrimitive(142), json["versionCode"])
        assertEquals(JsonPrimitive("fr-FR"), json["locale"])
        assertEquals(JsonPrimitive(true), json.getValue("dataStore").jsonObject["persisting"])
    }

    @Test
    fun aBadDataStoreStatusIsNotFatal() {
        val facts = Diagnostics.appFacts("1", 1, "debug", 30, "m", "x", "en", "not json")
        assertFalse(facts.dataStore.persisting)
    }

    @Test
    fun attrsAreAFlatJsonObject() {
        val json = TalosJson.parseToJsonElement(Diagnostics.attrsJson(mapOf("reason" to "anr", "status" to 6, "pss_kb" to 1234L, "ok" to true)))
        assertEquals(
            JsonObject(mapOf("reason" to JsonPrimitive("anr"), "status" to JsonPrimitive(6), "pss_kb" to JsonPrimitive(1234), "ok" to JsonPrimitive(true))),
            json,
        )
        assertEquals("{}", Diagnostics.attrsJson(emptyMap()))
    }

    @Test
    fun theZipIsNamedAfterItsDate() {
        assertTrue(Regex("""ichor-diagnostics-\d{8}-\d{4}\.zip""").matches(Diagnostics.zipName(1_793_284_800_000)))
    }

    @Test
    fun decodesTheEntriesTheCoreWrites() {
        // `just probe diag-tail` against a closed local port, reversed to DiagEntries' newest first.
        val out = """[
            {"at":1793284805123,"level":"warn","component":"api","msg":"call failed","attrs":{"fn":"ClusterOverview"},"err":"dial tcp <redacted>: connection refused"},
            {"at":1793284805001,"level":"info","component":"trace","msg":"startup step","attrs":{"step":"config.parse","ms":12,"count":3}},
            {"at":1793284800000,"level":"info","component":"android","msg":"startup app.onCreate start"}
        ]"""
        val entries = TalosJson.decodeFromString(ListSerializer(DiagEntry.serializer()), out)
        assertEquals(3, entries.size)
        assertEquals("ClusterOverview", entries[0].attrs.getValue("fn").content)
        assertEquals("dial tcp <redacted>: connection refused", entries[0].err)
        assertEquals("12", entries[1].attrs.getValue("ms").content)
        assertTrue(entries[2].attrs.isEmpty())
        assertEquals("", entries[2].err)
    }

    @Test
    fun anEntryWithoutItsMessageDoesNotDecode() {
        val bad = """[{"at":1,"level":"info","component":"api"}]"""
        runCatching { TalosJson.decodeFromString(ListSerializer(DiagEntry.serializer()), bad) }
            .onSuccess { fail("decoded $it") }
    }

    @Test
    fun decodesTheExportSummary() {
        val summary = TalosJson.decodeFromString(
            DiagExportSummary.serializer(),
            """{"path":"/data/user/0/app/cache/diagnostics/ichor-diagnostics-20261029-1412.zip","bytes":2048,"entries":3,"files":["README.txt","app.json","log.jsonl","platform.json","actions.md"]}""",
        )
        assertEquals(5, summary.files.size)
        assertEquals(3, summary.entries)
    }

    private fun assertNoNull(json: JsonObject, path: String) {
        json.forEach { (k, v) ->
            assertFalse("$path$k is null", v is JsonNull)
            if (v is JsonObject) assertNoNull(v, "$path$k.")
        }
    }
}
