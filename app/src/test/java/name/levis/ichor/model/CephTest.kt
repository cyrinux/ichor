package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Test

class CephTest {
    private val json = """
        {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
         "ceph":{"version":"v1","error":"","clusters":[
          {"namespace":"ceph-down","name":"down","phase":"Ready","cephHealth":"HEALTH_ERR","health":"critical","reasons":["healthErr","noOSD"],
           "checks":[{"name":"OSD_DOWN","severity":"HEALTH_WARN","message":"2 osds down"}],"bytesTotal":1000,"bytesUsed":100,
           "osdsUp":0,"osdsTotal":2,"monsReady":1,"monsTotal":1,"notReadyNodes":["node-3"],"version":"19.2.3-0","external":false},
          {"namespace":"storage","name":"ceph","phase":"Ready","cephHealth":"HEALTH_WARN","health":"warning","reasons":["healthWarn","nearFull"],
           "checks":[{"name":"OSD_NEARFULL","severity":"HEALTH_WARN","message":"1 nearfull osd(s)"}],"bytesTotal":1000,"bytesUsed":900,
           "osdsUp":3,"osdsTotal":3,"monsReady":3,"monsTotal":3,"notReadyNodes":[],"version":"19.2.3-0"},
          {"namespace":"ceph-new","name":"new","phase":"Progressing","health":"warning","reasons":["notReady"]},
          {"namespace":"ceph-ext","name":"remote","phase":"Connected","cephHealth":"HEALTH_OK","health":"ok","reasons":[],"external":true}],
         "pools":[
          {"namespace":"storage","name":"broken","kind":"blockPool","phase":"Failure","health":"critical"},
          {"namespace":"storage","name":"fs","kind":"filesystem","phase":"Progressing","health":"warning"},
          {"namespace":"storage","name":"s3","kind":"objectStore","phase":"Ready","health":"ok"}],
         "osds":[
          {"namespace":"ceph-down","id":"0","pod":"rook-ceph-osd-0-abc","node":"node-3","phase":"Running","ready":false},
          {"namespace":"storage","id":"1","pod":"rook-ceph-osd-1-abc","node":"node-1","phase":"Running","ready":true}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesClustersPoolsAndOsds() {
        val ceph = services.ceph!!
        assertEquals(4, ceph.clusters.size)
        assertEquals(listOf(CephReason.HEALTH_ERR, CephReason.NO_OSD), ceph.clusters[0].reasonList)
        assertEquals("OSD_NEARFULL", ceph.clusters[1].checks.single().name)
        assertEquals(0.9, ceph.clusters[1].usedFraction, 1e-9)
        assertEquals(0.0, ceph.clusters[2].usedFraction, 0.0)
        assertEquals(true, ceph.clusters[3].external)
        assertEquals(listOf("blockPool", "filesystem", "objectStore"), ceph.pools.map { it.kind })
        assertEquals("node-3", ceph.osds[0].node)
        assertEquals(listOf(DataServiceKind.LONGHORN, DataServiceKind.CEPH), services.detected)
    }

    @Test
    fun summaryCountsPoolsToo() {
        // Three clusters and two pools need a look; the total counts clusters.
        assertEquals(ServiceSummary(total = 4, attention = 5, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.CEPH))
    }

    @Test
    fun likelyCauseCountsTheClusterWithAnOsdThere() {
        // node-3: Longhorn's not-ready node runs the down cluster's OSD.
        assertEquals(listOf(LikelyCause("node-3", 1)), services.likelyCauses())
    }

    @Test
    fun alertsSkipAClusterBeingSetUp() {
        assertEquals(
            mapOf("ceph|ceph-down/down" to DATA_CRITICAL, "ceph|blockPool/storage/broken" to DATA_CRITICAL, "ceph|storage/ceph" to DATA_WARNING),
            dataIssuesOf(services),
        )
    }

    @Test
    fun hintsIncludeRook() {
        val inventory = Inventory(apps = listOf(InventoryApp(id = "rook", name = "Rook Ceph"), InventoryApp(id = "longhorn", name = "Longhorn")))
        assertEquals("longhorn,rook", inventory.dataServiceHints())
    }
}
