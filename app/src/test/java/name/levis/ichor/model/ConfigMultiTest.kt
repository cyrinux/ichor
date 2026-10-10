package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConfigMultiTest {

    @Test
    fun previewKeepsWhatWouldChangeAndItsModes() {
        val preview = TalosJson.decodeFromString(
            MultiConfigPreview.serializer(),
            """{"nodes":[
                {"node":"10.0.0.1","hostname":"cp-1","changed":true,"lines":[{"kind":"added","text":"a: b"}],"needsReboot":false},
                {"node":"10.0.0.2","hostname":"","changed":false,"lines":[],"needsReboot":false,"error":"machine.nodeLabels: not found"},
                {"node":"10.0.0.3","hostname":"w-1","changed":true,"lines":[],"needsReboot":true}
               ],"anyReboot":true}""",
        )
        assertEquals(listOf("cp-1", "w-1"), preview.changing.map { it.name })
        assertEquals("10.0.0.2", preview.nodes[1].name)
        assertEquals(listOf(ConfigApplyMode.STAGED, ConfigApplyMode.REBOOT), preview.applyModes)
    }

    @Test
    fun runFollowsProgressThenEnds() {
        val progress = TalosJson.decodeFromString(
            MultiConfigProgress.serializer(),
            """{"phase":"applying","message":"","at":1,"index":0,"total":2,"node":"10.0.0.3",
               "nodes":[{"node":"10.0.0.3","hostname":"w-1","state":"applying"},{"node":"10.0.0.1","hostname":"cp-1","state":"pending"}]}""",
        )
        var run = MultiApplyRun(ConfigApplyMode.STAGED).after(MultiConfigEvent.Progress(progress))
        assertEquals(MultiConfigNodeState.APPLYING, run.progress?.nodes?.first()?.state)

        run = run.after(MultiConfigEvent.Done(null))
        assertTrue(run.finished)
        assertNull(run.error)
        assertEquals("boom", MultiApplyRun(ConfigApplyMode.AUTO).after(MultiConfigEvent.Done("boom")).error)
    }
}
