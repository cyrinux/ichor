package name.levis.talosmobile.ui.storage

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.Switch
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.talosmobile.R
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.model.DiskHealthReport
import name.levis.talosmobile.model.DiskUsage
import name.levis.talosmobile.model.FeatureSupport
import name.levis.talosmobile.model.Mount
import name.levis.talosmobile.model.TalosFeature
import name.levis.talosmobile.model.UsageLevel
import name.levis.talosmobile.model.Volume
import name.levis.talosmobile.model.VolumeList
import name.levis.talosmobile.model.level
import name.levis.talosmobile.model.normalizePath
import name.levis.talosmobile.model.notice
import name.levis.talosmobile.model.support
import name.levis.talosmobile.model.usedFraction
import name.levis.talosmobile.model.versionNotice
import name.levis.talosmobile.model.shortList
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.InfoNotice
import name.levis.talosmobile.ui.components.InfoRow
import name.levis.talosmobile.ui.components.InlineError
import name.levis.talosmobile.ui.components.Section
import name.levis.talosmobile.ui.components.SectionBody
import name.levis.talosmobile.ui.components.SectionTitle
import name.levis.talosmobile.ui.components.UsageBar
import name.levis.talosmobile.ui.components.rememberNodeFeatures
import name.levis.talosmobile.ui.components.sectionOf
import name.levis.talosmobile.ui.components.text
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import name.levis.talosmobile.util.formatBytes
import java.util.Locale

/** Depth asked to `talosctl usage`: the direct children of the opened directory. */
private const val DISK_USAGE_DEPTH = 1

data class StorageState(
    val mounts: Section<List<Mount>> = Section.Loading,
    val volumes: Section<VolumeList> = Section.Loading,
    val health: Section<DiskHealthReport> = Section.Loading,
    /** Directory open in the disk usage explorer; null until the user picks one. */
    val usagePath: String? = null,
    val usage: Section<DiskUsage> = Section.Loading,
    val refreshing: Boolean = false,
)

/** Mounts, volumes, disk health and the disk usage explorer of one node, each loaded on its own. */
class StorageViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow(StorageState())
    val state: StateFlow<StorageState> = _state.asStateFlow()
    private var job: Job? = null
    private var usageJob: Job? = null
    private var started = false

    fun start() {
        if (started) return
        started = true
        refresh()
    }

    fun refresh() {
        job?.cancel()
        _state.update { it.copy(refreshing = true) }
        job = viewModelScope.launch {
            val mounts = launch { sectionOf { talos.mounts(node).mounts }.let { r -> _state.update { it.copy(mounts = r) } } }
            val volumes = launch { sectionOf { talos.volumes(node) }.let { r -> _state.update { it.copy(volumes = r) } } }
            val health = launch { sectionOf { talos.diskHealth(node) }.let { r -> _state.update { it.copy(health = r) } } }
            listOf(mounts, volumes, health).forEach { it.join() }
            _state.update { it.copy(refreshing = false) }
        }
        _state.value.usagePath?.let(::open)
    }

    /** Stops measuring and closes the explorer (the walk goes on on the node until its limit). */
    fun cancelUsage() {
        usageJob?.cancel()
        _state.update { it.copy(usagePath = null, usage = Section.Loading) }
    }

    /** Opens [path] in the disk usage explorer. */
    fun open(path: String) {
        val target = normalizePath(path)
        usageJob?.cancel()
        _state.update { it.copy(usagePath = target, usage = Section.Loading) }
        usageJob = viewModelScope.launch {
            val result = sectionOf { talos.diskUsage(node, target, DISK_USAGE_DEPTH) }
            _state.update { it.copy(usage = result) }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StorageScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: StorageViewModel = viewModel(key = "storage-$node", factory = factory { StorageViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val features = rememberNodeFeatures(node)
    LaunchedEffect(Unit) { vm.start() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.storage_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
            )
        },
    ) { padding ->
        PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = vm::refresh, modifier = Modifier.padding(padding).fillMaxSize()) {
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                item(key = "mounts") {
                    SectionCard(stringResource(R.string.storage_section_mounts), features.support(TalosFeature.MOUNTS)) {
                        SectionBody(state.mounts) { MountsContent(it) }
                    }
                }
                item(key = "volumes") {
                    SectionCard(stringResource(R.string.storage_section_volumes), features.support(TalosFeature.VOLUMES)) {
                        SectionBody(state.volumes) { VolumesContent(it) }
                    }
                }
                item(key = "health") {
                    SectionCard(stringResource(R.string.storage_section_health), features.support(TalosFeature.DISK_HEALTH)) {
                        SectionBody(state.health) { DiskHealthContent(it) }
                    }
                }
                diskUsageSection(state.usagePath, state.usage, features.support(TalosFeature.DISK_USAGE), vm::open, vm::cancelUsage)
            }
        }
    }
}

/** A titled card; when the node's Talos lacks the feature it only says what it needs. */
@Composable
private fun SectionCard(title: String, support: FeatureSupport, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            SectionTitle(title)
            val notice = support.notice
            if (notice != null) InfoNotice(notice.text()) else content()
        }
    }
}

/** The reason an optional section is unsupported: "Needs Talos…" when it is about the version. */
@Composable
fun UnsupportedReason(reason: String) {
    val notice = versionNotice(reason)
    InfoNotice(
        when {
            notice != null -> notice.text()
            reason.isNotBlank() -> reason
            else -> stringResource(R.string.common_not_on_this_talos)
        },
    )
}

@Composable
private fun MountsContent(mounts: List<Mount>) {
    var showAll by rememberSaveable { mutableStateOf(false) }
    val short = remember(mounts) { mounts.shortList() }
    val shown = if (showAll) mounts else short
    if (shown.isEmpty()) InfoNotice(stringResource(R.string.storage_mounts_empty))
    val colors = LocalStatusColors.current
    shown.forEach { m ->
        Column {
            Row {
                Text(m.mountedOn, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                Text(
                    stringResource(R.string.storage_used_of, formatBytes(m.used), formatBytes(m.size), percent(m.usedPercent)),
                    style = MaterialTheme.typography.bodySmall,
                    color = when (m.level) {
                        UsageLevel.BAD -> colors.bad
                        UsageLevel.WARN -> colors.warn
                        UsageLevel.OK -> MaterialTheme.colorScheme.onSurface
                    },
                )
            }
            Text(
                m.filesystem,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            UsageBar(m.usedFraction, Modifier.padding(top = 4.dp), warnAt = 0.8f)
        }
    }
    // Everything the node reported, pseudo filesystems and per-pod mounts included.
    if (mounts.size > short.size) {
        Row(
            Modifier.fillMaxWidth().toggleable(value = showAll, role = Role.Switch, onValueChange = { showAll = it }),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                stringResource(R.string.storage_mounts_show_all, mounts.size),
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
            )
            Switch(checked = showAll, onCheckedChange = null)
        }
    }
}

private fun percent(value: Double) = String.format(Locale.ROOT, "%.0f %%", value)

@Composable
private fun VolumesContent(list: VolumeList) {
    when {
        !list.supported -> UnsupportedReason(list.reason)
        list.volumes.isEmpty() -> InfoNotice(stringResource(R.string.storage_volumes_empty))
        else -> list.volumes.forEach { VolumeRow(it) }
    }
}

@Composable
private fun VolumeRow(v: Volume) {
    Column {
        Row {
            Text(v.id, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            Text(v.phase, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        val facts = listOfNotNull(
            v.type.takeIf { it.isNotBlank() },
            v.filesystem.takeIf { it.isNotBlank() },
            v.size.takeIf { it > 0 }?.let(::formatBytes),
        )
        if (facts.isNotEmpty()) {
            Text(facts.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (v.location.isNotBlank()) InfoRow(stringResource(R.string.storage_volume_location), v.location, mono = true)
        if (v.mountedOn.isNotBlank()) InfoRow(stringResource(R.string.storage_volume_mounted), v.mountedOn, mono = true)
        if (v.encryption.isNotBlank()) InfoRow(stringResource(R.string.storage_volume_encryption), v.encryption)
        v.error?.takeIf { it.isNotBlank() }?.let { InlineError(it) }
    }
}
