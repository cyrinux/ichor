package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class LogsBackendTest {
    @Test
    fun decodesTheGoAnswers() {
        val found = TalosJson.decodeFromString(
            LogsDiscovery.serializer(),
            """{"candidates":[{"kind":"loki","source":{"mode":"proxy","kind":"loki","namespace":"monitoring","service":"loki-gateway","port":80}}]}""",
        )
        assertEquals("monitoring/loki-gateway:80", found.candidates.single().source.label)

        val result = TalosJson.decodeFromString(
            LogsResult.serializer(),
            """{"lines":[{"time":3000,"labels":{"pod":"a"},"text":"hello"}],"stats":{"returned":1,"truncated":true}}""",
        )
        assertEquals(LogsResult(listOf(LogsLine(3000, mapOf("pod" to "a"), "hello")), LogsStats(1, true)), result)
    }

    @Test
    fun savedQueriesKeepTheNewestOnce() {
        assertEquals(listOf("b", "a"), savedWith(listOf("a", "b"), " b "))
        assertEquals(listOf("a"), savedWith(listOf("a"), "  "))
        val full = (1..LOG_SEARCH_MAX_SAVED).map { "q$it" }
        val saved = savedWith(full, "new")
        assertEquals(LOG_SEARCH_MAX_SAVED, saved.size)
        assertEquals("new", saved.first())
        assertEquals("q${LOG_SEARCH_MAX_SAVED - 1}", saved.last())
    }

    @Test
    fun addsAMatcherToALokiSelector() {
        assertEquals("""{namespace="demo"}""", withLabelMatcher(LOGS_KIND_LOKI, "", "namespace", "demo"))
        assertEquals("""{pod="a",namespace="demo"} |= "x"""", withLabelMatcher(LOGS_KIND_LOKI, """{pod="a"} |= "x"""", "namespace", "demo"))
        assertEquals("""{namespace="demo"}""", withLabelMatcher(LOGS_KIND_LOKI, "{}", "namespace", "demo"))
        assertEquals("""{a="b\"c"} x""", withLabelMatcher(LOGS_KIND_LOKI, "x", "a", "b\"c"))
    }

    @Test
    fun addsAMatcherToAVictoriaLogsQuery() {
        assertEquals("""namespace:"demo"""", withLabelMatcher(LOGS_KIND_VICTORIALOGS, "", "namespace", "demo"))
        assertEquals("""timeout AND namespace:"demo"""", withLabelMatcher(LOGS_KIND_VICTORIALOGS, "timeout", "namespace", "demo"))
    }

    @Test
    fun sharedTextIsOldestFirst() {
        val result = LogsResult(listOf(LogsLine(2, text = "new"), LogsLine(1, text = "old")))
        assertEquals("1 old\n2 new", result.asText(Long::toString))
    }
}
