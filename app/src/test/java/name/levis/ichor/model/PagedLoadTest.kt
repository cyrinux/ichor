package name.levis.ichor.model

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class PagedLoadTest {

    /** A list of [total] rows served [size] at a time; the token is the next row's index. */
    private class Server(val total: Int, val size: Int, val counted: Boolean = true) {
        var calls = 0
        var expireAt: String? = null

        suspend fun page(token: String): KubePage<Int> {
            calls++
            if (token == expireAt) {
                expireAt = null
                throw Exception("$KUBE_LIST_EXPIRED: The provided continue parameter is too old")
            }
            val from = token.ifEmpty { "0" }.toInt()
            val to = minOf(from + size, total)
            val complete = to == total
            return KubePage(
                items = (from until to).toList(),
                continueToken = if (complete) "" else to.toString(),
                remaining = if (complete) 0 else if (counted) (total - to).toLong() else -1,
                complete = complete,
            )
        }
    }

    @Test
    fun loadsEveryPageReportingProgress() = runBlocking {
        val server = Server(total = 1_200, size = 500)
        val seen = mutableListOf<PagedLoad<Int>>()
        val load = loadPages(cap = 10_000, fetch = server::page) { seen += it }

        assertTrue(load.done)
        assertFalse(load.capped)
        assertEquals((0 until 1_200).toList(), load.items)
        assertEquals(listOf(500, 1_000, 1_200), seen.map { it.items.size })
        // "500 / ~1,200" after the first page.
        assertEquals(1_200L, seen.first().estimatedTotal)
        assertEquals(3, server.calls)
    }

    @Test
    fun onePageListIsDoneAtOnce() = runBlocking {
        val load = loadPages(cap = 10_000, fetch = Server(total = 42, size = 500)::page)
        assertTrue(load.done)
        assertFalse(load.hasMore)
        assertEquals(42L, load.estimatedTotal)
    }

    @Test
    fun stopsAtTheCapThenLoadsMoreOnScroll() = runBlocking {
        val server = Server(total = 2_000, size = 500)
        val load = loadPages(cap = 1_000, fetch = server::page)

        assertTrue(load.capped)
        assertTrue(load.hasMore)
        assertFalse(load.done)
        assertEquals(1_000, load.items.size)
        assertEquals(2_000L, load.estimatedTotal)

        val more = load.loadMore(server::page)
        assertEquals(1_500, more.items.size)
        assertEquals((0 until 1_500).toList(), more.items) // server order, appended
        val last = more.loadMore(server::page)
        assertTrue(last.done)
        assertEquals(last, last.loadMore { fail("no page after the last"); error("") })
    }

    @Test
    fun theDefaultScopeStopsAfterItsFirstPage() = runBlocking {
        val load = loadPages(cap = eagerLimit(KubeScope(), metered = false), fetch = Server(total = 20_000, size = KUBE_PAGE_SIZE)::page)
        assertEquals(KUBE_PAGE_SIZE, load.items.size)
        assertTrue(load.capped)
    }

    @Test
    fun anExpiredListStartsAgainFromTheFirstPage() = runBlocking {
        val server = Server(total = 1_500, size = 500).apply { expireAt = "1000" }
        val seen = mutableListOf<Int>()
        val load = loadPages(cap = 10_000, fetch = server::page) { seen += it.items.size }

        assertTrue(load.done)
        assertEquals((0 until 1_500).toList(), load.items) // no row twice
        assertEquals(listOf(500, 1_000, 500, 1_000, 1_500), seen)
        assertEquals(6, server.calls)
    }

    @Test
    fun otherErrorsAndRepeatedExpiriesPropagate() = runBlocking {
        try {
            loadPages<Int>(cap = 10_000, fetch = { _: String -> throw IllegalStateException("Kubernetes API: permission denied") })
            fail("expected a failure")
        } catch (e: IllegalStateException) {
            assertEquals("Kubernetes API: permission denied", e.message)
        }
        var calls = 0
        try {
            loadPages<Int>(cap = 10_000, maxRestarts = 2, fetch = { _: String -> calls++; throw Exception("$KUBE_LIST_EXPIRED: gone") })
            fail("expected a failure")
        } catch (e: Exception) {
            assertTrue(e.isKubeListExpired())
        }
        assertEquals(3, calls)
    }

    @Test
    fun unknownRemainingCount() = runBlocking {
        val first = PagedLoad<Int>().append(Server(total = 1_000, size = 500, counted = false).page(""))
        assertNull(first.estimatedTotal)
        assertTrue(first.hasMore)
    }

    @Test
    fun detailedOnlyWhenEveryPageIs() {
        val load = PagedLoad<Int>()
            .append(KubePage(listOf(1), continueToken = "a", complete = false, detailed = true))
            .append(KubePage(listOf(2), detailed = false))
        assertFalse(load.detailed)
        assertTrue(PagedLoad.complete(listOf(1)).detailed)
    }

    @Test
    fun capIsLowerOnMeteredNetworks() {
        assertEquals(10_000, eagerCap(metered = false))
        assertEquals(5_000, eagerCap(metered = true))
    }

    @Test
    fun recognisesTheExpiredMessageThroughCauses() {
        assertTrue(RuntimeException("wrapped", Exception("go: $KUBE_LIST_EXPIRED: too old")).isKubeListExpired())
        assertFalse(Exception("Kubernetes API: not found").isKubeListExpired())
    }
}
