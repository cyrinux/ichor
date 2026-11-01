package name.levis.ichor

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.requiredWidth
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.getBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.height
import androidx.compose.ui.unit.width
import androidx.test.ext.junit.runners.AndroidJUnit4
import name.levis.ichor.ui.importconfig.SourceTileGrid
import name.levis.ichor.ui.theme.TalosTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/** The import tiles: the same size whatever their label, in rows the width allows. */
@RunWith(AndroidJUnit4::class)
class SourceTileGridTest {
    @get:Rule
    val compose = createComposeRule()

    private val short = "File"
    private val long = "A label long enough to wrap on several lines of its tile"

    private fun show(width: Dp) {
        compose.setContent {
            TalosTheme {
                // Required: the CI emulator's screen is narrower than the wide case.
                Box(Modifier.requiredWidth(width)) {
                    SourceTileGrid {
                        Text(short)
                        Text("Paste")
                        Text("QR")
                        Text(long)
                    }
                }
            }
        }
    }

    private fun bounds(text: String) = compose.onNodeWithText(text).getBoundsInRoot()

    @Test
    fun aPhoneHasTwoRowsOfEqualTiles() {
        show(256.dp)
        val first = bounds(short)
        val last = bounds(long)
        assertEquals(first.width, last.width)
        assertEquals("a wrapped label makes every tile taller", first.height, last.height)
        assertEquals("first column", first.left, bounds("QR").left)
        assertTrue("second row", last.top >= first.bottom)
    }

    @Test
    fun aWideScreenHasOneRow() {
        show(600.dp)
        val first = bounds(short)
        val last = bounds(long)
        assertEquals(first.top, last.top)
        assertEquals(first.height, last.height)
        assertTrue(last.left > first.right)
    }
}
