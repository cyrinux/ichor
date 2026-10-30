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
        assertTrue(KubeWorkload("StatefulSet", "ns", "a").hasHistory)
        assertTrue(KubeWorkload("DaemonSet", "ns", "a").hasHistory)
        assertTrue(KubeWorkload("Deployment", "ns", "a").canPauseRollout)
        assertFalse(KubeWorkload("StatefulSet", "ns", "a").canPauseRollout)
    }

    @Test
    fun decodesAStatefulSetHistoryAndItsDiff() {
        val list = TalosJson.decodeFromString(
            KubeRevisionList.serializer(),
            """{"kind":"StatefulSet","manual":true,"paused":false,"revisions":[{"revision":5,"replicaSet":"db-5a1b2","created":0,"images":["postgres:16"],"changeCause":"","replicas":0,"current":false}]}""",
        )
        assertEquals("StatefulSet", list.kind)
        assertTrue(list.manual)
        assertEquals("db-5a1b2", list.revisions.single().replicaSet)

        val diff = TalosJson.decodeFromString(
            KubeRevisionDiff.serializer(),
            """{"diff":"--- revision 7 (current)\n+++ revision 5\n@@ -1,1 +1,1 @@\n-image: postgres:17\n+image: postgres:16\n","from":"revision 7 (current)","to":"revision 5",
               "fromRevision":7,"toRevision":5,"images":{"from":["postgres:17","sidecar:1"],"to":["postgres:16","sidecar:1"]}}""",
        )
        assertEquals(5, diff.toRevision)
        assertEquals(listOf(DiffLine.Kind.HUNK, DiffLine.Kind.REMOVED, DiffLine.Kind.ADDED), parseDiffLines(diff.diff).map { it.kind })
        // Only the images that change, container by container.
        assertEquals(listOf(ImageChange("postgres:17", "postgres:16")), diff.images.changes())
        assertEquals(listOf(ImageChange("a:1", "")), KubeRevisionImages(listOf("x:1", "a:1"), listOf("x:1")).changes())
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
