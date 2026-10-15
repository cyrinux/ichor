package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.monitor.DATA_CRITICAL
import name.levis.ichor.monitor.DATA_WARNING
import name.levis.ichor.monitor.dataIssuesOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GarageOperatorTest {
    private val json = """
        {"garage":{"error":"",
         "instances":[{"namespace":"garage","name":"garage","pod":"garage-0","pods":3,"podsReady":3,"version":"v2.3.0",
           "status":"degraded","message":"1 node down","connectedNodes":2,"knownNodes":3,"storageNodes":3,"storageNodesUp":2,
           "partitions":256,"partitionsQuorum":256,"partitionsAllOk":171,"resyncQueue":10,"resyncErrors":4,"tableSyncQueue":0,
           "layoutVersion":12,"nodes":[],"source":"cli-json"}],
         "version":"v1beta2","truncated":false,
         "clusters":[
          {"namespace":"garage","name":"garage","instance":"garage/garage","phase":"Degraded","clusterId":"1b7e","garageVersion":"v2.3.0",
           "replicas":5,"readyReplicas":4,"storageReplicas":3,"storageReadyReplicas":2,"gatewayReplicas":2,"gatewayReadyReplicas":2,
           "layoutVersion":12,"stagedLayoutVersion":13,"pendingGatewayTombstones":["gn-gw-0"],
           "conditions":[{"type":"QuorumAtRisk","status":"True","reason":"PartitionsBelowQuorum","message":"1/3 partitions without write quorum","time":100}],
           "health":"warning","reasons":["quorumAtRisk","storageReplicas","layoutStaged","tombstones"]},
          {"namespace":"edge","name":"edge","instance":"","phase":"Running","health":"critical","reasons":["adminUnreachable"]}],
         "buckets":[
          {"namespace":"garage","name":"backups","instance":"garage/garage","cluster":"garage","phase":"Ready","bucketId":"a3f8",
           "size":"3.0 GiB","maxSizeQuota":"10Gi","quotaUsage":{"sizeBytes":3070230528,"sizeLimit":10737418240,"sizePercent":28,"objectCount":1204},
           "websiteEnabled":false,"websiteUrl":"","keysWithAccess":2,
           "conditions":[{"type":"LifecycleConfigured","status":"False","reason":"GarageTooOld","message":"lifecycle rules need Garage v2.3.0 or newer","time":200}],
           "health":"warning","reasons":["lifecycleNotApplied"]},
          {"namespace":"garage","name":"files","instance":"garage/garage","cluster":"garage","phase":"Ready","bucketId":"4f1d",
           "size":"52 MiB","quotaUsage":{"sizeBytes":54525952,"objectCount":312},
           "websiteEnabled":true,"websiteUrl":"https://files.example.com","keysWithAccess":1,"health":"ok","reasons":[]}],
         "keys":[
          {"namespace":"garage","name":"backup-agent","instance":"garage/garage","cluster":"garage","phase":"Ready","keyId":"e41a",
           "clusterWide":true,"allBuckets":{"read":true,"write":true,"owner":true},"bucketPermissions":[],"bucketPermissionsTotal":0,
           "health":"ok","reasons":[]},
          {"namespace":"garage","name":"app-writer","instance":"garage/garage","cluster":"garage","phase":"Ready","keyId":"6d03",
           "clusterWide":false,"bucketPermissions":[{"bucket":"files","read":true,"write":true,"owner":false}],"bucketPermissionsTotal":25,
           "health":"ok","reasons":[]}],
         "nodes":[
          {"namespace":"garage","name":"garage-2xv9p","instance":"garage/garage","cluster":"garage","phase":"Degraded",
           "nodeId":"9c2f","zone":"zone-3","capacity":"500Gi","gateway":false,"connected":false,"inLayout":true,"lastSeenSecs":172800,
           "health":"warning","reasons":["nodeDisconnected"]}],
         "operator":{"namespace":"garage-operator-system","name":"garage-operator","ready":1,"desired":1,"health":"ok","reasons":[]}}}
    """.trimIndent()

    private val services = TalosJson.decodeFromString(DataServices.serializer(), json)

    @Test
    fun decodesTheOperatorLayer() {
        val g = services.garage!!
        assertTrue(g.hasOperator)
        assertEquals("v1beta2", g.version)
        assertFalse(g.truncated)

        val degraded = g.clusters[0]
        assertEquals("garage/garage", degraded.instance)
        assertEquals(GarageOpReason.QUORUM_AT_RISK, degraded.reasonList.first())
        assertEquals(13, degraded.stagedLayoutVersion)
        assertEquals(listOf("gn-gw-0"), degraded.pendingGatewayTombstones)
        assertEquals(ServiceHealth.WARNING, degraded.serviceHealth)
        // The unmatched handle stays at the section root.
        assertEquals("", g.clusters[1].instance)
        assertEquals(ServiceHealth.CRITICAL, g.clusters[1].serviceHealth)

        val backups = g.buckets[0]
        assertEquals(28, backups.quotaUsage?.sizePercent)
        assertEquals("10Gi", backups.maxSizeQuota)
        assertEquals(GarageOpReason.LIFECYCLE, backups.reasonList.single())
        assertEquals("https://files.example.com", g.buckets[1].websiteUrl)

        val agent = g.keys[0]
        assertTrue(agent.clusterWide)
        assertTrue(agent.allBuckets!!.owner)
        assertNull(g.keys[1].allBuckets)
        assertEquals(25, g.keys[1].bucketPermissionsTotal)
        assertEquals("files", g.keys[1].bucketPermissions.single().bucket)

        val down = g.nodes.single()
        assertFalse(down.connected)
        assertTrue(down.inLayout)
        assertEquals(172800, down.lastSeenSecs)
        assertEquals(GarageOpReason.NODE_DISCONNECTED, down.reasonList.single())

        assertEquals(1, g.operator?.ready)
        assertEquals("garage-operator-system/garage-operator", g.operator?.label)
    }

    @Test
    fun decodesWithoutTheOperator() {
        val plain = TalosJson.decodeFromString(
            GarageStatus.serializer(),
            """{"error":"","instances":[{"namespace":"garage","name":"garage","status":"healthy"}]}""",
        )
        assertFalse(plain.hasOperator)
        assertEquals(0, plain.clusters.size)
        assertNull(plain.operator)
    }

    @Test
    fun alertsCoverClustersBucketsAndNodeDeclarations() {
        val issues = dataIssuesOf(services)
        // The degraded instance, the degraded cluster, the failed handle, the bucket whose
        // lifecycle was dropped and the disconnected node declaration.
        assertEquals(DATA_WARNING, issues["garage|garage/garage"])
        assertEquals(DATA_WARNING, issues["garage|cluster/garage/garage"])
        assertEquals(DATA_CRITICAL, issues["garage|cluster/edge/edge"])
        assertFalse(issues.containsKey("garage|edge/edge"))
        assertEquals(DATA_WARNING, issues["garage|bucket/garage/backups"])
        assertEquals(DATA_WARNING, issues["garage|node/garage/garage-2xv9p"])
        assertFalse(issues.containsKey("garage|garage/garage-2xv9p"))
    }

    @Test
    fun anInstanceAndAClusterSharingANameKeepTheirOwnSeverity() {
        val shared = TalosJson.decodeFromString(
            DataServices.serializer(),
            """
            {"garage":{"error":"",
             "instances":[{"namespace":"s3","name":"main","status":"unavailable"}],
             "clusters":[{"namespace":"s3","name":"main","phase":"Degraded","health":"warning","reasons":["storageReplicas"]}]}}
            """.trimIndent(),
        )
        val issues = dataIssuesOf(shared)
        assertEquals(DATA_CRITICAL, issues["garage|s3/main"])
        assertEquals(DATA_WARNING, issues["garage|cluster/s3/main"])
    }
}
