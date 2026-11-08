package name.levis.ichor.ui.join

import name.levis.ichor.ui.nav.Routes
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AddNodePathTest {
    @Test
    fun routeNamesItsPath() {
        AddNodePath.entries.forEach { path ->
            assertEquals(path, AddNodePath.fromRoute(Routes.bootMedia(path).substringAfter("path=")))
        }
    }

    @Test
    fun noOrUnknownPathAsks() {
        assertNull(AddNodePath.fromRoute(Routes.bootMedia().substringAfter("path=")))
        assertNull(AddNodePath.fromRoute(null))
        assertNull(AddNodePath.fromRoute("gcp"))
    }
}
