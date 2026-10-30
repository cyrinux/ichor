package name.levis.ichor.ui.settings

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.AiSettings
import name.levis.ichor.localmodel.DownloadBlock
import name.levis.ichor.localmodel.DownloadStatus
import name.levis.ichor.localmodel.EngineStatus
import name.levis.ichor.localmodel.IMPORTED_PREFIX
import name.levis.ichor.localmodel.LlamaNative
import name.levis.ichor.localmodel.LocalModelInfo
import name.levis.ichor.localmodel.ModelDownloads
import name.levis.ichor.localmodel.currentDownloadBlock
import name.levis.ichor.localmodel.downloadSupported
import name.levis.ichor.localmodel.formatModelSize
import name.levis.ichor.localmodel.partialBytes
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/**
 * Settings for the "This phone" provider: which model, its download (only on the user's tap,
 * on Wi-Fi unless they allow mobile data for this one download, never with Data Saver on or
 * on roaming), deleting it, or importing a model file instead.
 */
@Composable
fun LocalModelSection(app: TalosApp, settings: AiSettings) {
    val status = remember { LlamaNative.status }
    if (status != EngineStatus.AVAILABLE) {
        MutedText(stringResource(engineStatusText(status)))
        return
    }
    val store = app.localModels
    val prefs = app.aiPreferences
    val changes by store.changes.collectAsStateWithLifecycle()
    val catalog = store.catalog
    val imported = remember(changes) { store.imported() }
    val selected = settings.model.ifEmpty { catalog.firstOrNull { it.recommended }?.id ?: catalog.firstOrNull()?.id.orEmpty() }

    Column(Modifier.selectableGroup(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(stringResource(R.string.ai_model), style = MaterialTheme.typography.labelLarge)
        if (catalog.isEmpty()) MutedText(stringResource(R.string.ai_local_no_catalog))
        catalog.forEach { model ->
            val fits = store.fits(model)
            ModelRow(
                title = model.name,
                detail = stringResource(R.string.ai_local_model_detail, formatModelSize(model.bytes), model.license) +
                    if (fits) "" else "\n" + stringResource(R.string.ai_local_needs_memory, formatModelSize(model.minMemoryMiB * 1024 * 1024)),
                selected = model.id == selected,
                enabled = fits,
                onSelect = { prefs.update(settings.withModel(model.id)) },
            )
        }
        imported.forEach { id ->
            ModelRow(
                title = id.removePrefix(IMPORTED_PREFIX),
                detail = stringResource(R.string.ai_local_imported),
                selected = id == selected,
                enabled = true,
                onSelect = { prefs.update(settings.withModel(id)) },
            )
        }
    }

    store.info(selected)?.let { model -> DownloadState(app, model, changes) }
    if (selected.startsWith(IMPORTED_PREFIX)) DeleteModel(app, selected, settings)
    ImportModel(app, settings)
    MutedText(stringResource(R.string.ai_local_info))
}

@Composable
private fun ModelRow(title: String, detail: String, selected: Boolean, enabled: Boolean, onSelect: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().selectable(selected = selected, enabled = enabled, role = Role.RadioButton, onClick = onSelect),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = null, enabled = enabled)
        Column(Modifier.padding(start = 12.dp)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            MutedText(detail)
        }
    }
}

/**
 * The selected catalog model: on the phone, arriving, waiting for a network, or a Download
 * button (Android 14 and later: the download is a user-initiated data transfer job).
 */
@Composable
private fun DownloadState(app: TalosApp, model: LocalModelInfo, changes: Int) {
    val context = LocalContext.current
    val store = app.localModels
    val uri = LocalUriHandler.current
    val status by ModelDownloads.status.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { ModelDownloads.refresh(context) }
    // Not remembered across downloads: each one over mobile data is the user's explicit choice.
    var allowMetered by remember(model.id) { mutableStateOf(false) }
    val ready = remember(changes, status) { store.isReady(model.id) }
    val current = status

    when {
        ready -> {
            MutedText(stringResource(R.string.ai_local_ready, formatModelSize(model.bytes)))
            DeleteModel(app, model.id, null)
        }
        !downloadSupported() -> MutedText(stringResource(R.string.ai_local_download_needs_android_14))
        current is DownloadStatus.Running && current.id == model.id -> {
            val done = current.done
            val fraction = if (model.bytes > 0) (done.toFloat() / model.bytes).coerceIn(0f, 1f) else 0f
            LinearProgressIndicator(progress = { fraction }, modifier = Modifier.fillMaxWidth())
            MutedText(
                stringResource(R.string.ai_local_downloading, (fraction * 100).toInt(), formatModelSize(done), formatModelSize(model.bytes)),
            )
            IchorTextButton(onClick = { ModelDownloads.cancel(context, store, model.id) }) { Text(stringResource(R.string.common_cancel)) }
        }
        current is DownloadStatus.Waiting && current.id == model.id -> {
            WaitingLine(current.allowMetered)
            IchorTextButton(onClick = { ModelDownloads.cancel(context, store, model.id) }) { Text(stringResource(R.string.common_cancel)) }
        }
        else -> {
            if (current is DownloadStatus.Failed && current.id == model.id) {
                Text(
                    stringResource(downloadErrorText(current.error)),
                    color = LocalStatusColors.current.bad,
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.ai_local_allow_metered), style = MaterialTheme.typography.bodyMedium)
                    MutedText(stringResource(R.string.ai_local_allow_metered_desc))
                }
                Switch(checked = allowMetered, onCheckedChange = { allowMetered = it }, modifier = Modifier.padding(start = 12.dp))
            }
            IchorButton(
                onClick = { ModelDownloads.start(context, store, model.id, allowMetered) },
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.ai_local_download, formatModelSize(model.bytes - partialBytes(store, model.id)))) }
            IchorTextButton(onClick = { uri.openUri(model.licenseUrl) }) { Text(stringResource(R.string.ai_local_license, model.license)) }
        }
    }
}

/**
 * Why a queued download is not running, read again every few seconds (the network changes),
 * along with the job itself (the system's Task Manager can stop it without telling the app).
 */
@Composable
private fun WaitingLine(allowMetered: Boolean) {
    val context = LocalContext.current
    var tick by remember { mutableIntStateOf(0) }
    LaunchedEffect(Unit) {
        while (true) {
            delay(3_000)
            ModelDownloads.refresh(context)
            tick++
        }
    }
    val block = remember(tick) { currentDownloadBlock(context, allowMetered) }
    MutedText(
        stringResource(
            when (block) {
                DownloadBlock.METERED -> R.string.ai_local_wait_wifi
                DownloadBlock.DATA_SAVER -> R.string.ai_local_wait_data_saver
                DownloadBlock.ROAMING -> R.string.ai_local_wait_roaming
                DownloadBlock.NO_NETWORK -> R.string.ai_local_wait_network
                DownloadBlock.NONE -> R.string.ai_local_wait
            },
        ),
    )
}

@Composable
private fun DeleteModel(app: TalosApp, id: String, settings: AiSettings?) {
    var confirm by remember { mutableStateOf(false) }
    IchorTextButton(onClick = { confirm = true }) { Text(stringResource(R.string.ai_local_delete), color = LocalStatusColors.current.bad) }
    if (confirm) {
        ConfirmDialog(
            title = stringResource(R.string.ai_local_delete),
            text = stringResource(R.string.ai_local_delete_text),
            confirm = stringResource(R.string.ai_local_delete),
            destructive = true,
            onConfirm = {
                confirm = false
                app.localModelHost.unload()
                app.localModels.delete(id)
                // An imported file is gone for good: back to the catalog's choice.
                settings?.let { app.aiPreferences.update(it.withModel("")) }
            },
            onDismiss = { confirm = false },
        )
    }
}

@Composable
private fun ImportModel(app: TalosApp, settings: AiSettings) {
    val scope = rememberCoroutineScope()
    var importing by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        importing = true
        error = null
        scope.launch {
            runCatching { app.localModels.importFile(uri) }
                .onSuccess { app.aiPreferences.update(settings.withModel(it)) }
                .onFailure { error = it.userMessage() }
            importing = false
        }
    }
    IchorOutlinedButton(enabled = !importing, onClick = { pick.launch(arrayOf("*/*")) }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(if (importing) R.string.ai_local_importing else R.string.ai_local_import))
    }
    error?.let { Text(it, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall) }
}

private fun engineStatusText(status: EngineStatus) = when (status) {
    EngineStatus.NEEDS_64_BIT -> R.string.ai_local_needs_64bit
    EngineStatus.OLD_CPU -> R.string.ai_local_old_cpu
    else -> R.string.ai_local_unavailable
}

private fun downloadErrorText(error: String?) = when (error) {
    ModelDownloads.ERROR_CHECKSUM -> R.string.ai_local_error_checksum
    ModelDownloads.ERROR_SPACE -> R.string.ai_local_error_space
    else -> R.string.ai_local_error
}
