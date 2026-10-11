package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MetaKeysTest {
    @Test
    fun decodesTheList() {
        val got = TalosJson.decodeFromString(
            MetaKeyList.serializer(),
            """{"keys":[{"key":10,"hex":"0x0a","name":"metal network config","value":"addresses: []\n","size":14,"binary":false,"secret":false,"writable":true},
                        {"key":16,"hex":"0x10","name":"unique machine token","value":"","size":11,"binary":false,"secret":true,"writable":true},
                        {"key":12,"hex":"0x0c","name":"user reserved 1","value":"0001","size":2,"binary":true,"secret":false,"writable":true}],
               "loaded":true,
               "known":[{"key":6,"hex":"0x06","name":"upgrade","value":"","size":0,"binary":false,"secret":false,"writable":false}]}""",
        )
        assertTrue(got.loaded)
        assertEquals(listOf(true, false, false), got.keys.map { it.editable })
        assertFalse(got.known.single().writable)
    }

    @Test
    fun parsesTypedKeysInHex() {
        assertEquals(10, parseMetaKey("0x0a"))
        assertEquals(10, parseMetaKey(" 0A "))
        assertEquals(255, parseMetaKey("ff"))
        assertNull(parseMetaKey("0x100"))
        assertNull(parseMetaKey("zz"))
        assertNull(parseMetaKey(""))
        assertEquals("0x0a", metaHex(10))
    }
}
