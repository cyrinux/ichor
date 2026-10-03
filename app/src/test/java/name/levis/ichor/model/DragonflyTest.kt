package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Test

class DragonflyTest {
    private val json = """
        {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
         "dragonfly":{"version":"v1alpha1","error":"","instances":[
          {"namespace":"app","name":"down","phase":"Ready","health":"critical","reasons":["noReady"],"replicas":2,"readyPods":0,"master":"down-0",
           "pods":[{"name":"down-0","node":"node-3","phase":"Running","role":"master","ready":false},{"name":"down-1","node":"","phase":"Pending","role":"replica","ready":false}]},
          {"namespace":"app","name":"queue","phase":"Ready","health":"warning","reasons":["pods"],"replicas":2,"readyPods":1,"master":"queue-0",
           "pods":[{"name":"queue-0","node":"node-1","phase":"Running","role":"master","ready":true},{"name":"queue-1","node":"","phase":"Pending","role":"replica","ready":false}]},
          {"namespace":"app","name":"rolling","phase":"Rolling-update","health":"warning","reasons":["notReady"],"replicas":1,"readyPods":1,"master":"rolling-0",
           "pods":[{"name":"rolling-0","node":"node-1","phase":"Running","role":"master","ready":true}]},
          {"namespace":"app","name":"cache","phase":"Ready","health":"ok","reasons":[],"replicas":2,"readyPods":2,"master":"cache-0",
           "pods":[{"name":"cache-0","node":"node-1","role":"master","ready":true},{"name":"cache-1","node":"node-2","role":"replica","ready":true}]}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesInstancesAndRoles() {
        val df = services.dragonfly!!
        assertEquals(4, df.instances.size)
        assertEquals(listOf(DragonflyReason.NO_READY), df.instances[0].reasonList)
        assertEquals("master", df.instances[3].pods[0].role)
        assertEquals("replica", df.instances[3].pods[1].role)
        assertEquals(listOf(DataServiceKind.LONGHORN, DataServiceKind.DRAGONFLY), services.detected)
    }

    @Test
    fun summary() {
        assertEquals(ServiceSummary(total = 4, attention = 3, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.DRAGONFLY))
    }

    @Test
    fun likelyCauseCountsTheInstanceWithAPodThere() {
        // node-3: Longhorn's not-ready node holds the down instance's master.
        assertEquals(listOf(LikelyCause("node-3", 1)), services.likelyCauses())
    }

    @Test
    fun alertsSkipARollingUpdate() {
        assertEquals(
            mapOf("dragonfly|app/down" to DATA_CRITICAL, "dragonfly|app/queue" to DATA_WARNING),
            dataIssuesOf(services),
        )
    }

    @Test
    fun hintsIncludeDragonfly() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "dragonfly", name = "Dragonfly"), InventoryApp(id = "longhorn", name = "Longhorn")))
        assertEquals("longhorn,dragonfly", inventory.dataServiceHints())
    }
}
