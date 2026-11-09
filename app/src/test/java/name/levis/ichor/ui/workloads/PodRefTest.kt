package name.levis.ichor.ui.workloads

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PodRefTest {

    @Test
    fun splitsOnTheFirstSlash() {
        assertEquals("shop" to "cart-7d9f-x2k", podRef("shop/cart-7d9f-x2k"))
    }

    @Test
    fun aValueWithoutASlashIsNoPod() {
        assertNull(podRef("cart-7d9f-x2k"))
        assertNull(podRef(""))
    }

    @Test
    fun anEmptySideIsNoPod() {
        assertNull(podRef("shop/"))
        assertNull(podRef("/cart-7d9f-x2k"))
        assertNull(podRef("/"))
    }

    @Test
    fun aNameWithAnotherSlashIsNoPod() {
        assertNull(podRef("shop/cart/extra"))
    }
}
