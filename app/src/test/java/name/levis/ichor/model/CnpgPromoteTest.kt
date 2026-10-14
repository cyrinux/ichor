package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.*
import org.junit.Test

class CnpgPromoteTest {
    @Test fun unknownLagRemainsNullable() {
        val preview = TalosJson.decodeFromString(CnpgPromotePreview.serializer(),
            """{"currentPrimary":"pg-1","instance":"pg-2","lagBytes":null,"ready":true,"canPromote":true,"warnings":["lagUnknown"]}""")
        assertNull(preview.lagBytes)
        assertTrue(preview.canPromote)
        assertTrue("lagUnknown" in preview.warnings)
        val known = TalosJson.decodeFromString(CnpgPromotePreview.serializer(), """{"lagBytes":0}""")
        assertEquals(0L, known.lagBytes)
    }
    @Test fun replicaPickerExcludesPrimaryAndUnreadyPods() {
        val cluster = TalosJson.decodeFromString(CnpgCluster.serializer(), """
            {"namespace":"shop","name":"pg","currentPrimary":"pg-1","targetPrimary":"pg-1","instancePods":[
             {"name":"pg-1","ready":true},{"name":"pg-2","ready":true},{"name":"pg-3","ready":false}]}
        """)
        assertEquals(listOf("pg-2"), cluster.promotablePods.map { it.name })
        assertTrue(cluster.copy(phase="Primary instance is being restarted in-place").promotablePods.isEmpty())
        assertTrue(cluster.copy(hibernated=true).promotablePods.isEmpty())
        assertTrue(cluster.copy(targetPrimary="pg-2").promotablePods.isEmpty())
    }
}
