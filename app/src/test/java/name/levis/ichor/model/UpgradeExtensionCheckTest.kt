package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UpgradeExtensionCheckTest {

    @Test
    fun decodesTheCheck() {
        val c = TalosJson.decodeFromString(
            UpgradeExtensionCheck.serializer(),
            """{"schematic":"abc","targetVersion":"v1.12.0","installed":[{"name":"iscsi-tools","version":"v0.2.0"}],
               "missing":["iscsi-tools"],"unknown":false,"error":""}""",
        )
        assertEquals(listOf("iscsi-tools"), c.missing)
        assertEquals("v0.2.0", c.installed.single().version)
        assertFalse(c.unknown)
        assertTrue(TalosJson.decodeFromString(UpgradeExtensionCheck.serializer(), """{"unknown":true}""").unknown)
    }
}
