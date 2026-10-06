package name.levis.ichor.util

import java.io.ByteArrayOutputStream
import java.io.InputStream

/** Reads at most [limit] bytes (InputStream.readNBytes needs API 33). */
fun InputStream.readAtMost(limit: Int): ByteArray {
    val out = ByteArrayOutputStream()
    val buffer = ByteArray(16 * 1024)
    while (out.size() < limit) {
        val n = read(buffer, 0, minOf(buffer.size, limit - out.size()))
        if (n < 0) break
        out.write(buffer, 0, n)
    }
    return out.toByteArray()
}

/** Reads the whole stream, failing with [tooLarge] once it holds more than [maxBytes]. */
fun readBounded(stream: InputStream, maxBytes: Int, tooLarge: String = "File is too large"): ByteArray {
    val bytes = stream.readAtMost(maxBytes + 1)
    require(bytes.size <= maxBytes) { tooLarge }
    return bytes
}
