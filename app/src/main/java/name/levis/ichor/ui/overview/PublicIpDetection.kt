package name.levis.ichor.ui.overview

import android.widget.Toast
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ProcessLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.PublicIpReport
import name.levis.ichor.model.firstError
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.userMessage

/**
 * The public IPs found by asking each node from the internet (see PublicIpRepository) for
 * the cluster [fingerprint], and how to ask again: [detect] is null when it cannot be done
 * (not os:admin, or screenshot mode, which also hides what was found: not masked).
 */
class PublicIpDetection(val probed: PublicIpReport?, val running: Boolean, val detect: (() -> Unit)?)

@Composable
fun rememberPublicIpDetection(fingerprint: String?, canDetect: Boolean): PublicIpDetection {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val reports by app.publicIps.reports.collectAsStateWithLifecycle()
    val running by app.publicIps.running.collectAsStateWithLifecycle()
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    var confirming by remember { mutableStateOf(false) }
    // Screenshot mode turned on with the dialog open: closed, not just hidden until it is off.
    LaunchedEffect(mask.enabled) { if (mask.enabled) confirming = false }
    if (fingerprint.isNullOrBlank() || mask.enabled) return PublicIpDetection(null, false, null)

    if (confirming) {
        DetectPublicIpsDialog(
            onRun = {
                confirming = false
                // The app's scope: pulling the image can take minutes, the user may leave meanwhile.
                ProcessLifecycleOwner.get().lifecycleScope.launch {
                    try {
                        val error = app.publicIps.detect().firstError()
                        if (error.isNotEmpty()) Toast.makeText(app, error, Toast.LENGTH_LONG).show()
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        Toast.makeText(app, e.userMessage(), Toast.LENGTH_LONG).show()
                    }
                }
            },
            onDismiss = { confirming = false },
        )
    }
    return PublicIpDetection(
        probed = reports[fingerprint],
        running = fingerprint in running,
        detect = { confirming = true }.takeIf { canDetect },
    )
}

/** Says what finding the public IPs does before it runs anything on the cluster. */
@Composable
private fun DetectPublicIpsDialog(onRun: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.overview_public_ip_detect_title),
        text = stringResource(R.string.overview_public_ip_detect_body),
        confirm = stringResource(R.string.overview_public_ip_detect_run),
        onConfirm = onRun,
        onDismiss = onDismiss,
    )
}

/** In the Nodes card's title: find the public IPs Talos does not know, or the run going on. */
@Composable
fun DetectPublicIpsButton(detection: PublicIpDetection) {
    when {
        detection.running -> {
            val label = stringResource(R.string.overview_public_ip_detecting)
            CircularProgressIndicator(Modifier.size(16.dp).semantics { contentDescription = label }, strokeWidth = 2.dp)
        }
        detection.detect != null -> TextButton(onClick = detection.detect) {
            Text(stringResource(R.string.overview_public_ip_detect))
        }
    }
}
