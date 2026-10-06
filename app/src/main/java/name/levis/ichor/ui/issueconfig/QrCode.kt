package name.levis.ichor.ui.issueconfig

import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.WriterException
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.ui.components.SpinnerBox

/** Quiet zone in modules (the QR specification asks for 4). */
private const val QR_MARGIN = 4

/**
 * [text] as a QR code, one pixel per module (scaled without smoothing when drawn), in byte
 * mode at error correction level L to fit as much as possible. Null when it does not fit.
 */
fun qrBitmap(text: String): ImageBitmap? {
    val hints = buildMap<EncodeHintType, Any> {
        put(EncodeHintType.ERROR_CORRECTION, ErrorCorrectionLevel.L)
        put(EncodeHintType.MARGIN, QR_MARGIN)
        // ASCII needs no ECI header (ISO-8859-1 is the default); anything else is UTF-8.
        if (text.any { it.code > 127 }) put(EncodeHintType.CHARACTER_SET, "UTF-8")
    }
    val matrix = try {
        QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, 0, 0, hints)
    } catch (_: WriterException) {
        return null
    } catch (_: IllegalArgumentException) {
        return null
    }
    val width = matrix.width
    val height = matrix.height
    val pixels = IntArray(width * height) { i ->
        if (matrix.get(i % width, i / width)) android.graphics.Color.BLACK else android.graphics.Color.WHITE
    }
    return Bitmap.createBitmap(pixels, width, height, Bitmap.Config.ARGB_8888).asImageBitmap()
}

private sealed interface QrRender {
    data object Pending : QrRender
    /** [bitmap] null: too large for a QR code. */
    class Done(val bitmap: ImageBitmap?) : QrRender
}

/** Black on white whatever the theme, so any scanner reads it. [onTooLarge] when it cannot be encoded. */
@Composable
fun QrCode(text: String, contentDescription: String, onTooLarge: @Composable () -> Unit, modifier: Modifier = Modifier) {
    // Encoding a version-40 code takes a moment: keep it off the main thread.
    val render by produceState<QrRender>(QrRender.Pending, text) {
        value = QrRender.Done(withContext(Dispatchers.Default) { qrBitmap(text) })
    }
    when (val r = render) {
        QrRender.Pending -> SpinnerBox(modifier.fillMaxWidth().aspectRatio(1f))
        is QrRender.Done -> r.bitmap?.let { bitmap ->
            Image(
                bitmap = bitmap,
                contentDescription = contentDescription,
                filterQuality = FilterQuality.None,
                modifier = modifier.fillMaxWidth().aspectRatio(1f).background(Color.White),
            )
        } ?: onTooLarge()
    }
}
