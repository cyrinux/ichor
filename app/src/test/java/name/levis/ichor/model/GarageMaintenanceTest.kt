package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GarageMaintenanceTest {
    // Shaped like KubeGarageBlockErrors' answer (the demo cluster's), with a field the app does not know.
    private val reportJson = """
        {"errored":1294,"detailed":3,"live":1,"cleanupOnly":2,"staleRefs":1,"refcountMismatches":1,"retryable":1290,"repairsRunning":true,"futureField":1,
         "nodes":[
          {"id":"1b7e44c9a2f03d58","hostname":"garage-4kq7z","errored":648,"error":"","blocks":[
            {"hash":"${"a".repeat(64)}","refcount":1,"errors":41,"lastTrySecs":300,"nextTrySecs":3300,"impact":"live","staleRef":false,"refcountMismatch":false,"error":"",
             "refs":[{"kind":"object","bucket":"4f1d0c2e9b7a6583ffff","key":"photos/2024/beach.jpg","uploadId":"","version":"","live":true}]},
            {"hash":"${"b".repeat(64)}","refcount":1,"errors":38,"impact":"stale-ref","staleRef":true,"refcountMismatch":true,
             "refs":[{"kind":"upload","bucket":"","key":"","uploadId":"c3a9e1f07b2d4e68","live":false}]}]},
          {"id":"9c2f61a0d4e8b7c3aaaa","hostname":"","errored":0,"error":"Network error: Not connected","blocks":[]}]}
    """.trimIndent()

    @Test
    fun decodesTheBlockReport() {
        val report = TalosJson.decodeFromString(GarageBlockReport.serializer(), reportJson)
        assertEquals(1294, report.errored)
        assertEquals(1290, report.retryable)
        assertTrue(report.repairsRunning)
        assertEquals(GarageBlockVerdict.LIVE_DATA, report.verdict)

        val node = report.nodes[0]
        assertEquals("garage-4kq7z", node.label)
        assertEquals("9c2f61a0d4e8b7c3", report.nodes[1].label)

        val live = node.blocks[0]
        assertEquals("aaaaaaaaaaaa", live.shortHash)
        assertEquals(GarageBlockImpact.LIVE, live.impactKind)
        assertEquals(3300L, live.nextTrySecs)
        assertEquals("4f1d0c2e9b7a6583/photos/2024/beach.jpg", live.refs[0].label)
        assertTrue(live.refs[0].live)

        val stale = node.blocks[1]
        assertEquals(GarageBlockImpact.STALE_REF, stale.impactKind)
        assertTrue(stale.staleRef && stale.refcountMismatch)
        assertEquals("upload c3a9e1f07b2d4e68", stale.refs[0].label)
        assertFalse(stale.refs[0].live)
    }

    @Test
    fun verdictFollowsTheCounts() {
        assertEquals(GarageBlockVerdict.NONE_FAILING, GarageBlockReport().verdict)
        assertEquals(GarageBlockVerdict.DELETED_ONLY, GarageBlockReport(errored = 5, detailed = 2, cleanupOnly = 2).verdict)
        assertEquals(GarageBlockVerdict.LIVE_DATA, GarageBlockReport(errored = 5, detailed = 2, live = 1).verdict)
    }

    @Test
    fun unknownImpactIsUnknown() {
        assertEquals(GarageBlockImpact.UNKNOWN, GarageBlock(impact = "someday").impactKind)
        assertEquals(GarageBlockImpact.CLEANUP, GarageBlock(impact = "cleanup").impactKind)
        assertEquals("version 0123456789abcdef", GarageBlockRef(kind = "version", version = "0123456789abcdef0123").label)
    }

    @Test
    fun decodesTheRepairResult() {
        val json = """{"blockRefs":true,"blockRc":true,"repairsRunning":false,"unreachable":false,"retried":12,"errors":[]}"""
        val result = TalosJson.decodeFromString(GarageRepairResult.serializer(), json)
        assertEquals(12L, result.retried)
        assertEquals(listOf(GarageRepairOutcome.BOTH_LAUNCHED, GarageRepairOutcome.RETRIED), result.outcomes)
    }

    @Test
    fun repairOutcomesSayWhatHappened() {
        assertEquals(listOf(GarageRepairOutcome.REFS_LAUNCHED), GarageRepairResult(blockRefs = true).outcomes)
        assertEquals(listOf(GarageRepairOutcome.RC_LAUNCHED), GarageRepairResult(blockRc = true).outcomes)
        assertEquals(
            listOf(GarageRepairOutcome.ALREADY_RUNNING, GarageRepairOutcome.RETRIED),
            GarageRepairResult(repairsRunning = true, retried = 3).outcomes,
        )
        assertEquals(listOf(GarageRepairOutcome.UNREACHABLE), GarageRepairResult(unreachable = true).outcomes)
        assertEquals(listOf(GarageRepairOutcome.NOTHING), GarageRepairResult().outcomes)
        // Only errors: the errors say it all, no "nothing to repair".
        assertEquals(emptyList<GarageRepairOutcome>(), GarageRepairResult(errors = listOf("boom")).outcomes)
    }

    @Test
    fun nodeTranquilityDefaultsToUnknown() {
        assertEquals(-1L, TalosJson.decodeFromString(GarageNode.serializer(), """{"id":"aa"}""").tranquility)
        assertEquals(0L, TalosJson.decodeFromString(GarageNode.serializer(), """{"id":"aa","tranquility":0}""").tranquility)
    }
}
