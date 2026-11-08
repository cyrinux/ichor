package name.levis.ichor.ui.importconfig

import org.junit.Assert.assertEquals
import org.junit.Test

class SourceTileColumnsTest {

    @Test
    fun aPhoneGetsTwoColumns() {
        // A 320 dp phone: 64 dp of padding around the tiles.
        assertEquals(2, sourceTileColumns(256f, 1f))
        assertEquals(2, sourceTileColumns(256f, 1.3f))
        assertEquals(2, sourceTileColumns(439f, 1f))
    }

    @Test
    fun aWideScreenGetsFourAcross() {
        assertEquals(4, sourceTileColumns(440f, 1f))
        assertEquals(4, sourceTileColumns(600f, 1.3f))
    }

    @Test
    fun largerTextNeedsMoreWidthForFour() {
        assertEquals(2, sourceTileColumns(500f, 1.3f))
    }
}
