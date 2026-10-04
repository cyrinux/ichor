package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeActionsTest {

    @Test
    fun decodesRevisions() {
        val list = TalosJson.decodeFromString(
            KubeRevisionList.serializer(),
            """{"revisions":[{"revision":7,"replicaSet":"web-7d9c6","created":1700000000000,"images":["web:2"],"changeCause":"","replicas":3,"current":true},
                             {"revision":6,"replicaSet":"web-5b8f4","created":1690000000000,"images":null,"changeCause":"bump","replicas":0,"current":false}]}""",
        )
        assertEquals(listOf(7, 6), list.revisions.map { it.revision })
        assertTrue(list.revisions[0].current)
        assertTrue(list.revisions[1].images.isEmpty()) // null from Go
        assertEquals("bump", list.revisions[1].changeCause)
        assertTrue(TalosJson.decodeFromString(KubeRevisionList.serializer(), """{"revisions":null}""").revisions.isEmpty())
    }

    @Test
    fun onlyDeploymentsAndStatefulSetsScale() {
        assertTrue(KubeWorkload("Deployment", "ns", "a").canScale)
        assertTrue(KubeWorkload("StatefulSet", "ns", "a").canScale)
        assertFalse(KubeWorkload("DaemonSet", "ns", "a").canScale)
        assertTrue(KubeWorkload("Deployment", "ns", "a").hasHistory)
        assertFalse(KubeWorkload("StatefulSet", "ns", "a").hasHistory)
    }

    @Test
    fun replicaBounds() {
        assertEquals(0, clampReplicas(-1))
        assertEquals(5, clampReplicas(5))
        assertEquals(MAX_SCALE_REPLICAS, clampReplicas(MAX_SCALE_REPLICAS + 1))
        assertTrue(scaleNeedsTypedName(0))
        assertFalse(scaleNeedsTypedName(1))
        // A scale-down is confirmed; 0 has its own typed confirmation, a scale-up none.
        assertTrue(scaleNeedsConfirm(3, 2))
        assertFalse(scaleNeedsConfirm(3, 0))
        assertFalse(scaleNeedsConfirm(3, 4))
        assertFalse(scaleNeedsConfirm(3, 3))
    }

    @Test
    fun containersFromTheApiError() {
        assertEquals(
            listOf("app", "sidecar", "init-db"),
            containersToChoose("a container name must be specified for pod web-1, choose one of: [app sidecar] or one of the init containers: [init-db]"),
        )
        assertEquals(listOf("app", "proxy"), containersToChoose("a container name must be specified for pod web-1, choose one of: [app proxy]"))
        assertTrue(containersToChoose("previous terminated container \"app\" in pod \"web-1\" not found").isEmpty())
        assertTrue(containersToChoose("").isEmpty())
    }

    @Test
    fun splitsLogLines() {
        assertEquals(listOf("a", "b"), logLines("a\nb\n"))
        assertEquals(listOf("a", "", "b"), logLines("a\n\nb"))
        assertTrue(logLines("").isEmpty())
    }
}
