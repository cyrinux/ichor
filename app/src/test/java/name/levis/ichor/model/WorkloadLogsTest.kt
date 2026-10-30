package name.levis.ichor.model

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkloadLogsTest {
    private val sample = """
        {"pods":[{"name":"api-7d9f-x2k4","container":"app","color":0,"lines":1,"truncated":false}],
         "lines":[{"pod":"api-7d9f-x2k4","time":1000,"text":"request timeout","level":"error"}],
         "matched":1,"scanned":3,"truncated":true,"sinceMinutes":60}
    """.trimIndent()

    @Test fun decodesTheCoreAnswer() {
        val logs = Json.decodeFromString(WorkloadLogs.serializer(), sample)
        assertEquals("app", logs.pods.single().container)
        assertEquals(1, logs.matched)
        assertEquals(3, logs.scanned)
        assertTrue(logs.truncated)
    }

    @Test fun podSuffixTellsThePodsApart() {
        assertEquals("x2k4", WorkloadLogLine(pod = "api-7d9f-x2k4").podSuffix)
        assertEquals("0", WorkloadLogLine(pod = "db-0").podSuffix)
        assertEquals("solo", WorkloadLogLine(pod = "solo").podSuffix)
    }

    @Test fun asTextHoldsTimePodAndLine() {
        val logs = Json.decodeFromString(WorkloadLogs.serializer(), sample)
        assertEquals("1970-01-01T00:00:01Z api-7d9f-x2k4 request timeout\n", logs.asText())
    }

    @Test fun asTextMarksAnUnknownTime() {
        val logs = WorkloadLogs(lines = listOf(WorkloadLogLine(pod = "db-0", text = "up")))
        assertEquals("- db-0 up\n", logs.asText())
    }
}
