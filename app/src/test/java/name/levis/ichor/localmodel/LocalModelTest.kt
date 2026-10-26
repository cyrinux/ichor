package name.levis.ichor.localmodel

import android.net.ConnectivityManager.RESTRICT_BACKGROUND_STATUS_DISABLED
import android.net.ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED
import android.net.ConnectivityManager.RESTRICT_BACKGROUND_STATUS_WHITELISTED
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class LocalModelTest {

    private fun block(connected: Boolean = true, metered: Boolean = false, roaming: Boolean = false, dataSaver: Int = RESTRICT_BACKGROUND_STATUS_DISABLED, allowMetered: Boolean = false) =
        downloadBlock(connected, metered, roaming, dataSaver, allowMetered)

    @Test
    fun wifiOnlyByDefault() {
        assertEquals(DownloadBlock.NONE, block())
        assertEquals(DownloadBlock.METERED, block(metered = true))
        assertEquals(DownloadBlock.NO_NETWORK, block(connected = false))
    }

    @Test
    fun mobileDataOnlyWhenAllowedAndNeverWithDataSaver() {
        assertEquals(DownloadBlock.NONE, block(metered = true, allowMetered = true))
        assertEquals(DownloadBlock.DATA_SAVER, block(metered = true, allowMetered = true, dataSaver = RESTRICT_BACKGROUND_STATUS_ENABLED))
        // Exempted from Data Saver by the user: allowed, as for any other app they exempted.
        assertEquals(DownloadBlock.NONE, block(metered = true, allowMetered = true, dataSaver = RESTRICT_BACKGROUND_STATUS_WHITELISTED))
        // On Wi-Fi, Data Saver does not apply.
        assertEquals(DownloadBlock.NONE, block(dataSaver = RESTRICT_BACKGROUND_STATUS_ENABLED))
    }

    @Test
    fun neverRoaming() {
        assertEquals(DownloadBlock.ROAMING, block(roaming = true, metered = true, allowMetered = true))
        assertEquals(DownloadBlock.ROAMING, block(roaming = true))
    }

    @Test
    fun importedNamesStayInTheirFolder() {
        assertEquals("model.gguf", LocalModelStore.safeName("/sdcard/Download/model.gguf"))
        assertEquals("evil.gguf", LocalModelStore.safeName("../../evil.gguf"))
        assertEquals("a_b.gguf", LocalModelStore.safeName("a\u0000b.gguf"))
        assertEquals("hidden.gguf", LocalModelStore.safeName(".hidden.gguf"))
        assertNull(LocalModelStore.safeName("..."))
        assertNull(LocalModelStore.safeName(""))
    }
}
