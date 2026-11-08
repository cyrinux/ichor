package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeSanitizeTest {

    private val json = """{"pods":[
        {"namespace":"shop","name":"web-7d9f-abcde","status":"CrashLoopBackOff","ownerKind":"Deployment","ownerName":"web","ageSec":3600},
        {"namespace":"shop","name":"report-1-xyz","status":"Completed","ownerKind":"Job","ownerName":"report-1","ageSec":7200},
        {"namespace":"shop","name":"report-2-xyz","status":"Completed","ownerKind":"Job","ownerName":"report-2","ageSec":600},
        {"namespace":"shop","name":"fresh","status":"Pending","ageSec":30},
        {"namespace":"shop","name":"stuck","status":"Pending","ageSec":900},
        {"namespace":"shop","name":"edge","status":"Pending","ageSec":120}],
        "truncated":true}"""

    private val preview = TalosJson.decodeFromString(KubeSanitizePreview.serializer(), json)

    @Test
    fun decodesTheCoreJson() {
        assertTrue(preview.truncated)
        assertEquals(6, preview.pods.size)
        val web = preview.pods.first()
        assertEquals("shop/web-7d9f-abcde", web.key)
        assertTrue(web.owned)
        assertFalse(preview.pods.first { it.name == "fresh" }.owned)
    }

    @Test
    fun groupsByStatusLargestFirst() {
        val groups = preview.pods.groupedByStatus()
        assertEquals(listOf("Pending", "Completed", "CrashLoopBackOff"), groups.map { it.status })
        assertEquals(listOf(3, 2, 1), groups.map { it.pods.size })
        assertEquals(listOf("shop/edge", "shop/fresh", "shop/stuck"), groups.first().pods.map { it.key })
    }

    @Test
    fun youngPendingPodsStartUnchecked() {
        val checked = preview.pods.defaultChecked()
        assertFalse("shop/fresh" in checked)
        assertTrue("shop/stuck" in checked)
        // 120 s is old enough.
        assertTrue("shop/edge" in checked)
        assertEquals(5, checked.size)
        // Age matters only for Pending.
        assertTrue(SanitizePod(status = "Completed", ageSec = 1).checkedByDefault)
    }

    @Test
    fun sweepNamesAreTheCheckedKeysOnePerLine() {
        val names = preview.pods.sweepNames(setOf("shop/stuck", "shop/report-1-xyz"))
        assertEquals("shop/report-1-xyz\nshop/stuck", names)
        assertEquals("", preview.pods.sweepNames(emptySet()))
    }

    @Test
    fun decodesTheSweepOutcome() {
        val r = TalosJson.decodeFromString(KubeSanitizeResult.serializer(), """{"deleted":2,"failed":[{"name":"shop/gone","error":"not found"}]}""")
        assertEquals(2, r.deleted)
        assertEquals("shop/gone", r.failed.single().name)
        assertEquals(KubeSanitizeResult(), TalosJson.decodeFromString(KubeSanitizeResult.serializer(), """{"deleted":0,"failed":null}"""))
    }
}
