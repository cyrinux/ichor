package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeAttentionTest {

    private fun node(
        addr: String = "10.0.0.1",
        ready: Boolean = true,
        reachable: Boolean = true,
        version: String = "v1.11.2",
        error: String? = null,
        conditions: List<UnmetCondition> = emptyList(),
    ) = NodeOverview(
        node = addr, hostname = addr, reachable = reachable, ready = ready, version = version,
        error = error, unmetConditions = conditions,
    )

    @Test
    fun readyNodeIsCalm() = assertFalse(node().needsAttention)

    @Test
    fun notReadyOrUnreachableNeedsAttention() {
        assertTrue(node(ready = false).needsAttention)
        assertTrue(node(reachable = false).needsAttention)
    }

    @Test
    fun readyNodeReportingAProblemNeedsAttention() {
        assertTrue(node(conditions = listOf(UnmetCondition("services", "etcd not healthy"))).needsAttention)
        assertTrue(node(error = "disk full").needsAttention)
        assertFalse(node(error = " ").needsAttention)
    }

    @Test
    fun sharedVersionWhenEveryAnsweringNodeAgrees() {
        val nodes = listOf(node("a"), node("b"), node("c", reachable = false, version = "v1.10.0"))
        assertEquals("v1.11.2", nodes.sharedVersion())
    }

    @Test
    fun noSharedVersionWhileTheyDifferOrNoneAnswered() {
        assertNull(listOf(node("a"), node("b", version = "v1.12.0")).sharedVersion())
        assertNull(listOf(node("a", reachable = false)).sharedVersion())
        assertNull(listOf(node("a", version = "")).sharedVersion())
        assertNull(emptyList<NodeOverview>().sharedVersion())
    }
}
