package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PluginBackupTest {
    private val plugins = BackupPlugins(
        listOf(
            BackupPlugin("widgets", enabled = true, yaml = "schemaVersion: ichor.dev/plugin/v1alpha1\nid: widgets\n"),
            BackupPlugin("gadgets", enabled = false, yaml = "schemaVersion: ichor.dev/plugin/v1alpha1\nid: gadgets\n"),
        ),
    )
    private val record = GitProposalRecord(repoURL = "https://git.example.org/example-org/cluster-config", number = 12, title = "Raise ingest replicas", state = "open")

    // Format 4 shipped without plugins, and that app ignores the key: a backup with plugins is
    // format 5, so it is refused there rather than restored without them.
    @Test
    fun pluginsNeedFormat5() {
        assertEquals(3, backupFormatFor(null, null))
        assertEquals(3, backupFormatFor(null, BackupPlugins()))
        assertEquals(4, backupFormatFor(mapOf("fp" to listOf(record)), BackupPlugins()))
        assertEquals(5, backupFormatFor(null, plugins))
        assertEquals(5, backupFormatFor(mapOf("fp" to listOf(record)), plugins))
    }

    @Test
    fun pluginsTravelWithTheBackupAsTheCoreExportsThem() {
        val payload = BackupPayload(format = backupFormatFor(null, plugins), talosconfig = "context: lab\n", plugins = plugins)
        val json = TalosJson.encodeToString(BackupPayload.serializer(), payload)
        // The iOS app reads the same object: {"plugins":{"plugins":[{"id","enabled","yaml"}]}}.
        assertTrue(json, json.contains("\"plugins\":{\"plugins\":[{\"id\":\"widgets\",\"enabled\":true,"))

        val back = TalosJson.decodeFromString(BackupPayload.serializer(), json)
        assertEquals(plugins, restoredPlugins(back))
        assertNull("a format 4 backup has none", restoredPlugins(back.copy(format = 4)))
        assertNull("an empty list restores nothing", restoredPlugins(back.copy(plugins = BackupPlugins())))
    }

    @Test
    fun theCoreAnswersDecode() {
        val restored = TalosJson.decodeFromString(
            PluginRestoreResult.serializer(),
            """{"restored":["widgets"],"errors":[{"id":"gadgets","error":"plugin.yaml:3: unknown field"},{"id":"empty"}]}""",
        )
        assertEquals(listOf("widgets"), restored.restored)
        assertEquals("gadgets: plugin.yaml:3: unknown field\nempty", restored.skippedText)

        val qr = TalosJson.decodeFromString(PluginQrPayload.serializer(), """{"kind":"url","url":"https://example.org/p.yaml"}""")
        assertEquals(PLUGIN_QR_URL, qr.kind)
        assertEquals("https://example.org/p.yaml", qr.url)
    }
}
