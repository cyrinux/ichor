package name.levis.ichor.util

import org.junit.Assert.assertEquals
import org.junit.Test

class StartupTraceTest {

    @Test
    fun lineHasAbsoluteAndDeltaMillis() {
        assertEquals("config parsed 23 contexts +3120ms (Δ410ms)", StartupTrace.line("config parsed 23 contexts", 3120, 410))
    }

    @Test
    fun goStepsKeepCountsOnly() {
        assertEquals(
            "go:overview.all_nodes 8003ms count=8 unreachable=1",
            StartupTrace.goStep("""{"step":"overview.all_nodes","ms":8003,"at":1791750233034,"count":8,"unreachable":1}"""),
        )
        assertEquals("go:config.parse 12ms count=23", StartupTrace.goStep("""{"step":"config.parse","ms":12,"at":1,"count":23}"""))
        assertEquals("go:session.reused 0ms", StartupTrace.goStep("""{"step":"session.reused","ms":0,"at":1}"""))
    }

    @Test
    fun anUnexpectedGoStepIsKeptAsItCame() {
        assertEquals("go:not json", StartupTrace.goStep("not json"))
    }
}
