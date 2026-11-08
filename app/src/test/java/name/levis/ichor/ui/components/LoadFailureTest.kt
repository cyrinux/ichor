package name.levis.ichor.ui.components

import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import org.junit.Assert.assertEquals
import org.junit.Test

class LoadFailureTest {
    private val reason = UiText.Raw("connection refused")

    @Test
    fun loadingShowsTheLoadingLineOnly() {
        assertEquals(listOf(SectionPart.LOADING), sectionParts(UiState.Loading))
    }

    @Test
    fun aFailedReadShowsTheRowOnly() {
        assertEquals(listOf(SectionPart.FAILURE), sectionParts(UiState.Failed(reason)))
    }

    @Test
    fun loadedDataShowsTheContentOnly() {
        assertEquals(listOf(SectionPart.CONTENT), sectionParts(UiState.Loaded("data")))
    }

    @Test
    fun aFailedRefreshKeepsTheContentWithTheRowUnderIt() {
        assertEquals(
            listOf(SectionPart.CONTENT, SectionPart.FAILURE),
            sectionParts(UiState.Loaded("data", error = reason)),
        )
    }

    @Test
    fun aRefreshUnderWayHidesTheLastError() {
        assertEquals(listOf(SectionPart.CONTENT), sectionParts(UiState.Loaded("data", refreshing = true, error = reason)))
    }
}
