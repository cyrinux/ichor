package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeLoadBalancingTest {
    private val json = """
        {"controller":"metallb","controllers":["metallb","kube-vip"],
         "pools":[{"name":"default-pool","controller":"metallb","addresses":["192.0.2.240/31"],"total":2,"assigned":2,"available":0,
                   "countsKnown":true,"autoAssign":true,"namespaces":[],"advertisedBy":["l2:default"],"services":["dns/blocky"],"level":"critical"},
                  {"name":"lab-pool","controller":"metallb","addresses":["198.51.100.16-198.51.100.23"],"total":8,"available":8,"autoAssign":false,
                   "namespaces":["lab"],"advertisedBy":[],"services":[],"level":"warning"}],
         "advertisements":[{"name":"default","kind":"l2","pools":["default-pool"],"interfaces":["eth0"]}],
         "bgpPeers":[{"name":"upstream-router","peerAddress":"192.0.2.1","peerASN":64512,"myASN":64513,
                      "sessions":[{"node":"demo-worker-2","up":false}],"sessionsKnown":true,"level":"critical"}],
         "configErrors":[],
         "services":[{"namespace":"demo","name":"hello-ichor-public","addresses":[],"pending":true,"cause":"poolExhausted","level":"critical"},
                     {"namespace":"networking","name":"traefik","addresses":["192.0.2.240"],"pool":"default-pool","announcedBy":"demo-worker-1","level":"ok"}],
         "kubeVip":{"ranges":[],"leases":[{"name":"plndr-cp-lock","namespace":"kube-system","holder":"demo-cp-1","renewed":1}]},
         "partialAccess":false,"futureField":1}
    """.trimIndent()

    @Test
    fun decodesTheCoreAnswer() {
        val lb = TalosJson.decodeFromString(KubeLoadBalancing.serializer(), json)

        assertEquals(listOf("metallb", "kube-vip"), lb.controllers)
        assertEquals(StorageLevel.CRITICAL, lb.pools[0].level)
        assertEquals(1f, lb.pools[0].used)
        assertFalse(lb.pools[1].autoAssign)
        assertEquals(listOf("demo/hello-ichor-public"), lb.pending.map { it.key })
        assertEquals(LbCause.POOL_EXHAUSTED, LbCause.of(lb.pending[0].cause))
        assertFalse(lb.bgpPeers[0].sessions[0].up)
        assertEquals("demo-cp-1", lb.kubeVip?.leases?.single()?.holder)
        assertFalse(lb.empty)
    }

    @Test
    fun withoutControllerOrServiceItIsEmpty() {
        val lb = TalosJson.decodeFromString(KubeLoadBalancing.serializer(), """{"controllers":[],"services":[],"kubeVip":null}""")

        assertTrue(lb.empty)
        assertNull(lb.kubeVip)
    }

    @Test
    fun anUnknownCauseReadsAsUnknownButNotAsACheckupReason() {
        assertEquals(LbCause.UNKNOWN, LbCause.of("somethingNew"))
        assertNull(LbCause.ofReason("somethingNew"))
        assertNull(LbCause.ofReason(""))
        assertEquals(LbCause.POOL_NOT_ADVERTISED, LbCause.ofReason("poolNotAdvertised"))
    }

    @Test
    fun kubeVipIsAKnownController() {
        assertEquals(LbController.KUBE_VIP, LbController.of("kube-vip"))
    }
}
