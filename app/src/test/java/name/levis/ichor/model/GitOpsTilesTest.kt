package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/** The GitOps cards fall back on these tiles where there is no inventory: their logos must ship. */
class GitOpsTilesTest {
    private val bundled = File("src/main/assets/appicons").list().orEmpty().toSet()

    @Test
    fun builtInTilesNameBundledIcons() {
        for (tile in listOf(FLUX_TILE, ARGO_CD_TILE)) {
            assertTrue(tile.icon, isValidIconSlug(tile.icon))
            assertEquals("appicons/${tile.icon}.webp", bundledIconAsset(tile.icon, dark = false, available = bundled))
            assertTrue("${tile.icon}.webp", "${tile.icon}.webp" in bundled)
        }
    }

    @Test
    fun builtInTilesMatchTheCatalogIds() {
        assertEquals(FLUX_CATALOG_ID, FLUX_TILE.id)
        assertEquals(ARGO_CD_CATALOG_ID, ARGO_CD_TILE.id)
        assertTrue(FLUX_TILE.known && ARGO_CD_TILE.known)
    }
}
