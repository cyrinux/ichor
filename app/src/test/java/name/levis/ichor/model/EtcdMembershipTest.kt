package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class EtcdMembershipTest {

    private fun decode(json: String) = TalosJson.decodeFromString(EtcdOverview.serializer(), json)

    @Test
    fun learnerAndFailedJoinShowTheirPhase() {
        val etcd = decode(
            """
            {"leaderId":"a1","members":[],"alarms":[],"statuses":[
              {"node":"10.0.0.1","memberId":"a1","isLeader":true,"errors":[],"membershipPhase":"member"},
              {"node":"10.0.0.3","memberId":"c3","isLearner":true,"errors":[],"membershipPhase":"learner"},
              {"node":"10.0.0.4","error":"etcd is not running","memberId":"","errors":[],
               "membershipPhase":"joining","membershipError":"too many learner members in cluster"}
            ]}
            """.trimIndent(),
        )

        assertEquals(listOf(null, "learner", "joining"), etcd.statuses.map { it.unsettledPhase })
        assertEquals("too many learner members in cluster", etcd.statuses[2].membershipError)
    }

    @Test
    fun talos114HasNoPhase() {
        val etcd = decode("""{"leaderId":"a1","members":[],"alarms":[],"statuses":[{"node":"10.0.0.1","memberId":"a1","errors":[]}]}""")

        assertNull(etcd.statuses.single().membershipPhase)
        assertNull(etcd.statuses.single().unsettledPhase)
    }

    @Test
    fun blankPhaseIsSettled() {
        assertNull(EtcdNodeStatus(node = "10.0.0.1", membershipPhase = " ").unsettledPhase)
        assertEquals("left", EtcdNodeStatus(node = "10.0.0.1", membershipPhase = "left").unsettledPhase)
    }
}
