package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConfigSnippetsTest {
    @Test
    fun decodesTheList() {
        val got = TalosJson.decodeFromString(
            ConfigSnippetList.serializer(),
            """{"snippets":[{"id":"ntp","group":"network","params":[{"name":"servers","type":"list","hint":"time.cloudflare.com"}],"staged":false},
                            {"id":"kernel_args","group":"kernel","params":[{"name":"args","type":"list","hint":"console=ttyS0"}],"staged":true},
                            {"id":"dns","group":"network","params":[{"name":"nameservers","type":"list","hint":"1.1.1.1"}],"staged":false}]}""",
        )
        assertEquals(listOf("network" to listOf("ntp", "dns"), "kernel" to listOf("kernel_args")), got.snippets.byGroup().map { (g, s) -> g to s.map { it.id } })
        assertTrue(got.snippets[1].staged)
    }

    @Test
    fun decodesTheEdits() {
        val got = TalosJson.decodeFromString(
            SnippetEdits.serializer(),
            """{"fixable":true,"edits":[{"doc":0,"path":["machine","time","servers"],"op":"put","key":"","type":"yaml","value":"[\"a.example.test\"]"}],"staged":false}""",
        )
        assertEquals("put", got.edits.single().op)
        assertEquals(listOf("machine", "time", "servers"), got.edits.single().path)
    }

    @Test
    fun onlyConfigSettingsCanBeFixed() {
        fun change(key: String, before: String = "a") = DriftChange("192.0.2.11", "192.0.2.10", key, before, "b")
        assertEquals(DriftFix("ntp", "a", "b"), change("ntp").fix())
        assertEquals("mtu", change("mtu").fix()?.key)
        assertNull(change("version").fix())
        assertNull(change("dns", before = "").fix())
    }
}
