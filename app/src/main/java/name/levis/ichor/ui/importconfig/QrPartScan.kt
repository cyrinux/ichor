package name.levis.ichor.ui.importconfig

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichorgo.Ichorgo
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/**
 * Shared between the camera analyzer thread and the UI: [stopped] once a config is
 * delivered, [last] the last text posted (so a code held in view is posted once).
 */
internal class ScanGate {
    val stopped = AtomicBoolean(false)
    val last = AtomicReference<String?>(null)

    /** Whether [text] differs from the last one posted; records it. */
    fun isNew(text: String): Boolean = last.getAndSet(text) != text
}

@get:StringRes
private val QrPartError.message: Int
    get() = when (this) {
        QrPartError.MISMATCH -> R.string.qr_parts_mismatch
        QrPartError.MISSING -> R.string.qr_parts_missing
        QrPartError.INVALID -> R.string.qr_parts_invalid
    }

/**
 * The camera scan: a one-code config goes straight to [onScanned]; the codes of a split one
 * (`ichor:<i>/<n>:<chunk>`) are collected in any order, with a "k of n" ring, and the joined
 * config goes to [onScanned] once every part is in. Chunks are credentials: never logged.
 */
@Composable
internal fun QrPartScan(onScanned: (String) -> Unit, modifier: Modifier = Modifier) {
    val gate = remember { ScanGate() }
    val collector = remember { QrPartCollector(Ichorgo::qrPartsJoin) }
    val haptics = LocalHapticFeedback.current
    val callback by rememberUpdatedState(onScanned)
    var parts by remember { mutableStateOf(QrPartSet.EMPTY) }
    var notice by remember { mutableStateOf<Int?>(null) }
    var failed by remember { mutableStateOf(false) }

    fun deliver(text: String) {
        gate.stopped.set(true)
        callback(text)
    }

    fun warn(@StringRes message: Int, blocking: Boolean) {
        notice = message
        failed = blocking
        haptics.performHapticFeedback(HapticFeedbackType.Reject)
    }

    fun startOver() {
        parts = QrPartSet.EMPTY
        notice = null
        failed = false
        gate.last.set(null)
    }

    fun onText(text: String) {
        if (gate.stopped.get() || failed) return
        val json = try {
            Ichorgo.qrPart(text)
        } catch (e: Exception) {
            warn(QrPartError.fromMessage(e.message).message, blocking = false)
            return
        }
        if (json.isEmpty()) {
            // A one-code config, as before; ignored while a split one is being collected.
            if (parts.isEmpty) deliver(text)
            return
        }
        val part = QrPartCollector.parse(json) ?: return warn(QrPartError.INVALID.message, blocking = false)
        when (val outcome = collector.add(parts, part)) {
            is QrPartOutcome.Progress -> if (outcome.isNew) {
                parts = outcome.set
                notice = null
                haptics.performHapticFeedback(HapticFeedbackType.Confirm)
            }
            is QrPartOutcome.Reset -> {
                parts = outcome.set
                warn(R.string.qr_parts_reset, blocking = false)
            }
            is QrPartOutcome.Complete -> {
                haptics.performHapticFeedback(HapticFeedbackType.Confirm)
                deliver(outcome.text)
            }
            is QrPartOutcome.Failed -> warn(outcome.error.message, blocking = true)
        }
    }

    Box(modifier.fillMaxSize()) {
        CameraQrPreview(onText = ::onText, gate = gate, modifier = Modifier.fillMaxSize())
        if (!parts.isEmpty || notice != null) {
            QrPartProgress(
                parts = parts,
                notice = notice,
                onStartOver = ::startOver,
                modifier = Modifier.align(Alignment.BottomCenter).padding(16.dp),
            )
        }
    }
}

/** The progress card over the camera: ring, "k of n", the missing parts, a message, start over. */
@Composable
private fun QrPartProgress(
    parts: QrPartSet,
    @StringRes notice: Int?,
    onStartOver: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.medium,
        color = MaterialTheme.colorScheme.surface.copy(alpha = 0.92f),
        tonalElevation = 2.dp,
    ) {
        Row(
            Modifier.padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (!parts.isEmpty) {
                CircularProgressIndicator(
                    progress = { parts.received.toFloat() / parts.total },
                    modifier = Modifier.size(40.dp),
                )
            }
            Column(Modifier.weight(1f).semantics { liveRegion = LiveRegionMode.Polite }) {
                if (!parts.isEmpty) {
                    Text(
                        stringResource(R.string.qr_parts_progress, parts.received, parts.total),
                        style = MaterialTheme.typography.titleSmall,
                    )
                    if (parts.missing.isNotEmpty()) {
                        Text(
                            stringResource(R.string.qr_parts_still_missing, parts.missing.joinToString(", ")),
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
                if (notice != null) {
                    Text(stringResource(notice), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                }
            }
            TextButton(onClick = onStartOver) { Text(stringResource(R.string.qr_parts_start_over)) }
        }
    }
}
