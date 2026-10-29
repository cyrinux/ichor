package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.*
import org.junit.Test

class CnpgDestroyTest {
    @Test fun previewDecodesVolumesAndBlockers() {
        val preview = TalosJson.decodeFromString(CnpgDestroyPreview.serializer(), """
            {"namespace":"shop","name":"pg","instance":"pg-2","currentPrimary":"pg-1","targetPrimary":"pg-1","podPhase":"Pending","podNode":"",
             "volumes":[{"name":"pg-2","role":"PG_DATA","node":"worker-gone","nodeState":"gone"}],"blockers":[],"warnings":[],"canDestroy":true}
        """)
        assertTrue(preview.canDestroy)
        assertEquals("gone", preview.volumes.single().nodeState)
        val refused = TalosJson.decodeFromString(CnpgDestroyPreview.serializer(), """{"blockers":["primary"]}""")
        assertFalse(refused.canDestroy)
        assertEquals(listOf("primary"), refused.blockers)
    }

    @Test fun removableInstancesSkipThePrimary() {
        val cluster = TalosJson.decodeFromString(CnpgCluster.serializer(), """
            {"namespace":"shop","name":"pg","currentPrimary":"pg-1","targetPrimary":"pg-1",
             "instancePods":[{"name":"pg-3","phase":"Pending"},{"name":"pg-4","node":"worker-a","phase":"Running","ready":true}],
             "failedInstances":["pg-3","pg-1"],"missingInstances":["pg-1","pg-2"]}
        """)
        assertEquals(listOf("pg-2", "pg-3"), cluster.removableInstances)
        assertEquals(listOf("pg-2"), cluster.copy(failedInstances = emptyList(), instancePods = emptyList(), currentPrimary = "pg-9", targetPrimary = "pg-1").removableInstances)
        assertTrue(TalosJson.decodeFromString(CnpgCluster.serializer(), """{"namespace":"a","name":"b"}""").removableInstances.isEmpty())
    }
}
