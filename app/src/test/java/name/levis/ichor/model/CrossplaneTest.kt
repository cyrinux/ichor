package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CrossplaneTest {
    // The shape of Go KubeCrossplane's demo answer, trimmed to one row per kind.
    private val json = """
        {"installed":true,"version":"v1","error":"",
         "packages":[
          {"group":"pkg.crossplane.io","version":"v1","resource":"providers","kind":"Provider","namespace":"","name":"provider-broken",
           "package":"registry.example.org/packages/provider-broken:v1","currentRevision":"provider-broken-revision",
           "installed":{"status":"False","reason":"PullFailed","message":"cannot pull package","lastTransition":1000},
           "healthy":{"status":"False","reason":"PullFailed","message":"","lastTransition":1000},
           "health":"critical","reasons":["providerUnhealthy"]}],
         "definitions":[
          {"group":"apiextensions.crossplane.io","version":"v1","resource":"compositeresourcedefinitions","kind":"CompositeResourceDefinition","namespace":"","name":"xdatabases.platform.example.org",
           "compositeGroup":"platform.example.org","compositeKind":"XDatabase","plural":"xdatabases","scope":"Namespaced",
           "versions":[{"name":"v1beta1","served":true,"referenceable":true}],
           "established":{"status":"False","reason":"ReconcileError","message":"waiting for the generated CRD","lastTransition":1000},
           "offered":{"status":"True","reason":"WatchingCompositeResourceClaim","message":"","lastTransition":1000},
           "compositions":1,"compositionNames":["database-standard"],"composites":0,"health":"warning","reasons":["notEstablished"]}],
         "composites":[
          {"group":"platform.example.org","version":"v1beta1","resource":"xbuckets","kind":"XBucket","namespace":"","name":"assets",
           "composition":"bucket-standard","resources":1,"claim":"assets-claim",
           "ready":{"status":"True","reason":"Available","message":"","lastTransition":1000},
           "synced":{"status":"True","reason":"ReconcileSuccess","message":"","lastTransition":1000},
           "lastTransition":1000,"health":"ok","reasons":[]}],
         "claims":[],
         "managed":[
          {"group":"storage.example.org","version":"v1beta1","resource":"buckets","kind":"Bucket","namespace":"","name":"resource-1",
           "providerConfig":"example-auth","externalName":"external-1","paused":false,
           "ready":{"status":"True","reason":"Available","message":"","lastTransition":1000},
           "synced":{"status":"False","reason":"ReconcileError","message":"AccessDenied: not authorized","lastTransition":1000},
           "lastTransition":1000,"health":"critical","reasons":["syncFailed"]},
          {"group":"sql.example.org","version":"v1beta1","resource":"databases","kind":"Database","namespace":"apps","name":"resource-5",
           "providerConfig":"example-auth","externalName":"external-5","paused":true,
           "ready":{"status":"False","reason":"Deleting","message":"","lastTransition":1000},
           "synced":{"status":"False","reason":"ReconcilePaused","message":"","lastTransition":1000},
           "lastTransition":1000,"health":"warning","reasons":["deleting","paused"]}],
         "managedTotal":2,"truncated":false,"counts":{"ok":1,"warning":2,"critical":2}}
    """.trimIndent()

    private val xp = TalosJson.decodeFromString(Crossplane.serializer(), json)

    @Test
    fun decodesEveryKind() {
        assertTrue(xp.installed)
        assertEquals(1, xp.packages.size)
        assertEquals("providerUnhealthy", xp.packages[0].reasonList.single().wire)
        assertEquals("XDatabase", xp.definitions[0].compositeKind)
        val bucket = xp.managed[0]
        assertEquals("Cannot sync", reasonWording(bucket))
        assertEquals("AccessDenied: not authorized", bucket.synced.message)
        assertTrue(xp.managed[1].paused)
    }

    private fun reasonWording(o: CrossplaneObject) = when (o.reasonList.single()) {
        CrossplaneReason.SYNC_FAILED -> "Cannot sync"
        else -> ""
    }

    @Test
    fun problemsFirstAcrossKinds() {
        val problems = xp.problems
        // The failing Bucket (critical) before the deleting Database (warning); the ok XBucket is out.
        assertEquals(listOf("resource-1", "resource-5"), problems.map { it.name })
        assertEquals(ServiceHealth.CRITICAL, problems[0].serviceHealth)
    }

    @Test
    fun refsOpenTheObjectScreen() {
        val bucket = xp.managed[0]
        assertEquals(KubeObjectRef("storage.example.org", "v1beta1", "buckets", "Bucket", "", "resource-1", editable = true), bucket.ref())
    }

    @Test
    fun filterAndSearch() {
        val rows = xp.managed
        assertEquals(listOf("resource-1"), rows.filtered(CrossplaneFilter.CRITICAL, "").map { it.name })
        assertEquals(listOf("resource-5"), rows.filtered(CrossplaneFilter.WARNING, "").map { it.name })
        assertEquals(listOf("resource-1"), rows.filtered(CrossplaneFilter.ALL, "external-1").map { it.name })
        assertEquals(2, xp.counts.critical)
    }
}
