package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Test

class ImageReportTest {
    @Test
    fun reportNodesAreEveryNodeRunningAnAppOnce() {
        val inventory = Inventory(
            apps = listOf(
                InventoryApp(id = "web", name = "web", nodes = listOf("n2", "n1")),
                InventoryApp(id = "db", name = "db", nodes = listOf("n1", "")),
                InventoryApp(id = "idle", name = "idle"),
            ),
        )

        assertEquals(listOf("n1", "n2"), inventory.reportNodes)
    }

    @Test
    fun unusedBytesAddEveryNode() {
        val report = ImageReport(
            unusedOnNode = listOf(ReportUnused("n1", "a:1", 10), ReportUnused("n2", "a:1", 10), ReportUnused("n2", "b:2", 5)),
        )

        assertEquals(25L, report.unusedBytes)
    }
}
