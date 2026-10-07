package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate

class BackupTest {
    private val red = 0xFFAA0000.toInt()

    @Test
    fun backupClustersKeepsOnlyStoredClusters() {
        val clusters = backupClusters(
            fingerprints = listOf("aaaa", "bbbb", ""),
            names = mapOf("aaaa" to "Home", "gone" to "Old"),
            colors = mapOf("aaaa" to red, "bbbb" to 1),
            vpnOnly = setOf("bbbb", "gone"),
            wakeOnLan = mapOf(
                wolKey("aaaa", "10.0.0.2") to WolTarget("aa:bb:cc:dd:ee:ff", "10.0.0.255", 7),
                wolKey("gone", "10.0.0.3") to WolTarget("aa:bb:cc:dd:ee:01"),
            ),
            kubeServers = mapOf("bbbb" to "https://k8s.lan:6443", "gone" to "https://old.lan"),
        )

        assertEquals(setOf("aaaa", "bbbb"), clusters.keys)
        assertEquals(BackupCluster("Home", 0xAA0000, false, mapOf("10.0.0.2" to BackupWolTarget("aa:bb:cc:dd:ee:ff", "10.0.0.255", 7))), clusters["aaaa"])
        assertEquals(BackupCluster(null, 1, true, kubeServer = "https://k8s.lan:6443"), clusters["bbbb"])
    }

    @Test
    fun restoredClustersRoundTrip() {
        val wol = mapOf(wolKey("aaaa", "10.0.0.2") to WolTarget("aa:bb:cc:dd:ee:ff", "", 9))
        val servers = mapOf("aaaa" to "https://k8s.lan:6443")
        val saved = backupClusters(listOf("aaaa"), mapOf("aaaa" to "Home"), mapOf("aaaa" to red), setOf("aaaa"), wol, servers)

        val restored = restoredClusters(saved, listOf("aaaa"))

        assertEquals(RestoredClusters(mapOf("aaaa" to "Home"), mapOf("aaaa" to red), setOf("aaaa"), wol, servers), restored)
    }

    @Test
    fun restoredClustersDropsUnknownAndInvalidEntries() {
        val restored = restoredClusters(
            mapOf(
                "aaaa" to BackupCluster(
                    name = "   ",
                    color = 0x00123456,
                    wakeOnLan = mapOf(
                        "10.0.0.2" to BackupWolTarget("not a mac"),
                        "10.0.0.3" to BackupWolTarget("AA-BB-CC-DD-EE-FF", port = 70000),
                        "10.0.0.4" to BackupWolTarget("aabbccddeeff", broadcast = "a|b"),
                        "10.0.0.5" to BackupWolTarget("AA-BB-CC-DD-EE-FF"),
                    ),
                    kubeServer = "  ",
                ),
                "other" to BackupCluster(name = "Elsewhere", vpnOnly = true, kubeServer = "https://k8s.lan"),
            ),
            fingerprints = listOf("aaaa"),
        )

        assertTrue(restored.names.isEmpty())
        assertEquals(mapOf("aaaa" to 0xFF123456.toInt()), restored.colors)
        assertTrue(restored.vpnOnly.isEmpty())
        assertTrue(restored.kubeServers.isEmpty())
        assertEquals(mapOf(wolKey("aaaa", "10.0.0.5") to WolTarget("aa:bb:cc:dd:ee:ff")), restored.wakeOnLan)
    }

    @Test
    fun payloadRoundTripsThroughJson() {
        val payload = BackupPayload(
            platform = "android",
            createdAt = 1_790_000_000,
            talosconfig = "context: lab\n",
            activeContextIndex = 1,
            settings = BackupSettings(themeMode = "dark", language = "fr", remoteAppIcons = true, monitorIntervalMinutes = 30),
            clusters = mapOf("aaaa" to BackupCluster("Home", red, true)),
        )

        val json = TalosJson.encodeToString(BackupPayload.serializer(), payload)

        assertEquals(payload, TalosJson.decodeFromString(BackupPayload.serializer(), json))
        assertTrue("unset settings are left out", "\"privacyMask\"" !in json)
        assertTrue("the Go core requires the format", "\"format\":1" in json)
        assertTrue("iOS reads the same key", "\"remoteAppIcons\":true" in json)
    }

    @Test
    fun formatTwoOnlyWithAKubeconfig() {
        assertEquals(1, backupFormat(null))
        assertEquals(1, backupFormat("  "))
        assertEquals(2, backupFormat("apiVersion: v1\nkind: Config\n"))
    }

    @Test
    fun kubeconfigPayloadKeepsAnEmptyTalosconfig() {
        val kube = "apiVersion: v1\nkind: Config\n"
        val payload = BackupPayload(format = backupFormat(kube), platform = "android", talosconfig = "", kubeconfig = kube)

        val json = TalosJson.encodeToString(BackupPayload.serializer(), payload)

        assertTrue("\"format\":2" in json)
        assertTrue("older apps require the field", "\"talosconfig\":\"\"" in json)
        assertEquals(payload, TalosJson.decodeFromString(BackupPayload.serializer(), json))
    }

    @Test
    fun aTalosOnlyPayloadHasNoKubeconfigKey() {
        val json = TalosJson.encodeToString(BackupPayload.serializer(), BackupPayload(talosconfig = "context: lab\n"))

        assertTrue("\"format\":1" in json)
        assertTrue("\"kubeconfig\"" !in json)
        assertNull(TalosJson.decodeFromString(BackupPayload.serializer(), json).kubeconfig)
    }

    @Test
    fun decodesAnIosPayload() {
        val json = """
            {"format":1,"platform":"ios","createdAt":1790000000,"talosconfig":"context: lab\n",
             "activeContextIndex":0,"settings":{"themeMode":"light","privacyMask":true,"monitorAlerts":false,"future":1},
             "clusters":{"aaaa":{"name":"Home","color":16711680,"vpnOnly":false}}}
        """.trimIndent()

        val payload = TalosJson.decodeFromString(BackupPayload.serializer(), json)

        assertEquals("light", payload.settings.themeMode)
        assertNull(payload.settings.language)
        assertNull(payload.settings.monitorIntervalMinutes)
        assertNull("an older backup keeps the current setting", payload.settings.remoteAppIcons)
        assertEquals(emptyMap<String, BackupWolTarget>(), payload.clusters.getValue("aaaa").wakeOnLan)
    }

    @Test
    fun fileName() {
        assertEquals("ichor-2026-10-02.ichorbackup", backupFileName(LocalDate.of(2026, 10, 2)))
    }

    @Test
    fun looksLikeBackupChecksTheMagic() {
        assertTrue(looksLikeBackup("ICHORBAK".toByteArray() + ByteArray(60)))
        assertTrue(looksLikeBackup("ICHORBAK".toByteArray()))
        assertFalse(looksLikeBackup("ICHORBA".toByteArray()))
        assertFalse(looksLikeBackup("PK\u0003\u0004 a zip".toByteArray()))
        assertFalse(looksLikeBackup(ByteArray(0)))
    }
}
