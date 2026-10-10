package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ClusterUpgradeTest {
    private fun plan(json: String) = TalosJson.decodeFromString(ClusterUpgradePlan.serializer(), json)

    @Test
    fun aPartlyUpgradedClusterContinues() {
        val p = plan(
            """{"version":"v1.12.1","image":"ghcr.io/siderolabs/installer:v1.12.1","nodes":[
                {"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","from":"v1.12.1","state":"done","blockers":[],"warnings":[]},
                {"node":"10.0.0.2","hostname":"cp-2","role":"controlplane","from":"v1.12.0","state":"pending","leader":true,"blockers":[],"warnings":["no build of the extension gvisor"]},
                {"node":"10.0.0.3","hostname":"","role":"worker","from":"v1.12.0","state":"pending","blockers":[],"warnings":[]}
               ],"blockers":[],"warnings":[],"drain":false}""",
        )
        assertTrue(p.continues)
        assertFalse(p.upToDate)
        assertTrue(p.canStart)
        assertEquals(listOf("cp-2", "10.0.0.3"), p.pending.map { it.name })
        assertTrue(p.nodes[1].leader && p.nodes[1].controlPlane)
    }

    @Test
    fun blockersAreNamedAndStopTheStart() {
        val p = plan(
            """{"version":"v1.12.1","nodes":[
                {"node":"10.0.0.3","hostname":"w-1","role":"worker","state":"pending","blockers":["the node does not answer"]}
               ],"blockers":["another phone holds the upgrade lock"]}""",
        )
        assertEquals(listOf("another phone holds the upgrade lock", "w-1: the node does not answer"), p.allBlockers())
        assertFalse(p.canStart)
    }

    @Test
    fun nothingLeftIsUpToDate() {
        val p = plan("""{"version":"v1.12.1","nodes":[{"node":"10.0.0.1","state":"done"}]}""")
        assertTrue(p.upToDate)
        assertFalse(p.continues)
        assertFalse(p.canStart)
    }

    @Test
    fun progressDecodes() {
        val p = TalosJson.decodeFromString(
            ClusterUpgradeProgress.serializer(),
            """{"phase":"node","index":1,"total":3,"node":"10.0.0.2","hostname":"cp-2","nodePhase":"rebooting","message":"","at":5,
               "nodes":[{"node":"10.0.0.1","state":"done"},{"node":"10.0.0.2","state":"running"}]}""",
        )
        assertEquals("cp-2", p.name)
        assertEquals(ClusterUpgradeNode.RUNNING, p.nodes[1].state)
    }
}
