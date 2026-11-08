package name.levis.ichor.model

import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ImageScanTest {

    // As go/ichorgo/imagescan_report.go writes it.
    private val json = """{"source":"scan","scanner":"Trivy 0.75.0","started":1,"finished":2,"images":[
        {"image":"nginx:1.25","ref":"docker.io/library/nginx@sha256:abc","digest":"sha256:abc","os":"debian 12.4",
         "pods":["web/a"],"scannedAt":2,"summary":{"critical":1,"high":1,"medium":0,"low":1,"unknown":0,"fixable":2,"os":3},
         "vulnerabilities":[
          {"id":"CVE-1","package":"libssl3","installed":"3.0.11","fixed":"3.0.13","severity":"CRITICAL","score":9.8,"class":"os-pkgs","target":"nginx (debian 12.4)"},
          {"id":"CVE-2","package":"libssl3","installed":"3.0.11","fixed":"3.0.13","severity":"HIGH","class":"os-pkgs"},
          {"id":"CVE-3","package":"perl-base","installed":"5.36","severity":"LOW","class":"os-pkgs"}]},
        {"image":"ghcr.io/x/app:2","ref":"ghcr.io/x/app:2","pods":["web/a"],"error":"the image is no longer in its registry",
         "summary":{"critical":0,"high":0,"medium":0,"low":0,"unknown":0,"fixable":0,"os":0},"vulnerabilities":[]}]}"""

    private val report = TalosJson.decodeFromString(ImageScanReport.serializer(), json)

    @Test
    fun decodesTheGoJson() {
        val nginx = report.images[0]
        assertEquals("debian 12.4", nginx.os)
        assertEquals("libssl3", nginx.vulnerabilities[0].`package`)
        assertEquals("os-pkgs", nginx.vulnerabilities[0].`class`)
        assertEquals(VulnSeverity.CRITICAL, nginx.vulnerabilities[0].level)
        assertEquals(1, report.failed)
        assertEquals(VulnSummary(critical = 1, high = 1, low = 1, fixable = 2, os = 3), report.summary)
        assertEquals(3, report.summary.total)

        val operator = TalosJson.decodeFromString(OperatorReports.serializer(), """{"available":true,"report":$json}""")
        assertTrue(operator.available)
        assertEquals(2, operator.report.images.size)
    }

    @Test
    fun filtersAndGroupsByPackage() {
        val nginx = report.images[0]

        // Fixable only (the default): perl-base has no fix.
        val fixable = nginx.byPackage(VulnFilter())
        assertEquals(listOf("libssl3 3.0.11"), fixable.map { it.first })
        assertEquals(listOf("CVE-1", "CVE-2"), fixable[0].second.map { it.id })

        val all = nginx.byPackage(VulnFilter(fixableOnly = false))
        assertEquals(listOf("libssl3 3.0.11", "perl-base 5.36"), all.map { it.first })

        val noHigh = VulnFilter(fixableOnly = false).toggle(VulnSeverity.HIGH)
        assertFalse(VulnSeverity.HIGH in noHigh.severities)
        assertEquals(listOf("CVE-1", "CVE-3"), nginx.byPackage(noHigh).flatMap { p -> p.second.map { it.id } })
        assertTrue(VulnSeverity.HIGH in noHigh.toggle(VulnSeverity.HIGH).severities)
    }

    @Test
    fun severityOfUnknownNames() {
        assertEquals(VulnSeverity.UNKNOWN, severityOf("bogus"))
        assertEquals(VulnSeverity.MEDIUM, severityOf("MEDIUM"))
        assertEquals(VulnSummary(high = 2, fixable = 1), VulnSummary(high = 1) + VulnSummary(high = 1, fixable = 1))
    }

    @Test
    fun exportFormatsMatchTheCore() {
        assertEquals(listOf("html", "sarif", "cyclonedx", "csv", "json"), ImageScanFormat.entries.map { it.id })
    }

    @Test
    fun onlyAScanThatScannedSomethingReplacesTheOperatorReport() {
        val done = ImageScanSession("ctx", "nginx", report = report, running = false)
        assertEquals(report, done.usableReport)
        assertNull(done.copy(running = true).usableReport)
        assertNull(done.copy(stopped = true).usableReport)
        assertNull(done.copy(error = "image scan timed out").usableReport)

        // Stopped before the first image: every image "not scanned".
        val nothing = report.copy(images = report.images.map { it.copy(error = IMAGESCAN_NOT_SCANNED) })
        assertNull(done.copy(report = nothing).usableReport)
    }

    @Test
    fun scannerIsGrypeUnlessTrivyWasPicked() {
        assertEquals(ImageScanner.GRYPE, ImageScanner.parse(null))
        assertEquals(ImageScanner.GRYPE, ImageScanner.parse("other"))
        assertEquals(ImageScanner.TRIVY, ImageScanner.parse("trivy"))
        assertEquals(ImageScanner.GRYPE, ImageScanSession("ctx", "nginx").scanner)
    }

    @Test
    fun oldReportsDecodeWithoutTheNewFields() {
        // A report kept before the "new since the last scan" fields: nothing new, nothing to compare.
        assertEquals(VulnSummary(), report.newSummary)
        assertFalse(report.hasPrevious)
        assertFalse(report.images[0].vulnerabilities.any { it.new })
    }

    @Test
    fun marksWhatIsNewSinceTheLastScan() {
        val marked = TalosJson.decodeFromString(
            ImageScanReport.serializer(),
            """{"images":[{"image":"nginx:1.25","previousScannedAt":5,"summary":{"critical":1,"high":1},"new":{"high":1,"fixable":1},
                "vulnerabilities":[{"id":"CVE-1","severity":"CRITICAL","fixed":"1"},{"id":"CVE-2","severity":"HIGH","fixed":"2","new":true}]}]}""",
        )
        assertTrue(marked.hasPrevious)
        assertEquals(1, marked.newSummary.total)
        val newOnly = VulnFilter(newOnly = true)
        assertEquals(listOf("CVE-2"), marked.images[0].vulnerabilities.filter(newOnly::keeps).map { it.id })
        assertEquals(2, marked.images[0].vulnerabilities.count(VulnFilter()::keeps))
    }

    @Test
    fun savedScans() {
        val saved = TalosJson.decodeFromString(
            kotlinx.serialization.builtins.ListSerializer(SavedScan.serializer()),
            """[{"id":"2-aaaaaaaa","cluster":"lab","app":"grafana","scanner":"Grype 0.80","finished":2,"images":1,"summary":{"high":2},"new":{"high":1},"digests":["sha256:a"]},
                {"id":"1-bbbbbbbb","cluster":"lab","app":"grafana","finished":1,"summary":{},"new":{}},
                {"id":"0-cccccccc","cluster":"lab","app":"talos:10.0.0.2","finished":0}]""",
        )
        assertEquals("2-aaaaaaaa", saved.lastOf("grafana")?.id)
        assertNull(saved.lastOf("loki"))
        assertEquals(1, saved[0].new.total)
        assertEquals("10.0.0.2", systemImagesNode(saved[2].app))
        assertNull(systemImagesNode("grafana"))
        assertEquals("app", ImageScanOptions(app = "app").app)
    }
}
