package name.levis.ichor.ui.debug

import name.levis.ichor.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class DebugSnippetTest {

    private val json = """
        [{"group":"interfaces","label":"addresses","command":"ip -br -c a","run":true},
         {"group":"reachability","label":"ping","command":"ping -c3 ","run":false},
         {"group":"future","label":"new","command":"x","run":true,"extra":1}]
    """.trimIndent()

    @Test
    fun decodesTheCoreListIgnoringUnknownFields() {
        val snippets = decodeDebugSnippets(json)
        assertEquals(listOf("addresses", "ping", "new"), snippets.map { it.label })
        assertEquals(listOf(true, false, true), snippets.map { it.run })
    }

    @Test
    fun runSnippetsEndWithEnterTypedOnesDoNot() {
        val (run, typed) = decodeDebugSnippets(json)
        assertEquals("ip -br -c a\r", String(run.bytes()))
        assertEquals("ping -c3 ", String(typed.bytes()))
    }

    @Test
    fun typedSnippetsShowWhereTheArgumentGoes() {
        val (run, typed) = decodeDebugSnippets(json)
        assertEquals("ip -br -c a", run.display)
        assertEquals("ping -c3 …", typed.display)
    }

    @Test
    fun unknownGroupHasNoTitle() {
        assertNull(debugGroupTitle("future"))
        assertEquals(R.string.debug_group_dns, debugGroupTitle("dns"))
    }
}
