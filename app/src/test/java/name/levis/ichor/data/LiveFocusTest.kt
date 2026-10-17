package name.levis.ichor.data

import org.junit.Assert.assertEquals
import org.junit.Test

class LiveFocusTest {

    @Test
    fun theFocusIsTheUnionOfEverySource() {
        val focus = LiveFocus()
        focus.publish(LiveFocus.OVERVIEW, setOf("worker-1", "worker-2"))
        focus.publish(LiveFocus.NODES_SCREEN, setOf("worker-2", "worker-7"))
        assertEquals(setOf("worker-1", "worker-2", "worker-7"), focus.nodes.value)
        // A source's new set replaces its old one only.
        focus.publish(LiveFocus.NODES_SCREEN, setOf("worker-9"))
        assertEquals(setOf("worker-1", "worker-2", "worker-9"), focus.nodes.value)
    }

    @Test
    fun anotherClusterForgetsEverySourceTheSameOneKeepsThem() {
        val focus = LiveFocus()
        focus.scope("demo-a")
        focus.publish(LiveFocus.NODES_SCREEN, setOf("worker-3"))
        focus.scope("demo-a")
        assertEquals(setOf("worker-3"), focus.nodes.value)
        focus.scope("demo-b")
        assertEquals(emptySet<String>(), focus.nodes.value)
    }
}
