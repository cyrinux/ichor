package name.levis.ichor.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class UiStateTest {

    private val error = UiText.Raw("timed out")

    @Test
    fun aFailedRefreshKeepsTheDataOnScreenWithTheError() {
        val shown = UiState.Loaded("data", refreshing = true, fetchedAt = 1_000)
        assertEquals(UiState.Loaded("data", refreshing = false, fetchedAt = 1_000, error = error), shown.refreshFailed(error))
    }

    @Test
    fun theDataOnScreenWinsOverTheLastKnownOne() {
        val shown = UiState.Loaded("shown", fetchedAt = 2_000)
        assertEquals("shown", (shown.refreshFailed(error, "stored" to 1_000L) as UiState.Loaded).data)
    }

    @Test
    fun withNothingOnScreenTheLastKnownValueIsShown() {
        val state: UiState<String> = UiState.Loading
        assertEquals(UiState.Loaded("stored", fetchedAt = 1_000, error = error), state.refreshFailed(error, "stored" to 1_000L))
    }

    @Test
    fun withNothingToShowItFails() {
        val state: UiState<String> = UiState.Loading
        assertEquals(UiState.Failed(error), state.refreshFailed(error))
    }
}
