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

    /** Every part is in: [text] is what a one-code scan would have given. */
    data class Complete(val text: String) : QrPartOutcome

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

    companion object {
        /** The part in [json] (QRPart's output), or null when it is not one. */
        fun parse(json: String): QrPart? =
            if (json.isBlank()) null else runCatching { JSON.decodeFromString<QrPart>(json) }.getOrNull()
    }
}

private val JSON = Json { ignoreUnknownKeys = true }
