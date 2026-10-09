package name.levis.ichor.model

import name.levis.ichor.model.KubeObjectAction.COPY
import name.levis.ichor.model.KubeObjectAction.DELETE
import name.levis.ichor.model.KubeObjectAction.EDIT
import name.levis.ichor.model.KubeObjectAction.PORT_FORWARD
import name.levis.ichor.model.KubeObjectAction.REFRESH
import name.levis.ichor.model.KubeObjectAction.SCALE
import name.levis.ichor.model.KubeObjectAction.SHARE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeObjectBarTest {

    @Test
    fun defaultKeepsThreeIconsSoTheNameFits() {
        val bar = KubeObjectAction.bar.default
        assertEquals(listOf(EDIT, REFRESH, COPY), bar.icons)
        assertEquals(listOf(SHARE, PORT_FORWARD, SCALE, DELETE), bar.menu)
        assertTrue(bar.isDefault)
    }

    @Test
    fun encodeRoundTrips() {
        val bar = KubeObjectAction.bar.default.toBar(PORT_FORWARD).toMenu(COPY)
        assertEquals("EDIT,REFRESH,PORT_FORWARD|COPY,SHARE,SCALE,DELETE", bar.encode())
        assertEquals(bar, KubeObjectAction.bar.parse(bar.encode()))
    }

    @Test
    fun deleteJoinsASavedBarInItsMenu() {
        val saved = KubeObjectAction.bar.parse("EDIT,REFRESH,COPY|SHARE,PORT_FORWARD")
        assertEquals(listOf(SHARE, PORT_FORWARD, SCALE, DELETE), saved.menu)
    }
}
