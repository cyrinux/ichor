package name.levis.ichor

import name.levis.ichor.ui.importconfig.QrPart
import name.levis.ichor.ui.importconfig.QrPartCollector
import name.levis.ichor.ui.importconfig.QrPartError
import name.levis.ichor.ui.importconfig.QrPartOutcome
import name.levis.ichor.ui.importconfig.QrPartSet
import name.levis.ichor.ui.importconfig.QrScanFeedback
import name.levis.ichor.ui.importconfig.QrScanNotice
import name.levis.ichor.ui.importconfig.QrScanStep
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class QrPartCollectorTest {

    private var joined: String? = null
    private val collector = QrPartCollector { json -> joined = json; "ichor-config:joined" }

    private fun progress(outcome: QrPartOutcome): QrPartOutcome.Progress = outcome as QrPartOutcome.Progress

    @Test
    fun firstPartStartsTheSetAndReportsMissingIndexes() {
        val out = progress(collector.add(QrPartSet.EMPTY, QrPart(2, 3, "bbb")))
        assertTrue(out.isNew)
        assertEquals(3, out.set.total)
        assertEquals(1, out.set.received)
        assertEquals(listOf(1, 3), out.set.missing)
        assertFalse(out.set.isComplete)
    }

    @Test
    fun exactDuplicateIsIgnored() {
        val set = progress(collector.add(QrPartSet.EMPTY, QrPart(1, 3, "aaa"))).set
        val out = progress(collector.add(set, QrPart(1, 3, "aaa")))
        assertFalse(out.isNew)
        assertEquals(set, out.set)
    }

    @Test
    fun partsInAnyOrderCompleteAndJoinInIndexOrder() {
        var set = QrPartSet.EMPTY
        for (part in listOf(QrPart(3, 3, "ccc"), QrPart(1, 3, "aaa"))) {
            set = progress(collector.add(set, part)).set
        }
        val out = collector.add(set, QrPart(2, 3, "bbb"))
        assertEquals(QrPartOutcome.Complete("ichor-config:joined"), out)
        val json = joined.orEmpty()
        assertTrue(json.indexOf("aaa") < json.indexOf("bbb") && json.indexOf("bbb") < json.indexOf("ccc"))
        assertTrue(json.contains("\"index\":1") && json.contains("\"total\":3"))
    }

    @Test
    fun aSinglePartSetCompletesAtOnce() {
        assertEquals(QrPartOutcome.Complete("ichor-config:joined"), collector.add(QrPartSet.EMPTY, QrPart(1, 1, "aaa")))
    }

    @Test
    fun partWithAnotherTotalResetsTheSet() {
        val set = progress(collector.add(QrPartSet.EMPTY, QrPart(1, 3, "aaa"))).set
        val out = collector.add(set, QrPart(2, 4, "xxx")) as QrPartOutcome.Reset
        assertEquals(4, out.set.total)
        assertEquals(listOf(1, 3, 4), out.set.missing)
    }

    @Test
    fun sameIndexWithAnotherChunkResetsTheSet() {
        val set = progress(collector.add(QrPartSet.EMPTY, QrPart(1, 2, "aaa"))).set
        val out = collector.add(set, QrPart(1, 2, "zzz")) as QrPartOutcome.Reset
        assertEquals(mapOf(1 to "zzz"), out.set.chunks)
    }

    @Test
    fun outOfRangeIndexIsInvalid() {
        assertEquals(QrPartOutcome.Failed(QrPartError.INVALID), collector.add(QrPartSet.EMPTY, QrPart(4, 3, "aaa")))
        assertEquals(QrPartOutcome.Failed(QrPartError.INVALID), collector.add(QrPartSet.EMPTY, QrPart(0, 3, "aaa")))
    }

    @Test
    fun joinErrorsMapToTheirCode() {
        val cases = mapOf(
            "qr-parts-mismatch: chunks from different sets" to QrPartError.MISMATCH,
            "qr-parts-missing: part 2" to QrPartError.MISSING,
            "qr-parts-invalid: not a config" to QrPartError.INVALID,
            "something else" to QrPartError.INVALID,
        )
        for ((message, error) in cases) {
            val failing = QrPartCollector { throw Exception(message) }
            assertEquals(QrPartOutcome.Failed(error), failing.add(QrPartSet.EMPTY, QrPart(1, 1, "aaa")))
        }
        assertEquals(QrPartError.INVALID, QrPartError.fromMessage(null))
    }

    @Test
    fun parseReadsGoJsonAndRejectsTheRest() {
        assertEquals(QrPart(2, 3, "abc_-"), QrPartCollector.parse("""{"index":2,"total":3,"chunk":"abc_-"}"""))
        assertNull(QrPartCollector.parse(""))
        assertNull(QrPartCollector.parse("not json"))
    }

    @Test
    fun toStringNeverShowsAChunk() {
        assertFalse(QrPart(1, 2, "secretchunk").toString().contains("secretchunk"))
        assertFalse(QrPartSet(2, mapOf(1 to "secretchunk")).toString().contains("secretchunk"))
    }

    @Test
    fun toStringNeverShowsTheJoinedConfig() {
        assertFalse(QrPartOutcome.Complete("ichor-config:secret").toString().contains("secret"))
        assertFalse(QrScanStep.Deliver("ichor-config:secret", null).toString().contains("secret"))
    }

    /** Ichorgo.qrPart for `ichor:<i>/<n>:<chunk>`; "" for anything else. */
    private fun decode(text: String): String {
        if (!text.startsWith("ichor:")) return ""
        val (position, chunk) = text.removePrefix("ichor:").split(":", limit = 2)
        val (index, total) = position.split("/")
        return """{"index":$index,"total":$total,"chunk":"$chunk"}"""
    }

    private fun update(step: QrScanStep): QrScanStep.Update = step as QrScanStep.Update

    @Test
    fun oneCodeConfigIsDeliveredWhenNoSetIsCollected() {
        assertEquals(QrScanStep.Deliver("context: a", null), collector.scan(QrPartSet.EMPTY, "context: a", ::decode))
    }

    @Test
    fun oneCodeConfigIsIgnoredWithANoticeWhileASetIsCollected() {
        val set = update(collector.scan(QrPartSet.EMPTY, "ichor:1/3:aaa", ::decode)).set
        val step = update(collector.scan(set, "context: a", ::decode))
        assertEquals(set, step.set)
        assertEquals(QrScanNotice.FINISH_FIRST, step.notice)
    }

    @Test
    fun newPartAndCompletionPlaySuccess() {
        val first = update(collector.scan(QrPartSet.EMPTY, "ichor:1/2:aaa", ::decode))
        assertEquals(QrScanFeedback.SUCCESS, first.feedback)
        assertNull(first.notice)
        assertEquals(QrScanStep.Ignore, collector.scan(first.set, "ichor:1/2:aaa", ::decode))
        assertEquals(
            QrScanStep.Deliver("ichor-config:joined", QrScanFeedback.SUCCESS),
            collector.scan(first.set, "ichor:2/2:bbb", ::decode),
        )
    }

    @Test
    fun failedJoinDropsTheSetShowsWhyAndKeepsScanning() {
        val failing = QrPartCollector { throw Exception("qr-parts-invalid: gzip") }
        val set = update(failing.scan(QrPartSet.EMPTY, "ichor:1/2:aaa", ::decode)).set
        val failed = update(failing.scan(set, "ichor:2/2:bbb", ::decode))
        assertEquals(QrPartSet.EMPTY, failed.set)
        assertEquals(QrScanNotice.INVALID, failed.notice)
        assertEquals(QrScanFeedback.WARNING, failed.feedback)
        // Scanning goes on: the next part starts a new set.
        val next = update(failing.scan(failed.set, "ichor:1/2:aaa", ::decode))
        assertEquals(1, next.set.received)
    }

    @Test
    fun anotherSetResetsWithAWarning() {
        val set = update(collector.scan(QrPartSet.EMPTY, "ichor:1/3:aaa", ::decode)).set
        val step = update(collector.scan(set, "ichor:1/2:zzz", ::decode))
        assertEquals(QrScanNotice.RESET, step.notice)
        assertEquals(QrScanFeedback.WARNING, step.feedback)
    }

    @Test
    fun malformedPartKeepsTheSet() {
        val set = update(collector.scan(QrPartSet.EMPTY, "ichor:1/3:aaa", ::decode)).set
        val step = update(collector.scan(set, "ichor:x", { throw Exception("qr-parts-invalid: bad") }))
        assertEquals(set, step.set)
        assertEquals(QrScanNotice.INVALID, step.notice)
    }
}
