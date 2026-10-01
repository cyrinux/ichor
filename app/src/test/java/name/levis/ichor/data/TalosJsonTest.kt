package name.levis.ichor.data

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.health
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Decodes payloads shaped exactly like the Go core output (captured from a live cluster). */
class TalosJsonTest {

    @Test
    fun overviewWithUnreachableAndNotReadyNodes() {
        val json = """
            {"context":"lab","nodes":[
              {"node":"10.0.0.2","hostname":"cp-1","reachable":true,"version":"v1.14.1",
               "arch":"amd64","platform":"metal","role":"controlplane","stage":"running","ready":true,"unmetConditions":[]},
              {"node":"10.0.0.3","hostname":"cp-2","reachable":true,"version":"v1.14.1","arch":"amd64",
               "platform":"metal","role":"controlplane","stage":"running","ready":false,
               "unmetConditions":[{"name":"services","reason":"etcd not healthy"}]},
              {"node":"10.0.0.9","hostname":"10.0.0.9","reachable":false,
               "error":"timed out","version":"","arch":"","platform":"","role":"unknown","stage":"unknown",
               "ready":false,"unmetConditions":[],"futureField":42}
            ]}
        """.trimIndent()

        val overview = TalosJson.decodeFromString(ClusterOverview.serializer(), json)

        assertEquals(
            listOf(NodeHealth.READY, NodeHealth.NOT_READY, NodeHealth.UNREACHABLE),
            overview.nodes.map { it.health },
        )
        assertEquals("etcd not healthy", overview.nodes[1].unmetConditions.single().reason)
        assertNull(overview.nodes[0].error)
    }

    @Test
    fun etcdOverview() {
        val json = """
            {"leaderId":"8e9e05c52164694d","members":[{"id":"8e9e05c52164694d","hostname":"cp-1",
             "peerUrls":["https://10.0.0.2:2380"],"clientUrls":[],"isLearner":false}],
             "statuses":[{"node":"10.0.0.2","memberId":"8e9e05c52164694d","isLeader":true,"isLearner":false,
             "dbSize":467779584,"dbSizeInUse":42336256,"raftIndex":358583460,"raftTerm":856,"version":"3.7.0","errors":[]}],
             "alarms":[]}
        """.trimIndent()

        val etcd = TalosJson.decodeFromString(EtcdOverview.serializer(), json)

        assertEquals(true, etcd.statuses.single().isLeader)
        assertEquals(467_779_584L, etcd.statuses.single().dbSize)
    }
}
