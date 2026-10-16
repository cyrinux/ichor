package name.levis.ichor.ui.importconfig

import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.ChecksumException
import com.google.zxing.DecodeHintType
import com.google.zxing.FormatException
import com.google.zxing.LuminanceSource
import com.google.zxing.MultiFormatReader
import com.google.zxing.NotFoundException
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.Result
import com.google.zxing.ResultMetadataType
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import java.io.ByteArrayOutputStream
import java.util.Base64

/**
 * QR decoding for the import scanner, free of Android so it runs in unit tests: a camera
 * frame's luminance in, the text to import out (plain text, an "ichor-config:" text, or a
 * binary gzip code turned into an "ichor-config:" text, as go/ichorgo/qr_payload.go does).
 */
object QrPayload {
    private const val IMPORT_TEXT_PREFIX = "ichor-config:"
    private val GZIP_MAGIC = byteArrayOf(0x1f, 0x8b.toByte())

    private val HINTS: Map<DecodeHintType, Any> = mapOf(
        DecodeHintType.POSSIBLE_FORMATS to listOf(BarcodeFormat.QR_CODE),
        // A whole talosconfig is a dense version-40 code: search the full frame thoroughly.
        DecodeHintType.TRY_HARDER to true,
        DecodeHintType.CHARACTER_SET to Charsets.UTF_8.name(),
    )

    /** A reader set up for QR codes; not thread-safe, keep one per analyzer thread. */
    fun newReader(): MultiFormatReader = MultiFormatReader().apply { setHints(HINTS) }

    /**
     * The luminance of a camera Y plane, [rowStride] bytes per row (at least [width]). The
     * plane's last row may stop at [width], so a short buffer is padded to whole rows.
     */
    fun lumaSource(y: ByteArray, width: Int, height: Int, rowStride: Int): LuminanceSource {
        require(width > 0 && height > 0 && rowStride >= width) { "bad frame ${width}x$height/$rowStride" }
        val full = rowStride * height
        val data = if (y.size >= full) y else y.copyOf(full)
        return PlanarYUVLuminanceSource(data, rowStride, height, 0, 0, width, height, false)
    }

    /**
     * Decodes the QR code in [source], or null when there is none. The hybrid binarizer copes
     * with uneven lighting; the global histogram one sometimes locks a blurred dense code it
     * misses, at little extra cost once the first has failed.
     */
    fun decode(reader: MultiFormatReader, source: LuminanceSource): Result? {
        val bitmaps = sequenceOf(
            { BinaryBitmap(HybridBinarizer(source)) },
            { BinaryBitmap(GlobalHistogramBinarizer(source)) },
        )
        return bitmaps.firstNotNullOfOrNull { bitmap ->
            try {
                reader.decodeWithState(bitmap())
            } catch (_: NotFoundException) {
                null
            } catch (_: ChecksumException) {
                null
            } catch (_: FormatException) {
                null
            } finally {
                reader.reset()
            }
        }
    }

    /**
     * The text to import from [result]. A binary code (raw gzip, `qrencode -8`) is read from
     * its byte segments, never from the text, which ZXing decoded with a guessed charset.
     */
    fun text(result: Result): String {
        val bytes = byteSegments(result)
        if (bytes != null && bytes.startsWith(GZIP_MAGIC)) {
            return IMPORT_TEXT_PREFIX + Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
        }
        return result.text.orEmpty()
    }

    private fun byteSegments(result: Result): ByteArray? {
        val segments = result.resultMetadata?.get(ResultMetadataType.BYTE_SEGMENTS) as? List<*> ?: return null
        val out = ByteArrayOutputStream()
        segments.filterIsInstance<ByteArray>().forEach { out.write(it) }
        return out.toByteArray().takeIf { it.isNotEmpty() }
    }

    private fun ByteArray.startsWith(prefix: ByteArray): Boolean =
        size >= prefix.size && prefix.indices.all { this[it] == prefix[it] }
}
