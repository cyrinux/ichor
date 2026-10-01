package dev.talos.viewer.util

import java.io.ByteArrayOutputStream
import java.io.InputStream

/** Reads the whole stream, failing once more than [maxBytes] would be read. */
fun readBounded(stream: InputStream, maxBytes: Int): ByteArray {
    val out = ByteArrayOutputStream()
    val buffer = ByteArray(8 * 1024)
    while (true) {
        val n = stream.read(buffer)
        if (n < 0) break
        require(out.size() + n <= maxBytes) { "File is too large to be a talosconfig" }
        out.write(buffer, 0, n)
    }
    return out.toByteArray()
}
