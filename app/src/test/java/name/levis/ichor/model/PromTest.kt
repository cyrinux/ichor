package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.metrics.compact
import name.levis.ichor.ui.metrics.formatMetric
import org.junit.Assert.*
import org.junit.Test

class PromTest {
    @Test fun resultWithGapsDecodes() {
        val raw = """{"resultType":"matrix","times":[1000,2000,3000],"series":[{"name":"up{job=\"a\"}","labels":{"job":"a","pod":"p1"},"values":[1.5,null,2]}],"warnings":null,"truncated":false,"total":1}"""
        val res = TalosJson.decodeFromString(PromResult.serializer(), raw)
        assertEquals(listOf(1.5, null, 2.0), res.series[0].values)
        assertEquals(emptyList<String>(), res.warnings)
        assertEquals(2.0, res.series[0].latest()!!, 0.0)
    }

    @Test fun legendFillsLabels() {
        val s = PromSeries(name = "x{a=\"1\"}", labels = mapOf("namespace" to "kube-system", "pod" to "coredns"))
        assertEquals("kube-system/coredns", s.legend("{{namespace}}/{{ pod }}"))
        assertEquals("x{a=\"1\"}", s.legend(""))
        // Labels the series lacks leave nothing: the name stands in.
        assertEquals("x{a=\"1\"}", s.legend("{{missing}}"))
    }

    @Test fun sourceKeepsSecretOutOfLabel() {
        val proxy = PromSource(namespace = "monitoring", service = "prometheus-operated", port = 9090)
        assertEquals("monitoring/prometheus-operated:9090", proxy.label)
        val url = PromSource(mode = PromSource.MODE_URL, url = "https://m.example/prometheus", secret = "s3cret")
        assertEquals("https://m.example/prometheus", url.label)
        assertFalse(url.toString().contains("s3cret"))
    }

    @Test fun goGetsEveryFieldEvenDefaults() {
        val json = PromSource(namespace = "monitoring", service = "mimir", port = 8080).toGoJson()
        assertTrue(json, json.contains("\"mode\":\"proxy\""))
        assertTrue(json, json.contains("\"auth\":\"\""))
    }

    @Test fun valuesFormatByUnit() {
        assertEquals("42.1%", formatMetric(42.123, "percent"))
        assertEquals("1.5 GiB", formatMetric(1.5 * (1 shl 30), "bytes"))
        assertEquals("0.25", formatMetric(0.25, "cores"))
        assertEquals("3.2/s", formatMetric(3.2, "persec"))
        assertEquals("7", formatMetric(7.0, "count"))
        assertEquals("1.23M", compact(1_234_567.0))
        assertEquals("12.5", compact(12.5))
        assertEquals("1.23e-04", compact(0.000123))
        assertEquals("0", compact(0.0))
        assertEquals("100", compact(100.0))
        assertEquals("200", compact(200.0))
        assertEquals("1000", compact(999.6))
        assertEquals("1235", compact(1234.5))
        assertEquals("1", compact(1.0))
        assertEquals("50", compact(50.0))
        assertEquals("-12.5", compact(-12.5))
        assertEquals("100/s", formatMetric(100.0, "persec"))
    }
}
