package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Test

class MariaDbTest {
    private val json = """
        {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
         "mariadb":{"version":"v1alpha1","error":"","clusters":[
          {"namespace":"app","name":"down","topology":"replication","health":"critical","reasons":["noReady"],"replicas":2,"readyPods":0,"primary":"down-0",
           "pods":[{"name":"down-0","node":"node-3","phase":"Running","role":"primary","ready":false},{"name":"down-1","node":"","phase":"Pending","role":"replica","ready":false}]},
          {"namespace":"app","name":"forum","topology":"galera","health":"warning","reasons":["pods","galeraRecovery"],"message":"Recovering Galera cluster","replicas":3,"readyPods":2,"primary":"forum-0",
           "pods":[{"name":"forum-0","node":"node-1","role":"primary","ready":true},{"name":"forum-1","node":"node-2","role":"member","ready":true},{"name":"forum-2","node":"node-2","role":"member","ready":false}]},
          {"namespace":"app","name":"nightly","topology":"standalone","health":"warning","reasons":["backupFailed"],"replicas":1,"readyPods":1,"primary":"nightly-0",
           "pods":[{"name":"nightly-0","node":"node-1","role":"primary","ready":true}],"lastBackupAt":1759370000000,"lastBackupFailedAt":1759460000000,"backupSchedule":"0 1 * * *"},
          {"namespace":"app","name":"rollout","topology":"replication","health":"warning","reasons":["pods"],"replicas":2,"readyPods":1,"primary":"rollout-0",
           "pods":[{"name":"rollout-0","node":"node-1","role":"primary","ready":true},{"name":"rollout-1","node":"","phase":"Pending","role":"replica","ready":false}]},
          {"namespace":"app","name":"shop","topology":"replication","health":"ok","reasons":[],"replicas":2,"readyPods":2,"primary":"shop-0",
           "pods":[{"name":"shop-0","node":"node-1","role":"primary","ready":true},{"name":"shop-1","node":"node-2","role":"replica","ready":true}]},
          {"namespace":"app","name":"paused","topology":"standalone","health":"idle","reasons":[],"suspended":true,"replicas":1,"readyPods":1}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesClustersRolesAndBackups() {
        val db = services.mariadb!!
        assertEquals(6, db.clusters.size)
        assertEquals(listOf(MariaDbReason.PODS, MariaDbReason.GALERA_RECOVERY), db.clusters[1].reasonList)
        assertEquals("Recovering Galera cluster", db.clusters[1].message)
        assertEquals(listOf("primary", "member", "member"), db.clusters[1].pods.map { it.role })
        assertEquals("0 1 * * *", db.clusters[2].backupSchedule)
        assertEquals(1759460000000, db.clusters[2].lastBackupFailedAt)
        assertEquals(true, db.clusters[5].suspended)
        assertEquals(listOf(DataServiceKind.LONGHORN, DataServiceKind.MARIADB), services.detected)
    }

    @Test
    fun summary() {
        assertEquals(ServiceSummary(total = 6, attention = 4, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.MARIADB))
    }

    @Test
    fun likelyCauseCountsTheClusterWithAPodThere() {
        // node-3: Longhorn's not-ready node holds the down cluster's primary.
        assertEquals(listOf(LikelyCause("node-3", 1)), services.likelyCauses())
    }

    @Test
    fun alertsSkipAReplicaRollingOut() {
        assertEquals(
            mapOf("mariadb|app/down" to DATA_CRITICAL, "mariadb|app/forum" to DATA_WARNING, "mariadb|app/nightly" to DATA_WARNING),
            dataIssuesOf(services),
        )
    }

    @Test
    fun hintsIncludeMariaDb() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "mariadb", name = "MariaDB"), InventoryApp(id = "longhorn", name = "Longhorn")))
        assertEquals("longhorn,mariadb", inventory.dataServiceHints())
    }
}
