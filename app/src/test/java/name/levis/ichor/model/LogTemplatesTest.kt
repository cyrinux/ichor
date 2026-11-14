package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Test

class LogTemplatesTest {
    @Test
    fun decodesTheGoShape() {
        val json = """{"groups":[{"template":"connection to <*> refused after <*>","level":"error","source":"","count":1842,
            "share":0.61,"firstTs":1,"lastTs":2,"sample":"connection to 10.0.0.7:5432 refused after 30s","pods":2}],
            "lines":3000,"distinct":41}"""
        val templates = TalosJson.decodeFromString(LogTemplates.serializer(), json)
        val group = templates.groups.single()
        assertEquals(3000, templates.lines)
        assertEquals(LogLevel.ERROR, group.logLevel)
        assertEquals("connection to 10.0.0.7:5432 refused after 30s", group.sample)
        assertEquals(2, group.pods)
    }

    @Test
    fun shareIsAWholePercentAndARareMessageStaysVisible() {
        assertEquals("61 %", LogTemplateGroup(count = 1842, share = 0.614).shareText())
        assertEquals("<1 %", LogTemplateGroup(count = 1, share = 0.0004).shareText())
        assertEquals("100 %", LogTemplateGroup(count = 5, share = 1.0).shareText())
        assertEquals(LogLevel.WARN, LogTemplateGroup(level = "WARNING").logLevel)
    }
}
