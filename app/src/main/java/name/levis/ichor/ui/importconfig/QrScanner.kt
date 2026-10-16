package name.levis.ichor.ui.importconfig

import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import android.util.Size
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.google.zxing.LuminanceSource
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Camera preview that reports the first QR code payload once.
 * Generate one on the desktop with: qrencode -r ~/.talos/config -o talosconfig.png, or for a
 * config too large for one code, raw gzip (gzip -9 < config | qrencode -8) or the compressed
 * text form (see HelpDialog: ichor-config:).
 */
@Composable
fun QrScanner(onScanned: (String) -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    var granted by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED,
        )
    }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted = it }

    LaunchedEffect(Unit) {
        if (!granted) launcher.launch(Manifest.permission.CAMERA)
    }

    if (granted) {
        CameraQrPreview(onScanned, modifier)
    } else {
        Column(
            modifier.fillMaxSize().padding(24.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(stringResource(R.string.import_camera_needed))
            Button(onClick = { launcher.launch(Manifest.permission.CAMERA) }, Modifier.padding(top = 12.dp)) {
                Text(stringResource(R.string.import_grant_camera))
            }
        }
    }
}

// A whole talosconfig is a dense version-40 code (177 modules a side): the default 640x480
// analysis frame leaves under 3 pixels per module, so ask for 1080p.
private val ANALYSIS_RESOLUTION = ResolutionSelector.Builder()
    .setResolutionStrategy(
        ResolutionStrategy(Size(1920, 1080), ResolutionStrategy.FALLBACK_RULE_CLOSEST_LOWER_THEN_HIGHER),
    )
    .build()

@Composable
private fun CameraQrPreview(onScanned: (String) -> Unit, modifier: Modifier) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val callback by rememberUpdatedState(onScanned)
    val delivered = remember { AtomicBoolean(false) }
    val executor = remember { Executors.newSingleThreadExecutor() }
    val reader = remember { QrPayload.newReader() }
    val previewView = remember { PreviewView(context) }

    DisposableEffect(lifecycleOwner) {
        val providerFuture = ProcessCameraProvider.getInstance(context)
        var disposed = false
        providerFuture.addListener({
            if (disposed) return@addListener
            val provider = providerFuture.get()
            val preview = Preview.Builder().build().also { it.setSurfaceProvider(previewView.surfaceProvider) }
            val analysis = ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .setResolutionSelector(ANALYSIS_RESOLUTION)
                .build()
            analysis.setAnalyzer(executor) { proxy ->
                val text = proxy.use {
                    if (delivered.get()) null else QrPayload.decode(reader, it.luminance())?.let(QrPayload::text)
                }
                if (!text.isNullOrEmpty() && delivered.compareAndSet(false, true)) {
                    ContextCompat.getMainExecutor(context).execute { callback(text) }
                }
            }
            provider.unbindAll()
            provider.bindToLifecycle(lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis)
        }, ContextCompat.getMainExecutor(context))

        onDispose {
            disposed = true
            if (providerFuture.isDone) runCatching { providerFuture.get().unbindAll() }
            executor.shutdown()
        }
    }

    AndroidView(factory = { previewView }, modifier = modifier.fillMaxSize())
}

/**
 * The frame's Y plane as ZXing luminance, in sensor orientation: the QR detector finds the
 * finder patterns at any angle, so the frame is not rotated (rotationDegrees is not needed,
 * and rotating a 1080p plane per frame would cost more than the decode). The row stride is
 * kept as the data width, so the padded rows are cropped rather than copied.
 */
private fun ImageProxy.luminance(): LuminanceSource {
    val plane = planes[0]
    val buffer = plane.buffer.duplicate().apply { rewind() }
    val y = ByteArray(buffer.remaining()).also(buffer::get)
    return QrPayload.lumaSource(y, width, height, plane.rowStride)
}
