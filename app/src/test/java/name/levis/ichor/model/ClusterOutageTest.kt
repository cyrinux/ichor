package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ClusterOutageTest {

    private fun down(addr: String, kind: String? = "network", error: String = "unreachable: no route to host") =
        NodeOverview(node = addr, hostname = addr, reachable = false, error = error, errorKind = kind)

    private fun up(addr: String) = NodeOverview(node = addr, hostname = addr, reachable = true, ready = true)

    private fun outage(vararg nodes: NodeOverview) = ClusterOverview("ctx", nodes.toList()).outage

    @Test
    fun anyNodeAnsweringIsNoOutage() {
        assertNull(outage(up("a"), down("b"), down("c")))
    }

    @Test
    fun noNodesIsNoOutage() {
        assertNull(outage())
    }

    @Test
    fun allNetworkFailuresMeanThePhoneIsOffTheClusterNetwork() {
        val o = outage(down("a"), down("b", error = "timed out (is the endpoint reachable from this network?)"))
        assertEquals(OutageCause.NETWORK, o?.cause)
        assertEquals(2, o?.nodes)
    }

    @Test
    fun certificateOrRoleFailuresAreCredentials() {
        assertEquals(OutageCause.CREDENTIALS, outage(down("a", kind = "tls"), down("b", kind = "auth"))?.cause)
    }

    @Test
    fun mixedOrUnknownKindsAreOther() {
        assertEquals(OutageCause.OTHER, outage(down("a"), down("b", kind = "auth"))?.cause)
        assertEquals(OutageCause.OTHER, outage(down("a", kind = null))?.cause)
    }

    @Test
    fun theSameErrorFromEveryNodeIsShownOnce() {
        val o = outage(down("a"), down("b"), down("c", error = "unreachable: connection refused"))
        assertEquals(listOf("unreachable: no route to host", "unreachable: connection refused"), o?.errors)
    }
}
