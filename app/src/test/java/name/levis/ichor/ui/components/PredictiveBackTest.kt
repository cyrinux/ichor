package name.levis.ichor.ui.components

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PredictiveBackTest {
    private val previews = mutableListOf<BackPreview>()
    private var closed = 0

    private suspend fun follow(events: Flow<BackPreview>) =
        followBackGesture(events, onPreview = { previews += it }, onBack = { closed++ })

    @Test
    fun theLayerFollowsTheSwipeThenClosesOnRelease() = runTest {
        follow(flowOf(BackPreview(0.2f), BackPreview(0.6f, fromLeftEdge = false)))

        assertEquals(listOf(BackPreview(0.2f), BackPreview(0.6f, fromLeftEdge = false), BackPreview.Idle), previews)
        assertEquals(1, closed)
    }

    @Test
    fun aKeyOrButtonBackClosesWithoutAPreview() = runTest {
        follow(emptyFlow())

        assertEquals(listOf(BackPreview.Idle), previews)
        assertEquals(1, closed)
    }

    @Test
    fun aCancelledSwipeRestoresTheLayerAndKeepsItOpen() = runTest {
        var rethrown = false
        try {
            follow(
                flow {
                    emit(BackPreview(0.4f))
                    throw CancellationException("swipe cancelled")
                },
            )
        } catch (e: CancellationException) {
            rethrown = true
        }

        assertTrue("the cancellation must reach PredictiveBackHandler", rethrown)
        assertEquals(listOf(BackPreview(0.4f), BackPreview.Idle), previews)
        assertEquals(0, closed)
    }

    @Test
    fun theLayerIsBackInPlaceWhenItStaysAfterBack() = runTest {
        // A folder trail: back goes up one level and the same layer stays on screen.
        follow(flowOf(BackPreview(0.9f)))

        assertEquals(BackPreview.Idle, previews.last())
    }
}
