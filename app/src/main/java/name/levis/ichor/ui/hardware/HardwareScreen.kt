package name.levis.ichor.ui.hardware

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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.hardwareKey
import name.levis.ichor.model.DiskInfo
import name.levis.ichor.model.ExtensionInfo
import name.levis.ichor.model.HardwareSection
import name.levis.ichor.model.MemoryModule
import name.levis.ichor.model.NodeHardware
import name.levis.ichor.model.ProcessorInfo
import name.levis.ichor.model.SecurityInfo
import name.levis.ichor.model.SystemInfo
import name.levis.ichor.model.sortedDisks
import name.levis.ichor.model.totalMemoryBytes
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

class HardwareViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<NodeHardware>() {
    override val keepsDataOnFailure = true
    override fun cached(): TalosRepository.Timed<NodeHardware>? = talos.cached(hardwareKey(node))
    override val restores get() = talos.restores
    override suspend fun fetch() = talos.hardware(node)
}

/** "About this node": SMBIOS system, CPUs, memory, disks, extensions and security state. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HardwareScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: HardwareViewModel = viewModel(key = "hardware-$node", factory = factory { HardwareViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        bottomBar = { DataFreshness(state) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.hardware_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.padding(padding))
            is UiState.Failed -> ErrorBox(s.message, vm::refresh, Modifier.padding(padding))
            is UiState.Loaded -> PullToRefreshBox(
                isRefreshing = s.refreshing,
                onRefresh = vm::refresh,
                modifier = Modifier.padding(padding).fillMaxSize(),
            ) {
                HardwareContent(s.data)
            }
        }
    }
}

@Composable
private fun HardwareContent(hw: NodeHardware) {
    LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxSize()) {
        item {
            Section(stringResource(R.string.hardware_system), hw.errors[HardwareSection.SYSTEM], hw.system == null) {
                hw.system?.let { SystemRows(it) }
            }
        }
        item {
            Section(stringResource(R.string.hardware_processors), hw.errors[HardwareSection.PROCESSORS], hw.processors.isEmpty()) {
                hw.processors.forEachIndexed { i, cpu ->
                    if (i > 0) HorizontalDivider()
                    ProcessorRow(cpu)
                }
            }
        }
        item {
            Section(stringResource(R.string.hardware_memory), hw.errors[HardwareSection.MEMORY], hw.memory.isEmpty()) {
                InfoRow(stringResource(R.string.hardware_memory_total), formatBytes(hw.totalMemoryBytes))
                hw.memory.forEach { MemoryRow(it) }
            }
        }
        item {
            Section(stringResource(R.string.hardware_disks), hw.errors[HardwareSection.DISKS], hw.disks.isEmpty()) {
                hw.sortedDisks.forEachIndexed { i, disk ->
                    if (i > 0) HorizontalDivider()
                    DiskRow(disk)
                }
            }
        }
        item {
            Section(stringResource(R.string.hardware_extensions), hw.errors[HardwareSection.EXTENSIONS], hw.extensions.isEmpty()) {
                hw.extensions.forEach { ext -> ExtensionRow(ext) }
            }
        }
        item {
            Section(stringResource(R.string.hardware_security), hw.errors[HardwareSection.SECURITY], hw.security == null) {
                hw.security?.let { SecurityRows(it) }
            }
        }
    }
}

/** A card with a title, the section's error if it failed, "unavailable" when empty, else [content]. */
@Composable
private fun Section(title: String, error: String?, empty: Boolean, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            SectionTitle(title)
            when {
                error != null -> Text(error, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                empty -> Muted(stringResource(R.string.hardware_none))
                else -> content()
            }
        }
    }
}

@Composable
private fun Muted(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** Versions longer than this (e.g. the schematic's 64-char hash) get their own line instead of squeezing the name. */
private const val INLINE_VERSION_MAX = 16

@Composable
private fun ExtensionRow(ext: ExtensionInfo) {
    val inlineVersion = ext.version.length <= INLINE_VERSION_MAX
    Column(Modifier.padding(vertical = 2.dp)) {
        Row {
            Text(ext.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold, modifier = Modifier.weight(1f))
            if (inlineVersion) {
                Text(ext.version, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.padding(start = 8.dp))
            }
        }
        if (!inlineVersion) {
            Text(ext.version, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (ext.description.isNotBlank()) Muted(ext.description)
    }
}

/** An [InfoRow] skipped when [value] is blank (SMBIOS fields are often empty). */
@Composable
private fun OptionalRow(label: String, value: String, mono: Boolean = false) {
    if (value.isNotBlank()) InfoRow(label, value, mono)
}

@Composable
private fun SystemRows(s: SystemInfo) {
    OptionalRow(stringResource(R.string.hardware_manufacturer), s.manufacturer)
    OptionalRow(stringResource(R.string.hardware_product), s.product)
    OptionalRow(stringResource(R.string.hardware_version), s.version)
    OptionalRow(stringResource(R.string.hardware_serial), s.serial, mono = true)
    OptionalRow(stringResource(R.string.hardware_sku), s.sku)
    OptionalRow("UUID", s.uuid, mono = true)
    OptionalRow(stringResource(R.string.hardware_bios), s.biosVersion)
}

@Composable
private fun ProcessorRow(cpu: ProcessorInfo) {
    Column(Modifier.padding(vertical = 4.dp)) {
        Text(
            listOf(cpu.manufacturer, cpu.model).filter { it.isNotBlank() }.distinct().joinToString(" ").ifEmpty { cpu.socket },
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Bold,
        )
        val speed = if (cpu.maxSpeedMhz > 0) stringResource(R.string.hardware_mhz, cpu.maxSpeedMhz) else null
        Muted(
            listOfNotNull(
                cpu.socket.ifBlank { null },
                stringResource(R.string.hardware_cores_threads, cpu.cores, cpu.threads),
                speed,
            ).joinToString("  ·  "),
        )
    }
}

@Composable
private fun MemoryRow(m: MemoryModule) {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Column(Modifier.weight(1f)) {
            Text(m.slot.ifBlank { m.bank }, style = MaterialTheme.typography.bodyMedium)
            val speed = if (m.speed > 0) stringResource(R.string.hardware_mts, m.speed) else null
            Muted(listOfNotNull(m.manufacturer.ifBlank { null }, m.type.ifBlank { null }, speed).joinToString("  ·  "))
        }
        Text(formatBytes(m.sizeMib * 1024 * 1024), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
    }
}

@Composable
private fun DiskRow(d: DiskInfo) {
    val colors = LocalStatusColors.current
    Column(Modifier.padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(d.devPath.ifEmpty { d.name }, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, fontWeight = FontWeight.Bold)
            if (d.systemDisk) StatusPill(stringResource(R.string.hardware_system_disk), MaterialTheme.colorScheme.primary, Modifier.padding(start = 8.dp))
            if (d.readonly) StatusPill(stringResource(R.string.hardware_readonly), colors.warn, Modifier.padding(start = 8.dp))
            Text(
                formatBytes(d.size),
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.weight(1f).padding(start = 8.dp),
                textAlign = TextAlign.End,
            )
        }
        Muted(listOf(d.type.uppercase(), d.model).filter { it.isNotBlank() }.joinToString("  ·  "))
        if (d.serial.isNotBlank()) Text(d.serial, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun SecurityRows(s: SecurityInfo) {
    YesNoRow(stringResource(R.string.hardware_secure_boot), s.secureBoot)
    YesNoRow(stringResource(R.string.hardware_uki), s.bootedWithUki)
    YesNoRow(stringResource(R.string.hardware_module_signatures), s.moduleSignatureEnforced)
    OptionalRow("SELinux", s.selinuxState)
    OptionalRow("FIPS", s.fipsState)
    OptionalRow(stringResource(R.string.hardware_uki_key), s.ukiSigningKeyFingerprint, mono = true)
    OptionalRow(stringResource(R.string.hardware_pcr_key), s.pcrSigningKeyFingerprint, mono = true)
}

@Composable
private fun YesNoRow(label: String, on: Boolean) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
        StatusPill(stringResource(if (on) R.string.hardware_enabled else R.string.hardware_disabled), if (on) colors.ok else colors.muted)
    }
}
