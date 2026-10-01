package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class LogFollowTest {

    @Test
    fun appendKeepsTheLastLines() {
        assertEquals(listOf("a", "b", "c"), listOf("a").appendCapped(listOf("b", "c"), cap = 5))
        assertEquals(listOf("c", "d", "e"), listOf("a", "b", "c").appendCapped(listOf("d", "e"), cap = 3))
        assertEquals(MAX_FOLLOW_LINES, List(MAX_FOLLOW_LINES) { "x" }.appendCapped(listOf("y")).size)
    }
}
