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
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.EditNote
import androidx.compose.material.icons.outlined.FileOpen
import androidx.compose.material.icons.outlined.QrCodeScanner
import name.levis.ichor.ui.design.IchorAssistChip
import androidx.compose.material3.AssistChipDefaults
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
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
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DiscoveryProvider
import name.levis.ichor.model.TalosForm
import name.levis.ichor.model.discoveryLogo
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.BundledLogo
import name.levis.ichor.ui.components.VersionFooter
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.readBounded

/** Where the add screen takes a config from: the drop zone, or the paste, QR and form screens it opens. */
internal enum class ImportSource { PICK, PASTE, QR, FORM }

@Composable
internal fun SourcePicker(
    source: ImportSource,
    error: String?,
    pasted: String,
    onPasted: (String) -> Unit,
    form: TalosForm,
    onForm: (TalosForm) -> Unit,
    onSubmitForm: (TalosForm) -> Unit,
    onSource: (ImportSource) -> Unit,
    onYaml: (String) -> Unit,
    onDemo: (kube: Boolean) -> Unit,
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
                ImportSource.FORM -> TalosFormSource(form, onForm, onSubmitForm)
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
    onDemo: (kube: Boolean) -> Unit,
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
        DropZone(
            onFile = pickFile,
            onPaste = { onSource(ImportSource.PASTE) },
            onQr = { onSource(ImportSource.QR) },
            onForm = { onSource(ImportSource.FORM) },
        )
        readError?.let { Text(it, color = LocalStatusColors.current.bad) }
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.kube_discover_entry), style = MaterialTheme.typography.titleSmall)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                DiscoveryProvider.entries.forEach { provider ->
                    IchorAssistChip(
                        onClick = { onDiscover(provider) },
                        label = { Text(stringResource(provider.label)) },
                        leadingIcon = { BundledLogo(discoveryLogo(provider.id), Modifier.size(AssistChipDefaults.IconSize)) },
                    )
                }
            }
        }
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.Top,
        ) {
            DemoButton(onDemo)
            restore?.invoke()
        }
    }
}

/** "Try demo", then which one: the Talos demo, or the Kubernetes one (an EKS-like kubeconfig cluster). */
@Composable
private fun DemoButton(onDemo: (kube: Boolean) -> Unit) {
    var choosing by rememberSaveable { mutableStateOf(false) }
    Box {
        IchorTextButton(onClick = { choosing = true }) { Text(stringResource(R.string.demo_try)) }
        DropdownMenu(expanded = choosing, onDismissRequest = { choosing = false }) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.demo_talos)) },
                onClick = { choosing = false; onDemo(false) },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.demo_kube)) },
                onClick = { choosing = false; onDemo(true) },
            )
        }
    }
}

/** The Ichor glyph over the two config kinds, and the three ways to bring one in. */
@Composable
private fun DropZone(onFile: () -> Unit, onPaste: () -> Unit, onQr: () -> Unit, onForm: () -> Unit) {
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
        SourceTileGrid(Modifier.fillMaxWidth()) {
            SourceTile(stringResource(R.string.import_tab_file), Icons.Outlined.FileOpen, onFile)
            SourceTile(stringResource(R.string.import_tab_paste), Icons.Outlined.ContentPaste, onPaste)
            SourceTile(stringResource(R.string.import_tab_qr), Icons.Outlined.QrCodeScanner, onQr)
            SourceTile(stringResource(R.string.import_form_tab), Icons.Outlined.EditNote, onForm)
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
private fun SourceTile(label: String, icon: ImageVector, onClick: () -> Unit) {
    Surface(
        onClick = onClick,
        shape = RoundedCornerShape(14.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHigh,
    ) {
        Column(
            Modifier.padding(vertical = 12.dp, horizontal = 4.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(4.dp, Alignment.CenterVertically),
        ) {
            Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
            Text(label, style = MaterialTheme.typography.labelLarge, textAlign = TextAlign.Center)
        }
    }
}

/** Four across from this width at font scale 1: below, a label would wrap or be cut. */
private const val FOUR_TILES_MIN_WIDTH_DP = 440f

/** Tiles per row: two on a phone, four when the width (in scaled text) leaves each one room. */
internal fun sourceTileColumns(widthDp: Float, fontScale: Float): Int =
    if (widthDp / fontScale >= FOUR_TILES_MIN_WIDTH_DP) 4 else 2

/** Lays [content] in rows ([sourceTileColumns]), every tile as wide and as tall as the tallest. */
@Composable
internal fun SourceTileGrid(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Layout(content, modifier) { tiles, constraints ->
        val gap = 8.dp.roundToPx()
        val columns = sourceTileColumns(constraints.maxWidth.toDp().value, fontScale)
        val width = (constraints.maxWidth - gap * (columns - 1)) / columns
        val height = tiles.maxOfOrNull { it.maxIntrinsicHeight(width) } ?: 0
        val placeables = tiles.map { it.measure(Constraints.fixed(width, height)) }
        val rows = (placeables.size + columns - 1) / columns
        layout(constraints.maxWidth, rows * height + (rows - 1).coerceAtLeast(0) * gap) {
            placeables.forEachIndexed { i, tile ->
                tile.place((i % columns) * (width + gap), (i / columns) * (height + gap))
            }
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
internal fun rememberConfigFilePicker(onYaml: (String) -> Unit, onError: (String) -> Unit): () -> Unit {
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
        IchorButton(onClick = { onYaml(text) }, enabled = text.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.import_validate))
        }
    }
}
