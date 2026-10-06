package name.levis.ichor.util

import java.net.HttpURLConnection
import java.net.URL

/**
 * GETs [url] and returns its body: null when the answer is not a 200 ([onStatus] sees the
 * status first, to throw a message of its own) or holds more than [maxBytes]. Blocking.
 */
fun httpGetBounded(
    url: String,
    maxBytes: Int,
    timeoutMs: Int,
    userAgent: String,
    followRedirects: Boolean = true,
    useCaches: Boolean = true,
    onStatus: (Int) -> Unit = {},
): ByteArray? {
    val connection = (URL(url).openConnection() as HttpURLConnection).apply {
        connectTimeout = timeoutMs
        readTimeout = timeoutMs
        instanceFollowRedirects = followRedirects
        this.useCaches = useCaches
        setRequestProperty("User-Agent", userAgent)
    }
    try {
        val status = connection.responseCode
        if (status != HttpURLConnection.HTTP_OK) {
            onStatus(status)
            return null
        }
        return connection.inputStream.use { it.readAtMost(maxBytes + 1) }.takeIf { it.size <= maxBytes }
    } finally {
        connection.disconnect()
    }
}
