package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AiToolCallsTest {
    private val pod = AiToolCall("pod_logs", AiToolArgs(namespace = "ns-1", pod = "pod-3"))

    @Test
    fun argumentsDecodeFromWhatGoSends() {
        val args = TalosJson.decodeFromString(AiToolArgs.serializer(), """{"namespace":"ns-1","pod":"pod-3","container":"ctr-1","tail":100}""")
        assertEquals("pod-3", args.pod)
        assertEquals("ns-1", args.namespace)
    }

    @Test
    fun aResultClosesTheOldestRequestOfThatTool() {
        val calls = listOf(pod, pod.copy(args = AiToolArgs(pod = "pod-4")))
        val once = calls.finished(AnswerEvent.ToolDone("pod_logs", 4096, "a log", null))
        assertTrue(once[0].done)
        assertEquals("a log", once[0].result)
        assertEquals(4096, once[0].bytes)
        assertFalse(once[1].done)

        val twice = once.finished(AnswerEvent.ToolDone("pod_logs", 0, "", "not in the report"))
        assertEquals("not in the report", twice[1].error)
    }

    @Test
    fun aResultForNothingOpenChangesNothing() {
        assertEquals(listOf(pod), listOf(pod).finished(AnswerEvent.ToolDone("object_yaml", 1, "x", null)))
    }

    private fun preferences(prefs: MemoryPrefs) =
        AiPreferences(prefs, listOf("anthropic"), { error("no key in these tests") }, CoroutineScope(Dispatchers.Unconfined))

    @Test
    fun theSwitchIsOffByDefault() {
        assertFalse(AiSettings().allowTools)
        assertFalse(preferences(MemoryPrefs()).settings.value.allowTools)
    }

    @Test
    fun theOldKeySaysNothingAndIsRemoved() {
        val prefs = MemoryPrefs().apply { edit().putBoolean("allow_tools", true).apply() }
        assertFalse(preferences(prefs).settings.value.allowTools)
        assertFalse(prefs.contains("allow_tools"))
    }

    @Test
    fun aChoiceSurvivesAndOtherSavesDoNotMakeOne() {
        val prefs = MemoryPrefs()
        val first = preferences(prefs)
        first.update(first.settings.value.copy(enabled = true))
        assertFalse(preferences(prefs).settings.value.allowTools)

        val second = preferences(prefs)
        second.update(second.settings.value.copy(allowTools = true))
        assertTrue(preferences(prefs).settings.value.allowTools)
    }
}
