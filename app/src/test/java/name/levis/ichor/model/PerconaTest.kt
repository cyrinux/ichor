package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PerconaTest {
    private val json = """
        {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
         "percona":{"version":"v1","error":"","clusters":[
          {"namespace":"db","name":"down","state":"error","message":"PXC: back-off","health":"critical","reasons":["error","noMember"],
           "pxcSize":3,"pxcReady":0,"proxy":"haproxy","proxySize":2,"proxyReady":0,
           "pods":[{"name":"down-pxc-0","node":"node-3","phase":"Running","ready":false},{"name":"down-pxc-1","node":"","phase":"Pending","ready":false}]},
          {"namespace":"db","name":"crm","state":"initializing","health":"warning","reasons":["members"],"pxcSize":3,"pxcReady":2,
           "proxy":"proxysql","proxySize":2,"proxyReady":2,"pods":[{"name":"crm-pxc-0","node":"node-1","phase":"Running","ready":true}]},
          {"namespace":"db","name":"fresh","state":"initializing","health":"warning","reasons":["initializing"],"pxcSize":1,"pxcReady":1},
          {"namespace":"db","name":"wiki","state":"ready","health":"warning","reasons":["backupStale"],"pxcSize":1,"pxcReady":1,
           "lastBackupAt":1759000000000,"backupSchedules":[{"name":"daily","schedule":"0 2 * * *","keep":7,"storageName":"s3"}]},
          {"namespace":"db","name":"shop","state":"ready","health":"ok","reasons":[],"pxcSize":3,"pxcReady":3,"proxy":"haproxy","proxySize":2,"proxyReady":2},
          {"namespace":"db","name":"old","state":"paused","paused":true,"health":"idle","reasons":[],"pxcSize":3}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesClustersAndBackups() {
        val pxc = services.percona!!
        assertEquals(6, pxc.clusters.size)
        assertEquals(listOf(PerconaReason.ERROR, PerconaReason.NO_MEMBER), pxc.clusters[0].reasonList)
        assertEquals("proxysql", pxc.clusters[1].proxy)
        assertEquals("daily", pxc.clusters[3].backupSchedules.single().name)
        assertEquals(1759000000000, pxc.clusters[3].lastBackupAt)
        assertTrue(pxc.clusters[5].paused)
        assertEquals(listOf(DataServiceKind.LONGHORN, DataServiceKind.PERCONA), services.detected)
    }

    @Test
    fun summary() {
        assertEquals(ServiceSummary(total = 6, attention = 4, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.PERCONA))
    }

    @Test
    fun likelyCauseCountsTheClusterWithAMemberThere() {
        // node-3: Longhorn's not-ready node holds the down cluster's member.
        assertEquals(listOf(LikelyCause("node-3", 1)), services.likelyCauses())
    }

    @Test
    fun alertsSkipInitializing() {
        assertEquals(
            mapOf("percona|db/crm" to DATA_WARNING, "percona|db/down" to DATA_CRITICAL, "percona|db/wiki" to DATA_WARNING),
            dataIssuesOf(services),
        )
    }

    @Test
    fun hintsIncludePercona() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "percona-xtradb", name = "Percona XtraDB Cluster"), InventoryApp(id = "longhorn", name = "Longhorn")))
        assertEquals("longhorn,percona-xtradb", inventory.dataServiceHints())
    }
}
