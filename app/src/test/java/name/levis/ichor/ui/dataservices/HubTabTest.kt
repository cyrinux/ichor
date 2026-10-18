package name.levis.ichor.ui.dataservices

import name.levis.ichor.model.DataServiceKind
import org.junit.Assert.assertEquals
import org.junit.Test

class HubTabTest {
    /** The pager keeps its page keys in a Bundle: they must be Strings, never a HubTab. */
    @Test
    fun pagesAreStringIds() {
        val tabs = listOf(HubTab.Native(DataServiceKind.LONGHORN), HubTab.Plugin("kube-green", "kube-green"))
        val pages: List<Any> = hubPages(tabs)
        assertEquals(listOf("LONGHORN", "plugin:kube-green"), pages)
        pages.forEach { assertEquals(String::class.java, it.javaClass) }
        assertEquals(pages.size, pages.toSet().size)
    }
}
