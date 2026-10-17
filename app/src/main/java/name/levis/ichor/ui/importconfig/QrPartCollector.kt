package name.levis.ichor.ui.importconfig

import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/**
 * One code of a config split over several QR codes (`ichor:<i>/<n>:<chunk>`, made by
 * `just qr-parts`), as go/ichorgo/qr_parts.go QRPart parses it. [index] is 1-based.
 * The chunk is part of a credential: never log it.
 */
@Serializable
data class QrPart(val index: Int, val total: Int, val chunk: String) {
    override fun toString(): String = "QrPart($index/$total)"
}

/** Why a set of parts cannot be imported, from the Go error code prefix. */
enum class QrPartError(val code: String) {
    MISMATCH("qr-parts-mismatch"),
    MISSING("qr-parts-missing"),
    INVALID("qr-parts-invalid"),
    ;

    companion object {
        /** The error a Go message starts with; anything else is an invalid part. */
        fun fromMessage(message: String?): QrPartError =
            entries.firstOrNull { message?.startsWith(it.code + ":") == true || message == it.code } ?: INVALID
    }
}

/** The parts collected so far: [total] is 0 before the first part. Immutable. */
data class QrPartSet(val total: Int = 0, val chunks: Map<Int, String> = emptyMap()) {
    val received: Int get() = chunks.size
    val missing: List<Int> get() = (1..total).filter { it !in chunks }
    val isEmpty: Boolean get() = total == 0
    val isComplete: Boolean get() = total > 0 && missing.isEmpty()

    fun with(part: QrPart): QrPartSet = copy(total = part.total, chunks = chunks + (part.index to part.chunk))

    /** The JSON array QRPartsJoin takes, in index order. */
    fun toJson(): String =
        JSON.encodeToString(chunks.toSortedMap().map { (index, chunk) -> QrPart(index, total, chunk) })

    override fun toString(): String = "QrPartSet($received/$total)"

    companion object {
        val EMPTY = QrPartSet()
    }
}

/** What adding one scanned part did. */
sealed interface QrPartOutcome {
    /** The part was kept ([isNew]) or was already there; [set] is the new state. */
    data class Progress(val set: QrPartSet, val isNew: Boolean) : QrPartOutcome

    /** The part belongs to another set: collection restarted from it. */
    data class Reset(val set: QrPartSet) : QrPartOutcome

    /** Every part is in: [text] is what a one-code scan would have given. A credential: never logged. */
    data class Complete(val text: String) : QrPartOutcome {
        override fun toString(): String = "Complete(${text.length} chars)"
    }

    /** The complete set could not be joined. */
    data class Failed(val error: QrPartError) : QrPartOutcome
}

/**
 * The collection rules of a multi-part QR scan, free of Android and gomobile so they run in
 * unit tests: [join] is Ichorgo.qrPartsJoin (throws with a `qr-parts-*:` message).
 */
class QrPartCollector(private val join: (String) -> String) {

    fun add(set: QrPartSet, part: QrPart): QrPartOutcome {
        if (part.total < 1 || part.index !in 1..part.total) return QrPartOutcome.Failed(QrPartError.INVALID)
        val known = set.chunks[part.index]
        val sameSet = set.isEmpty || (set.total == part.total && (known == null || known == part.chunk))
        if (!sameSet) return finish(QrPartSet.EMPTY.with(part), reset = true)
        if (known != null) return QrPartOutcome.Progress(set, isNew = false)
        return finish(set.with(part), reset = false)
    }

    private fun finish(next: QrPartSet, reset: Boolean): QrPartOutcome = when {
        next.isComplete -> joinAll(next)
        reset -> QrPartOutcome.Reset(next)
        else -> QrPartOutcome.Progress(next, isNew = true)
    }

    private fun joinAll(set: QrPartSet): QrPartOutcome =
        try {
            QrPartOutcome.Complete(join(set.toJson()))
        } catch (e: Exception) {
            QrPartOutcome.Failed(QrPartError.fromMessage(e.message))
        }

    /**
     * What the scanner does with one scanned [text] given the [set] so far. [decode] is
     * Ichorgo.qrPart: the part's JSON, "" for a one-code config, or a `qr-parts-*:` error.
     */
    fun scan(set: QrPartSet, text: String, decode: (String) -> String): QrScanStep {
        val json = try {
            decode(text)
        } catch (e: Exception) {
            // A malformed part: the set is kept.
            return QrScanStep.Update(set, QrScanNotice.of(QrPartError.fromMessage(e.message)), QrScanFeedback.WARNING)
        }
        if (json.isEmpty()) {
            // A one-code config imports as before, but not over a set being collected.
            return if (set.isEmpty) QrScanStep.Deliver(text, feedback = null) else QrScanStep.Update(set, QrScanNotice.FINISH_FIRST, null)
        }
        val part = parse(json)
            ?: return QrScanStep.Update(set, QrScanNotice.INVALID, QrScanFeedback.WARNING)
        return when (val outcome = add(set, part)) {
            is QrPartOutcome.Progress ->
                if (outcome.isNew) QrScanStep.Update(outcome.set, null, QrScanFeedback.SUCCESS) else QrScanStep.Ignore
            is QrPartOutcome.Reset -> QrScanStep.Update(outcome.set, QrScanNotice.RESET, QrScanFeedback.WARNING)
            is QrPartOutcome.Complete -> QrScanStep.Deliver(outcome.text, QrScanFeedback.SUCCESS)
            // The set cannot be imported: drop it and keep scanning, with the reason shown.
            is QrPartOutcome.Failed ->
                QrScanStep.Update(QrPartSet.EMPTY, QrScanNotice.of(outcome.error), QrScanFeedback.WARNING)
        }
    }

    companion object {
        /** The part in [json] (QRPart's output), or null when it is not one. */
        fun parse(json: String): QrPart? =
            if (json.isBlank()) null else runCatching { JSON.decodeFromString<QrPart>(json) }.getOrNull()
    }
}

/** The message under the progress ring. */
enum class QrScanNotice {
    FINISH_FIRST,
    RESET,
    MISMATCH,
    MISSING,
    INVALID,
    ;

    companion object {
        fun of(error: QrPartError): QrScanNotice = when (error) {
            QrPartError.MISMATCH -> MISMATCH
            QrPartError.MISSING -> MISSING
            QrPartError.INVALID -> INVALID
        }
    }
}

/** The haptic of a scan step: success on a new part or the import, warning on a reset or failure. */
enum class QrScanFeedback { SUCCESS, WARNING }

/** What the scanner does with one scanned text. */
sealed interface QrScanStep {
    /** Import [text] (a one-code config, or a completed set with [feedback]): a credential, never logged. */
    data class Deliver(val text: String, val feedback: QrScanFeedback?) : QrScanStep {
        override fun toString(): String = "Deliver(${text.length} chars)"
    }

    /** Show [set] and [notice] (null clears it), playing [feedback] when not null. */
    data class Update(val set: QrPartSet, val notice: QrScanNotice?, val feedback: QrScanFeedback?) : QrScanStep

    /** Nothing changed (a part already collected). */
    data object Ignore : QrScanStep
}

private val JSON = Json { ignoreUnknownKeys = true }
