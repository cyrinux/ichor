package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MachineConfigModelTest {
    // As the Go core's MachineConfigDescribe writes it: absent fields are omitted.
    private val treeJson = """
        {"schema":true,"documents":[
          {"index":0,"title":"v1alpha1","node":{"key":"","path":[],"type":"object","children":[
            {"key":"debug","path":["debug"],"type":"boolean","value":"false","description":"Verbose logging."},
            {"key":"machine","path":["machine"],"type":"object","addable":[{"key":"nodeLabels","type":"object","description":"Labels."}],"children":[
              {"key":"type","path":["machine","type"],"type":"string","value":"worker","enum":["controlplane","worker"]},
              {"key":"token","path":["machine","token"],"type":"string","value":"******","redacted":true},
              {"key":"certSANs","path":["machine","certSANs"],"type":"array","itemType":"string","children":[
                {"key":"0","path":["machine","certSANs","0"],"type":"string","value":"192.0.2.1"}]},
              {"key":"sysctls","path":["machine","sysctls"],"type":"object","freeKeyType":"string"}]}]}},
          {"index":1,"title":"HostnameConfig","node":{"key":"","path":[],"type":"object","children":[
            {"key":"auto","path":["auto"],"type":"string","value":"stable"}]}}]}
    """.trimIndent()

    private val tree = TalosJson.decodeFromString(ConfigTree.serializer(), treeJson)

    private fun fields(rows: List<ConfigRow>) = rows.filterIsInstance<ConfigRow.Field>().filterNot { it.isRoot }.map { it.node.key }

    @Test
    fun decodesTheTree() {
        assertTrue(tree.schema)
        assertNull(tree.error)
        val machine = tree.documents[0].node.children[1]
        assertEquals(listOf("controlplane", "worker"), machine.children[0].allowed)
        assertTrue(machine.children[1].redacted)
        assertEquals("nodeLabels", machine.addable.single().key)
        assertTrue(machine.canGrow)
        assertTrue(machine.children[2].canGrow) // a list
        assertTrue(machine.children[3].canGrow) // free keys
        assertFalse(tree.documents[1].node.canGrow)
    }

    @Test
    fun decodesASyntaxError() {
        val broken = TalosJson.decodeFromString(ConfigTree.serializer(), """{"schema":false,"documents":[],"error":{"line":3,"message":"did not find expected key"}}""")
        assertEquals(3, broken.error?.line)
        assertTrue(broken.documents.isEmpty())
    }

    @Test
    fun rowsShowOnlyOpenContainers() {
        val closed = tree.rows(emptySet())
        assertEquals(listOf("debug", "machine", "auto"), fields(closed))
        assertEquals(listOf("v1alpha1", "HostnameConfig"), closed.filterIsInstance<ConfigRow.Title>().map { it.title })

        val open = tree.rows(setOf(fieldId(0, listOf("machine")), fieldId(0, listOf("machine", "certSANs"))))
        assertEquals(listOf("debug", "machine", "type", "token", "certSANs", "0", "sysctls", "auto"), fields(open))
        val san = open.filterIsInstance<ConfigRow.Field>().single { it.node.key == "0" }
        assertTrue(san.inList)
        assertEquals(3, san.depth)
        assertEquals(open.size, open.map { it.id }.toSet().size)
    }

    @Test
    fun searchOpensWhatMatchesAndDropsTheRest() {
        assertEquals(listOf("machine", "certSANs", "0"), fields(tree.rows(emptySet(), "192.0")))
        // A matching container keeps everything under it.
        assertEquals(listOf("machine", "certSANs", "0"), fields(tree.rows(emptySet(), "certsans")))
        assertEquals(listOf("auto"), fields(tree.rows(emptySet(), "STABLE")))
        assertTrue(tree.rows(emptySet(), "nothing-like-this").isEmpty())
        // The mask of a hidden secret is not something to find.
        assertTrue(tree.rows(emptySet(), "*****").isEmpty())
    }

    @Test
    fun editsEncodeAsTheCoreExpects() {
        // Defaults are left out: the core reads an absent key as empty.
        assertEquals(
            """{"doc":0,"path":["machine","type"],"op":"set","type":"string","value":"worker"}""",
            TalosJson.encodeToString(ConfigEdit.serializer(), ConfigEdit.set(0, listOf("machine", "type"), ConfigType.STRING, "worker")),
        )
        assertEquals(
            """{"doc":1,"path":[],"op":"add","key":"hostname","type":"string","value":"a"}""",
            TalosJson.encodeToString(ConfigEdit.serializer(), ConfigEdit.add(1, emptyList(), "hostname", ConfigType.STRING, "a")),
        )
        assertEquals(
            """{"doc":0,"path":["debug"],"op":"remove"}""",
            TalosJson.encodeToString(ConfigEdit.serializer(), ConfigEdit.remove(0, listOf("debug"))),
        )
    }

    @Test
    fun decodesAPreview() {
        val preview = TalosJson.decodeFromString(
            ConfigPreview.serializer(),
            """{"changed":true,"needsReboot":false,"lines":[{"kind":"hunk","text":"@@ -1,2 +1,3 @@"},{"kind":"added","text":"  a: b"}]}""",
        )
        assertTrue(preview.changed)
        assertEquals(listOf(ConfigDiffLine.HUNK, ConfigDiffLine.ADDED), preview.lines.map { it.kind })
    }

    @Test
    fun tryStateFollowsTheEvents() {
        fun progress(phase: String, message: String = "", deadline: Long = 0) = ConfigTryEvent.Progress(ConfigTryProgress(phase, message, deadline))

        var state: ConfigTryState? = null
        state = state.after(progress(ConfigTryState.APPLYING))
        assertFalse((state as ConfigTryState.Running).trying)

        state = state.after(progress(ConfigTryState.TRYING, deadline = 5_000))
        assertTrue((state as ConfigTryState.Running).trying)
        assertEquals(5_000L, state.deadline)

        // A failed keep goes back to trying with the reason; the deadline stays through keeping.
        state = state.after(progress(ConfigTryState.KEEPING))
        assertEquals(5_000L, (state as ConfigTryState.Running).deadline)
        state = state.after(progress(ConfigTryState.TRYING, message = "connection lost", deadline = 5_000))
        assertEquals("connection lost", (state as ConfigTryState.Running).message)

        assertEquals(ConfigTryState.Kept, state.after(ConfigTryEvent.Done("kept", "")))
        assertEquals(ConfigTryState.Reverted, state.after(ConfigTryEvent.Done("reverted", "")))
        assertEquals(ConfigTryState.Failed("needs a reboot"), state.after(ConfigTryEvent.Done("", "needs a reboot")))
    }

    @Test
    fun countdown() {
        assertEquals(300, secondsLeft(300_000, 0))
        assertEquals(1, secondsLeft(1_000, 1))
        assertEquals(0, secondsLeft(1_000, 1_000))
        assertEquals(0, secondsLeft(1_000, 9_000))
        assertEquals("5:00", countdownText(300))
        assertEquals("0:09", countdownText(9))
        assertEquals("10:00", countdownText(600))
    }

    @Test
    fun valueTypesOfANewValue() {
        assertEquals(listOf(ConfigType.BOOLEAN), valueTypes(ConfigType.BOOLEAN))
        assertEquals(ConfigType.choices, valueTypes(ConfigType.ANY))
        assertEquals(ConfigType.choices, valueTypes(""))
        assertTrue(CONFIG_TRY_DEFAULT_TIMEOUT in CONFIG_TRY_TIMEOUTS)
    }
}
