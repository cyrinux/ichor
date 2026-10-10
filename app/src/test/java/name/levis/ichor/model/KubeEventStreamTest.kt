package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeEventStreamTest {

    private fun row(key: String, lastSeen: Long, reason: String = "BackOff", count: Int = 1, note: String = "") = LiveKubeEvent(
        key = key,
        type = "Warning",
        reason = reason,
        note = note,
        regarding = KubeEventObject("Pod", "v1", "", "v1", "pods", true, "shop", "web-$key"),
        count = count,
        lastSeen = lastSeen,
    )

    @Test
    fun decodesTheCoreBatchAndStatus() {
        val batch = TalosJson.decodeFromString(
            KubeEventsBatch.serializer(),
            """{"reset":true,"upserts":[{"key":"0fe04095a1c19432","type":"Warning","reason":"BackOff",
               "note":"Back-off restarting failed container","regarding":{"kind":"Deployment","apiVersion":"apps/v1",
               "group":"apps","version":"v1","resource":"deployments","namespaced":true,"namespace":"shop","name":"web"},
               "count":6,"firstSeen":1000,"lastSeen":2000,"source":"kubelet","later":1}],"removed":[]}""",
        )
        assertTrue(batch.reset)
        val e = batch.upserts.single()
        assertTrue(e.isWarning)
        assertEquals(6, e.count)
        assertEquals("Deployment shop/web", e.regarding.label)
        val status = TalosJson.decodeFromString(
            KubeEventsStatus.serializer(),
            """{"state":"reconnecting","reason":"connection refused","api":"v1","tracked":3,"limit":2000,"dropped":0,"partial":false,"note":""}""",
        )
        assertEquals("reconnecting", status.state)
        assertEquals("connection refused", status.reason)
    }

    @Test
    fun resetReplacesThenRemovedThenUpsertsSortedNewestFirst() {
        val start = KubeEventsBatch(reset = true, upserts = listOf(row("a", 10), row("b", 30), row("c", 20))).applyTo(emptyList())
        assertEquals(listOf("b", "c", "a"), start.map { it.key })

        // c is let go, a counts up (same key, replaced in place), d is new.
        val next = KubeEventsBatch(upserts = listOf(row("a", 40, count = 2), row("d", 25)), removed = listOf("c")).applyTo(start)
        assertEquals(listOf("a", "b", "d"), next.map { it.key })
        assertEquals(2, next.first().count)

        // A reset drops everything not in it.
        val reset = KubeEventsBatch(reset = true, upserts = listOf(row("z", 1))).applyTo(next)
        assertEquals(listOf("z"), reset.map { it.key })
        // An empty reset (another namespace with nothing) empties the list.
        assertTrue(KubeEventsBatch(reset = true).applyTo(next).isEmpty())
    }

    @Test
    fun aRowRemovedAndUpsertedInOneBatchStays() {
        val rows = KubeEventsBatch(upserts = listOf(row("a", 50)), removed = listOf("a")).applyTo(listOf(row("a", 10)))
        assertEquals(50L, rows.single().lastSeen)
    }

    @Test
    fun keepsTheCapNewestFirst() {
        val rows = KubeEventsBatch(upserts = (1..5).map { row("k$it", it.toLong()) }).applyTo(emptyList(), cap = 3)
        assertEquals(listOf("k5", "k4", "k3"), rows.map { it.key })
    }

    @Test
    fun pauseHoldsBatchesAndResumeAppliesThemInOrder() {
        val live = KubeEventsFeed().receive(KubeEventsBatch(reset = true, upserts = listOf(row("a", 10), row("b", 20))))
        val paused = live.pause()
            .receive(KubeEventsBatch(upserts = listOf(row("c", 30))))
            .receive(KubeEventsBatch(upserts = listOf(row("a", 40, count = 3)), removed = listOf("b")))
            .receive(KubeEventsBatch(upserts = listOf(row("c", 50, count = 2))))
        // The list stays still while paused.
        assertEquals(listOf("b", "a"), paused.rows.map { it.key })
        assertTrue(paused.paused)
        assertEquals(2, paused.pendingCount)

        val resumed = paused.resume()
        assertFalse(resumed.paused)
        assertNull(resumed.pending)
        assertEquals(0, resumed.pendingCount)
        assertEquals(listOf("c", "a"), resumed.rows.map { it.key })
        assertEquals(listOf(2, 3), resumed.rows.map { it.count })
        // The same as applying every batch as it came.
        val direct = listOf(
            KubeEventsBatch(upserts = listOf(row("c", 30))),
            KubeEventsBatch(upserts = listOf(row("a", 40, count = 3)), removed = listOf("b")),
            KubeEventsBatch(upserts = listOf(row("c", 50, count = 2))),
        ).fold(live.rows) { rows, b -> b.applyTo(rows) }
        assertEquals(direct, resumed.rows)
    }

    @Test
    fun pendingRowsLetGoAndBackAgreeWithApplyingInTurn() {
        val first = KubeEventsBatch(upserts = listOf(row("x", 5)))
        val second = KubeEventsBatch(removed = listOf("x"))
        val third = KubeEventsBatch(upserts = listOf(row("x", 9)))
        val start = listOf(row("y", 1))
        val folded = first.then(second)
        assertEquals(listOf("y"), folded.applyTo(start).map { it.key })
        assertEquals(listOf("x", "y"), folded.then(third).applyTo(start).map { it.key })
        // A reset received while paused replaces what waited.
        val reset = KubeEventsBatch(reset = true, upserts = listOf(row("r", 3)))
        assertEquals(reset, first.then(reset))
        assertEquals(listOf("r"), first.then(reset).applyTo(start).map { it.key })
    }

    @Test
    fun searchesReasonObjectAndNote() {
        val rows = listOf(row("a", 3, reason = "BackOff"), row("b", 2, reason = "Pulled", note = "image pulled in 2s"), row("c", 1, reason = "Unhealthy"))
        assertEquals(listOf("b"), rows.matching("PULLED IN").map { it.key })
        assertEquals(listOf("c"), rows.matching("web-c").map { it.key })
        assertEquals(listOf("a"), rows.matching(" backoff ").map { it.key })
        assertEquals(rows, rows.matching(" "))
    }

    @Test
    fun shareTextHasOneEntryPerRowWithItsMessage() {
        val rows = listOf(row("a", 2000, count = 6, note = "Back-off restarting failed container"), row("b", 1000, reason = "Scheduled"))
        val text = kubeEventsText(rows) { "t$it" }
        assertEquals(
            "t2000 Warning BackOff Pod shop/web-a ×6\n    Back-off restarting failed container\n" +
                "t1000 Warning Scheduled Pod shop/web-b",
            text,
        )
        assertEquals("", kubeEventsText(emptyList()) { "" })
    }

    @Test
    fun regardingOpensItsObjectWhenItsResourceIsKnown() {
        val deployment = KubeEventObject("Deployment", "apps/v1", "apps", "v1", "deployments", true, "shop", "web")
        assertEquals(KubeObjectRef("apps", "v1", "deployments", "Deployment", "shop", "web", editable = true), deployment.ref)
        // A cluster-scoped object opens without a namespace, even if one was given.
        val node = KubeEventObject("Node", "v1", "", "v1", "nodes", false, "default", "worker-1")
        assertEquals("", node.ref?.namespace)
        assertEquals("nodes", node.ref?.resource)
        assertEquals("Node worker-1", node.label)
        // A kind discovery does not know: no navigation.
        assertNull(KubeEventObject("Widget", "example.com/v1", "example.com", "v1", "", true, "shop", "w").ref)
        assertNull(KubeEventObject().ref)
    }
}
