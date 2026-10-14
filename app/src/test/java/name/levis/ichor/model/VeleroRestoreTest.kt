package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.*
import org.junit.Test

class VeleroRestoreTest {
    @Test fun previewDecodesWithDefaults() {
        val preview = TalosJson.decodeFromString(VeleroRestorePreview.serializer(), """
            {"namespace":"velero","backup":"apps-daily-2","phase":"Completed","completedAt":20,"expiresAt":99,
             "storageLocation":"default","locationAvailable":true,"includedNamespaces":["demo"],
             "includeClusterResources":"unset","snapshotVolumes":true,"totalItems":4,"itemsBackedUp":4,
             "volumeSnapshots":2,"canRestore":true,"reasons":[],"restores":[
              {"name":"apps-daily-2-r","phase":"InProgress","startedAt":30,"totalItems":4,"itemsRestored":1,
               "namespaceMapping":{"demo":"demo-restored"},"includedNamespaces":["demo"],"existingResourcePolicy":"update"}]}
        """)
        assertTrue(preview.canRestore)
        assertTrue(preview.snapshotVolumes == true)
        assertEquals("unset", preview.includeClusterResources)
        assertEquals(1, preview.restores.size)
        val restore = preview.restores[0]
        assertTrue(restore.running)
        assertFalse(restore.finished)
        assertEquals("demo-restored", restore.namespaceMapping["demo"])
        // Missing fields fall back to their defaults.
        val bare = TalosJson.decodeFromString(VeleroRestorePreview.serializer(), "{}")
        assertFalse(bare.canRestore)
        assertTrue(bare.reasons.isEmpty() && bare.restores.isEmpty())
        assertNull(bare.snapshotVolumes)
    }

    @Test fun restorePhases() {
        val running = listOf("New", "InProgress", "WaitingForPluginOperations", "FinalizingPartiallyFailed")
        val finished = listOf("Completed", "PartiallyFailed", "Failed", "FailedValidation")
        assertTrue(running.all { VeleroRestoreDetail(phase = it).running && !VeleroRestoreDetail(phase = it).finished })
        assertTrue(finished.all { VeleroRestoreDetail(phase = it).finished && !VeleroRestoreDetail(phase = it).running })
    }

    @Test fun reasonsAreKnownOrKeptAsIs() {
        assertEquals(VeleroRestoreReason.PARTIALLY_FAILED, VeleroRestoreReason.from("partiallyFailed"))
        assertEquals(VeleroRestoreReason.LOCATION_UNAVAILABLE, VeleroRestoreReason.from("locationUnavailable"))
        assertEquals(VeleroRestoreReason.EXPIRED, VeleroRestoreReason.from("expired"))
        assertEquals(VeleroRestoreReason.RESTORE_RUNNING, VeleroRestoreReason.from("restoreRunning"))
        assertEquals(VeleroRestoreReason.ALL_NAMESPACES, VeleroRestoreReason.from("allNamespaces"))
        assertNull(VeleroRestoreReason.from("somethingNew"))
    }

    @Test fun optionsSerializeForTheCore() {
        val options = VeleroRestoreOptions(
            includedNamespaces = listOf("demo"),
            namespaceMapping = mapOf("demo" to "demo-restored"),
            existingResourcePolicy = "update",
            restorePVs = false,
        )
        assertEquals(
            """{"includedNamespaces":["demo"],"namespaceMapping":{"demo":"demo-restored"},""" +
                """"existingResourcePolicy":"update","restorePVs":false}""",
            TalosJson.encodeToString(VeleroRestoreOptions.serializer(), options),
        )
        // Nulls and empties are left out, so the core keeps Velero's defaults.
        assertEquals("{}", TalosJson.encodeToString(VeleroRestoreOptions.serializer(), VeleroRestoreOptions()))
    }
}
