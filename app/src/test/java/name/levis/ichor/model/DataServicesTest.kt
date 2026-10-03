package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DataServicesTest {
    // Shaped like KubeDataServices' answer on a cluster with one node down.
    private val json = """
        {"longhorn":{"version":"v1beta2","error":"","volumes":[
          {"name":"pvc-1","namespace":"longhorn-system","pvcNamespace":"app","pvcName":"search","state":"detached","robustness":"faulted","health":"critical",
           "replicasDesired":1,"replicasHealthy":0,"rebuilding":0,"replicaNodes":["node-3"],"node":"","size":10,"actualSize":5,"lastBackupAt":0},
          {"name":"pvc-2","pvcNamespace":"app","pvcName":"db","state":"attached","robustness":"healthy","health":"ok","replicasDesired":3,"replicasHealthy":3,"replicaNodes":["node-1","node-2","node-3"]},
          {"name":"pvc-3","state":"detached","robustness":"unknown","health":"idle","replicaNodes":["node-1"]}],
         "nodes":[{"name":"node-1","ready":true,"schedulable":true,"disks":[{"path":"/var/lib/longhorn","schedulable":true,"available":1,"maximum":2,"scheduled":1}]},
                  {"name":"node-3","ready":false,"schedulable":false,"disks":[]}],
         "backupTargets":[{"name":"default","url":"s3://b@garage/","available":true,"message":""}]},
         "garage":{"error":"","instances":[
          {"namespace":"storage","name":"garage","pod":"garage-a","pods":3,"podsReady":2,"version":"v2.3.0","status":"degraded",
           "message":"1 node down (zone z3, tags node-3)","storageNodes":3,"storageNodesUp":2,"partitions":256,"partitionsQuorum":256,"partitionsAllOk":170,
           "resyncQueue":12,"resyncErrors":10,"tableSyncQueue":4,"layoutVersion":3,"source":"cli-json","futureField":true,
           "nodes":[{"id":"aa","hostname":"","zone":"z3","tags":["node-3"],"kubeNode":"","storage":true,"up":false,"lastSeenSecs":-1,"resyncQueue":-1,"resyncErrors":-1,"tableSyncQueue":-1,"statsError":"Not connected"},
                    {"id":"bb","hostname":"garage-a","zone":"z1","tags":["node-1"],"kubeNode":"node-1","storage":true,"up":true,"lastSeenSecs":-1,"dataAvail":1,"dataTotal":2,"resyncQueue":6,"resyncErrors":5,"tableSyncQueue":2}]},
          {"namespace":"nas","name":"garage-nas","pods":1,"podsReady":1,"status":"healthy","source":"cli-json"}]},
         "cnpg":{"version":"v1","error":"","clusters":[
          {"namespace":"app","name":"down-db","phase":"Waiting for the instances to become active","health":"critical","hibernated":false,"reasons":["noInstance","someNewReason"],
           "instances":2,"readyInstances":0,"currentPrimary":"down-db-1","targetPrimary":"down-db-1",
           "instancePods":[{"name":"down-db-1","node":"node-3","phase":"Running","role":"primary","ready":false},{"name":"down-db-2","node":"","phase":"Pending","role":"","ready":false}],
           "archiving":"ok","lastBackup":"failed","backupMethod":"plugin","objectStore":"garage-store","scheduled":true,
           "lastSuccessfulBackupAt":1000,"lastFailedBackupAt":2000,"firstRecoverabilityAt":500},
          {"namespace":"app","name":"ok-db","phase":"Cluster in healthy state","health":"ok","reasons":[],"instances":2,"readyInstances":2,"archiving":"off","lastBackup":"ok"}]}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesTheGoAnswer() {
        val lh = services.longhorn!!
        assertEquals(3, lh.volumes.size)
        assertEquals(ServiceHealth.CRITICAL, lh.volumes[0].serviceHealth)
        assertEquals("app/search", lh.volumes[0].label)
        assertEquals("pvc-3", lh.volumes[2].label)

        val garage = services.garage!!.instances[0]
        assertEquals(GarageState.DEGRADED, garage.state)
        assertTrue(garage.detailed)
        assertEquals("node-3", garage.nodes[0].label)
        assertEquals("garage-a", garage.nodes[1].label)

        val down = services.cnpg!!.clusters[0]
        assertEquals(listOf(CnpgReason.NO_INSTANCE), down.reasonList)
        assertEquals(1000L, down.lastSuccessAt)
        assertEquals(500L, down.recoverableAt)
        assertEquals(listOf(DataServiceKind.LONGHORN, DataServiceKind.GARAGE, DataServiceKind.CNPG), services.detected)
    }

    @Test
    fun emptyAnswerHasNoSection() {
        val none = TalosJson.decodeFromString(DataServices.serializer(), "{}")
        assertTrue(none.detected.isEmpty())
        assertNull(none.summary(DataServiceKind.LONGHORN))
        assertEquals(ServiceHealth.OK, none.worst)
    }

    @Test
    fun unknownWireValues() {
        assertEquals(ServiceHealth.UNKNOWN, ServiceHealth.from("bogus"))
        assertEquals(ServiceHealth.UNKNOWN, ServiceHealth.from(""))
        assertEquals(GarageState.UNKNOWN, GarageState.from("weird"))
        assertNull(CnpgReason.from("someNewReason"))
    }

    @Test
    fun summaries() {
        // The faulted volume and the node that is not ready.
        assertEquals(ServiceSummary(total = 3, attention = 2, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.LONGHORN))
        assertEquals(ServiceSummary(total = 2, attention = 1, health = ServiceHealth.WARNING), services.summary(DataServiceKind.GARAGE))
        assertEquals(ServiceSummary(total = 2, attention = 1, health = ServiceHealth.CRITICAL), services.summary(DataServiceKind.CNPG))
        assertEquals(ServiceHealth.CRITICAL, services.worst)
        assertEquals(1, services.cnpg!!.pendingInstances)
    }

    @Test
    fun summaryOfAnUnreadableSystem() {
        val broken = LonghornStatus(error = "Kubernetes API: permission denied")
        assertEquals(ServiceHealth.UNKNOWN, broken.summary().health)
        assertEquals("Kubernetes API: permission denied", broken.summary().error)
    }

    @Test
    fun idleOnlyIsOk() {
        val idle = LonghornStatus(volumes = listOf(LonghornVolume(name = "v", state = "detached", health = "idle")))
        assertEquals(ServiceHealth.OK, idle.summary().health)
        assertEquals(0, idle.summary().attention)
    }

    @Test
    fun hintsFromTheInventory() {
        fun app(id: String) = InventoryApp(id = id, name = id)
        val inventory = Inventory(apps = listOf(app("cloudnative-pg"), app("nginx"), app("longhorn")))
        assertEquals("longhorn,cloudnative-pg", inventory.dataServiceHints())
        assertEquals("", Inventory(apps = listOf(app("nginx"))).dataServiceHints())
    }

    @Test
    fun likelyCausePointsToTheNodeThatIsDown() {
        // node-3 is not ready for Longhorn: the faulted volume, the Garage cluster missing a node
        // tagged node-3 and the Postgres cluster with an instance there all point to it.
        assertEquals(listOf(LikelyCause("node-3", 3)), services.likelyCauses())
        // A node reported down elsewhere (Talos) that explains nothing is not listed.
        assertEquals(listOf(LikelyCause("node-3", 3)), services.likelyCauses(setOf("node-9")))
    }

    @Test
    fun noLikelyCauseWithoutDownNodes() {
        val longhorn = services.longhorn!!
        val healthy = services.copy(longhorn = longhorn.copy(nodes = longhorn.nodes.map { it.copy(ready = true) }))
        assertTrue(healthy.likelyCauses().isEmpty())
    }

    @Test
    fun volumeFilters() {
        val volumes = services.longhorn!!.volumes
        assertEquals(listOf("pvc-1"), volumes.filtered(VolumeFilter.PROBLEMS, "").map { it.name })
        assertEquals(listOf("pvc-1", "pvc-3"), volumes.filtered(VolumeFilter.DETACHED, "").map { it.name })
        assertEquals(listOf("pvc-2"), volumes.filtered(VolumeFilter.ALL, "APP/DB").map { it.name })
    }

    @Test
    fun clusterFilters() {
        val clusters = services.cnpg!!.clusters
        assertEquals(listOf("down-db"), clusters.filtered(problemsOnly = true, query = "").map { it.name })
        assertEquals(listOf("ok-db"), clusters.filtered(problemsOnly = false, query = "ok").map { it.name })
        assertFalse(clusters.filtered(problemsOnly = false, query = "").isEmpty())
    }
}
