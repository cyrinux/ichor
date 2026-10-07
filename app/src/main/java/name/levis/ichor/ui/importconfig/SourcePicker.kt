package name.levis.ichor.ui.importconfig

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material.icons.outlined.QrCodeScanner
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DiscoveryProvider
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.VersionFooter
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.readBounded

/** Where the add screen takes a config from: the drop zone, or the paste and QR screens it opens. */
internal enum class ImportSource { PICK, PASTE, QR }

@Composable
internal fun SourcePicker(
    source: ImportSource,
    error: String?,
    pasted: String,
    onPasted: (String) -> Unit,
    onSource: (ImportSource) -> Unit,
    onYaml: (String) -> Unit,
    onDemo: () -> Unit,
    onDiscover: (DiscoveryProvider) -> Unit,
    restore: (@Composable () -> Unit)?,
) {
    Column(Modifier.fillMaxSize()) {
        if (error != null) {
            Text(
                error,
                color = LocalStatusColors.current.bad,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }
        Box(Modifier.weight(1f)) {
            when (source) {
                ImportSource.PICK -> Sources(onSource, onYaml, onDemo, onDiscover, restore)
                ImportSource.PASTE -> PasteSource(pasted, onPasted, onYaml)
                ImportSource.QR -> QrScanner(onScanned = onYaml)
            }
        }
        VersionFooter()
    }
}

/**
 * The first view of the add screen: one drop zone for either config (the core tells them
 * apart), the cloud accounts discovery reaches, then the demo and a backup restore.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Sources(
    onSource: (ImportSource) -> Unit,
    onYaml: (String) -> Unit,
    onDemo: () -> Unit,
    onDiscover: (DiscoveryProvider) -> Unit,
    restore: (@Composable () -> Unit)?,
) {
    var readError by rememberSaveable { mutableStateOf<String?>(null) }
    val pickFile = rememberConfigFilePicker(
        onYaml = { readError = null; onYaml(it) },
        onError = { readError = it },
    )
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        DropZone(onFile = pickFile, onPaste = { onSource(ImportSource.PASTE) }, onQr = { onSource(ImportSource.QR) })
        readError?.let { Text(it, color = LocalStatusColors.current.bad) }
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.kube_discover_entry), style = MaterialTheme.typography.titleSmall)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                DiscoveryProvider.entries.forEach { provider ->
                    AssistChip(
                        onClick = { onDiscover(provider) },
                        label = { Text(stringResource(provider.label)) },
                        leadingIcon = {
                            Icon(Icons.Outlined.Cloud, contentDescription = null, Modifier.size(AssistChipDefaults.IconSize))
                        },
                    )
                }
            }
        }
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.Top,
        ) {
            TextButton(onClick = onDemo) { Text(stringResource(R.string.demo_try)) }
            restore?.invoke()
        }
    }
}

/** The Ichor glyph over the two config kinds, and the three ways to bring one in. */
@Composable
private fun DropZone(onFile: () -> Unit, onPaste: () -> Unit, onQr: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().dashedBorder(MaterialTheme.colorScheme.outline, 20.dp).padding(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            painterResource(R.drawable.ic_stat_ichor),
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.size(64.dp),
        )
        Text(stringResource(R.string.import_bring_title), style = MaterialTheme.typography.titleMedium)
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            FormatPill("talosconfig", MaterialTheme.colorScheme.primaryContainer, MaterialTheme.colorScheme.onPrimaryContainer)
            FormatPill("kubeconfig", MaterialTheme.colorScheme.tertiaryContainer, MaterialTheme.colorScheme.onTertiaryContainer)
        }
        Text(
            stringResource(R.string.import_detects),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            SourceTile(stringResource(R.string.import_tab_file), Icons.Outlined.FileOpen, onFile, Modifier.weight(1f))
            SourceTile(stringResource(R.string.import_tab_paste), Icons.Outlined.ContentPaste, onPaste, Modifier.weight(1f))
            SourceTile(stringResource(R.string.import_tab_qr), Icons.Outlined.QrCodeScanner, onQr, Modifier.weight(1f))
        }
    }
}

@Composable
private fun FormatPill(text: String, container: Color, content: Color) {
    Surface(shape = RoundedCornerShape(8.dp), color = container, contentColor = content) {
        Text(
            text,
            style = MaterialTheme.typography.labelMedium,
            fontFamily = FontFamily.Monospace,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 2.dp),
        )
    }
}

@Composable
private fun SourceTile(label: String, icon: ImageVector, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Surface(
        onClick = onClick,
        shape = RoundedCornerShape(14.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
        modifier = modifier,
    ) {
        Column(
            Modifier.padding(vertical = 12.dp, horizontal = 4.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
            Text(label, style = MaterialTheme.typography.labelLarge, textAlign = TextAlign.Center)
        }
    }
}

/** A dashed rounded outline, the drop-zone look. */
private fun Modifier.dashedBorder(color: Color, radius: Dp): Modifier = drawBehind {
    val width = 1.5.dp.toPx()
    drawRoundRect(
        color = color,
        topLeft = Offset(width / 2, width / 2),
        size = Size(size.width - width, size.height - width),
        cornerRadius = CornerRadius(radius.toPx()),
        style = Stroke(width = width, pathEffect = PathEffect.dashPathEffect(floatArrayOf(8.dp.toPx(), 6.dp.toPx()))),
    )
}

/** Opens the document picker; the chosen file's text goes to [onYaml], a read failure to [onError]. */
@Composable
private fun rememberConfigFilePicker(onYaml: (String) -> Unit, onError: (String) -> Unit): () -> Unit {
    val context = LocalContext.current
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        runCatching {
            context.contentResolver.openInputStream(uri)?.use { stream ->
                readBounded(stream, MAX_CONFIG_BYTES, "File is too large to be a talosconfig").decodeToString()
            } ?: throw LocalizedException(UiText.Res(R.string.import_could_not_open))
        }.fold(
            onSuccess = onYaml,
            onFailure = {
                // readBounded rejects oversized files with IllegalArgumentException.
                onError(
                    if (it is IllegalArgumentException) {
                        context.getString(R.string.import_file_too_large)
                    } else {
                        it.uiText().resolve(context)
                    },
                )
            },
        )
    }
    return { picker.launch(arrayOf("*/*")) }
}

@Composable
private fun PasteSource(text: String, onText: (String) -> Unit, onYaml: (String) -> Unit) {
    Column(Modifier.fillMaxSize().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        OutlinedTextField(
            value = text,
            onValueChange = { if (it.length <= MAX_CONFIG_BYTES) onText(it) },
            label = { Text(stringResource(R.string.import_paste_label)) },
            textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth().weight(1f),
        )
        Button(onClick = { onYaml(text) }, enabled = text.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.import_validate))
        }
    }
}
