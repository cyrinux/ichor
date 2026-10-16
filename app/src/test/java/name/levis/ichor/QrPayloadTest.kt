package name.levis.ichor

import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.common.BitMatrix
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import name.levis.ichor.ui.importconfig.QrPayload
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.util.Base64
import java.util.zip.GZIPInputStream
import java.util.zip.GZIPOutputStream
import kotlin.random.Random

class QrPayloadTest {

    private val sampleConfig = """
        context: lab
        contexts:
          lab:
            endpoints:
              - 192.0.2.10
            ca: c2FtcGxlLWNh
            crt: c2FtcGxlLWNydA==
            key: c2FtcGxlLWtleQ==
    """.trimIndent()

    @Test
    fun plainYamlIsReturnedAsIs() {
        assertEquals(sampleConfig, scan(encodeText(sampleConfig)))
    }

    @Test
    fun compressedTextFormIsUnchanged() {
        val text = "ichor-config:" + Base64.getUrlEncoder().withoutPadding().encodeToString(gzip(sampleConfig.toByteArray()))
        assertEquals(text, scan(encodeText(text)))
    }

    @Test
    fun nonAsciiTextKeepsItsCharacters() {
        val text = "# café ☕\n$sampleConfig"
        assertEquals(text, scan(encodeText(text)))
    }

    @Test
    fun binaryGzipCodeBecomesTheCompressedTextForm() {
        val compressed = gzip(sampleConfig.toByteArray())
        val text = scan(encodeBytes(compressed))
        assertEquals(sampleConfig, gunzip(fromImportText(text)))
    }

    @Test
    fun denseVersion40BinaryCodeDecodesThroughAPaddedCameraPlane() {
        // About as much as a version-40 code holds in byte mode at level L (2953 bytes).
        val payload = byteArrayOf(0x1f, 0x8b.toByte()) + Random(80).nextBytes(2900)
        val matrix = encodeBytes(payload, scale = 3)
        assertEquals(177 * 3, matrix.width - 2 * QUIET_ZONE * 3)
        assertArrayEquals(payload, fromImportText(scan(matrix, rowPadding = 64, truncateLastRow = true)))
    }

    @Test
    fun aRotatedCodeDecodesWithoutRotatingTheFrame() {
        assertEquals(sampleConfig, scan(rotate90(encodeText(sampleConfig))))
    }

    @Test
    fun aFrameWithoutACodeDecodesToNull() {
        val blank = ByteArray(200 * 200) { 0xff.toByte() }
        assertNull(QrPayload.decode(QrPayload.newReader(), QrPayload.lumaSource(blank, 200, 200, 200)))
    }

    /** Renders [matrix] as a camera Y plane (dark modules 0, light 255) and decodes it. */
    private fun scan(matrix: BitMatrix, rowPadding: Int = 0, truncateLastRow: Boolean = false): String {
        val width = matrix.width
        val height = matrix.height
        val stride = width + rowPadding
        val plane = ByteArray(stride * height) { i ->
            val x = i % stride
            if (x < width && matrix[x, i / stride]) 0 else 0xff.toByte()
        }
        // A camera's Y buffer may end right after the last row's pixels.
        val y = if (truncateLastRow) plane.copyOf(stride * (height - 1) + width) else plane
        val result = QrPayload.decode(QrPayload.newReader(), QrPayload.lumaSource(y, width, height, stride))
            ?: error("no QR code found")
        return QrPayload.text(result)
    }

    private fun encodeText(text: String): BitMatrix = encode(
        text,
        mapOf(EncodeHintType.CHARACTER_SET to "UTF-8", EncodeHintType.MARGIN to QUIET_ZONE),
        scale = 4,
    )

    /** Byte mode with each byte as an ISO-8859-1 char, like `qrencode -8` (no ECI). */
    private fun encodeBytes(bytes: ByteArray, scale: Int = 4): BitMatrix = encode(
        String(bytes, Charsets.ISO_8859_1),
        mapOf(
            EncodeHintType.CHARACTER_SET to "ISO-8859-1",
            EncodeHintType.ERROR_CORRECTION to ErrorCorrectionLevel.L,
            EncodeHintType.MARGIN to QUIET_ZONE,
        ),
        scale,
    )

    private fun encode(contents: String, hints: Map<EncodeHintType, Any>, scale: Int): BitMatrix {
        // Size 0 yields one pixel per module; scale it up so the detector sees real modules.
        val modules = QRCodeWriter().encode(contents, BarcodeFormat.QR_CODE, 0, 0, hints)
        val out = BitMatrix(modules.width * scale, modules.height * scale)
        for (y in 0 until out.height) {
            for (x in 0 until out.width) {
                if (modules[x / scale, y / scale]) out.set(x, y)
            }
        }
        return out
    }

    private fun rotate90(matrix: BitMatrix): BitMatrix {
        val out = BitMatrix(matrix.height, matrix.width)
        for (y in 0 until matrix.height) {
            for (x in 0 until matrix.width) {
                if (matrix[x, y]) out.set(matrix.height - 1 - y, x)
            }
        }
        return out
    }

    private fun fromImportText(text: String): ByteArray {
        val prefix = "ichor-config:"
        check(text.startsWith(prefix)) { "not an import text: ${text.take(20)}" }
        return Base64.getUrlDecoder().decode(text.removePrefix(prefix))
    }

    private fun gzip(bytes: ByteArray): ByteArray =
        ByteArrayOutputStream().also { out -> GZIPOutputStream(out).use { it.write(bytes) } }.toByteArray()

    private fun gunzip(bytes: ByteArray): String = GZIPInputStream(bytes.inputStream()).use { String(it.readBytes()) }

    private companion object {
        const val QUIET_ZONE = 4
    }
}
