package name.levis.ichor.ui

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class LoadingViewModelTest {

    private class Counting(override val loadsOnOpen: Boolean = true) : LoadingViewModel<Int>() {
        var fetches = 0
        override suspend fun fetch() = ++fetches
    }

    @Before
    fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun aScreenThatCollectsTheStateStartsTheFirstRead() = runTest(UnconfinedTestDispatcher()) {
        val vm = Counting()
        val screen = launch { vm.state.collect {} }

        assertEquals(1, vm.fetches)
        assertEquals(1, (vm.state.value as UiState.Loaded).data)
        screen.cancel()
    }

    @Test
    fun readingTheValueStartsNothing() {
        val vm = Counting()

        assertEquals(UiState.Loading, vm.state.value)
        assertEquals(0, vm.fetches)
    }

    @Test
    fun collectingAgainDoesNotReadAgain() = runTest(UnconfinedTestDispatcher()) {
        val vm = Counting()
        launch { vm.state.collect {} }.cancel()
        val back = launch { vm.state.collect {} }

        assertEquals(1, vm.fetches)
        back.cancel()
    }

    @Test
    fun aReadAlreadyStartedIsNotStartedTwice() = runTest(UnconfinedTestDispatcher()) {
        val vm = Counting()
        vm.refresh()
        val screen = launch { vm.state.collect {} }

        assertEquals(1, vm.fetches)
        screen.cancel()
    }

    @Test
    fun aReadThatWaitsStartsOnlyOnRefresh() = runTest(UnconfinedTestDispatcher()) {
        val vm = Counting(loadsOnOpen = false)
        val screen = launch { vm.state.collect {} }
        assertEquals(0, vm.fetches)

        vm.refresh()
        assertEquals(1, vm.fetches)
        screen.cancel()
    }
}
