package name.levis.ichor

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import name.levis.ichor.ui.DeepLink
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.w3c.dom.Element

class DeepLinkTest {

    @Test
    fun namedReadsTheExtraValue() {
        assertEquals(DeepLink.SEARCH, DeepLink.named("SEARCH"))
        assertEquals(DeepLink.ARGO_CD, DeepLink.named("ARGO_CD"))
        assertNull(DeepLink.named("search"))
        assertNull(DeepLink.named(""))
        assertNull(DeepLink.named(null))
    }

    /** Each build's static shortcut opens its own app on the search screen. */
    @Test
    fun staticShortcutOpensTheSearch() {
        val builds = mapOf(
            "src/main/res/xml/shortcuts.xml" to "name.levis.ichor",
            "src/foss/res/xml/shortcuts.xml" to "name.levis.ichor.foss",
        )
        for ((path, applicationId) in builds) {
            val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(File(path))
            val intent = document.getElementsByTagName("intent").item(0) as Element
            val extra = document.getElementsByTagName("extra").item(0) as Element
            assertEquals(path, applicationId, intent.getAttribute("android:targetPackage"))
            assertEquals(path, MainActivity::class.java.name, intent.getAttribute("android:targetClass"))
            assertEquals(path, MainActivity.EXTRA_OPEN, extra.getAttribute("android:name"))
            assertEquals(path, DeepLink.SEARCH, DeepLink.named(extra.getAttribute("android:value")))
        }
    }
}
