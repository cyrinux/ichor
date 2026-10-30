package name.levis.ichor.ui.join

import androidx.compose.runtime.getValue
import name.levis.ichor.ui.components.ClusterScreenTitle
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
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
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.model.BootMedia
import name.levis.ichor.model.bootArchitectures
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.uiStateOf

/** What the active cluster's new machine boots from, for the architecture and Secure Boot chosen. */
class BootMediaViewModel(private val lifecycle: NodeLifecycleRepository) : ViewModel() {
    /** "" until the first answer: the template member's architecture. */
    private val _arch = MutableStateFlow("")
    val arch: StateFlow<String> = _arch.asStateFlow()
    private val _secureBoot = MutableStateFlow(false)
    val secureBoot: StateFlow<Boolean> = _secureBoot.asStateFlow()
    private val _media = MutableStateFlow<UiState<BootMedia>>(UiState.Loading)
    val media: StateFlow<UiState<BootMedia>> = _media.asStateFlow()
    private var loading: Job? = null

    init {
        load()
    }

    fun load() {
        loading?.cancel()
        _media.value = UiState.Loading
        loading = viewModelScope.launch {
            _media.value = uiStateOf { lifecycle.nodeBootMedia("", _arch.value, _secureBoot.value) }
            (_media.value as? UiState.Loaded)?.data?.let { _arch.value = it.arch }
        }
    }

    fun selectArch(arch: String) {
        if (arch == _arch.value) return
        _arch.value = arch
        load()
    }

    fun setSecureBoot(on: Boolean) {
        _secureBoot.value = on
        load()
    }
}

/**
 * "Boot a new machine…": the phone cannot serve a network boot (privileged ports), so it hands
 * over what does: the booter command for a laptop of the machine's network, the ISO, the iPXE
 * URL, and on Omni the join arguments. [onAddNode] continues with the machine in maintenance mode;
 * null on Omni, which adds the machine itself once it boots.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BootMediaScreen(
    onBack: () -> Unit,
    onAddNode: (() -> Unit)?,
    vm: BootMediaViewModel = viewModel(key = "boot-media", factory = factory { BootMediaViewModel(app.nodeLifecycleRepository) }),
) {
    val media by vm.media.collectAsStateWithLifecycle()
    val arch by vm.arch.collectAsStateWithLifecycle()
    val secureBoot by vm.secureBoot.collectAsStateWithLifecycle()
    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { ClusterScreenTitle(stringResource(R.string.boot_media_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            MutedText(stringResource(R.string.boot_media_intro))
            when (val state = media) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::load)
                is UiState.Loaded -> {
                    TargetCard(state.data, arch, secureBoot, vm::selectArch, vm::setSecureBoot)
                    MediaCards(state.data)
                }
            }
            if (onAddNode != null) {
                IchorButton(onClick = onAddNode, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.boot_media_machine_up)) }
            } else {
                InfoNotice(stringResource(R.string.boot_media_omni_next))
            }
        }
    }
}

/** The version and schematic the media boot, and the choices that change them. */
@Composable
private fun TargetCard(media: BootMedia, arch: String, secureBoot: Boolean, onArch: (String) -> Unit, onSecureBoot: (Boolean) -> Unit) {
    val source = if (media.fromMember) {
        stringResource(R.string.boot_media_schematic_member, media.templateNode)
    } else {
        stringResource(R.string.boot_media_schematic_default)
    }
    SectionCard(stringResource(R.string.boot_media_target, media.talosVersion), subtitle = source) {
        Text(media.schematic, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            bootArchitectures.forEach { a ->
                FilterChip(selected = a == arch, onClick = { onArch(a) }, label = { Text(a) })
            }
        }
        ToggleRow(stringResource(R.string.boot_media_secure_boot), secureBoot, onSecureBoot)
        media.warnings.forEach { InfoNotice(it) }
    }
}

@Composable
private fun MediaCards(media: BootMedia) {
    val booter = listOf(BootValue("docker", media.booterDocker), BootValue("podman", media.booterPodman))
    BootMediaCard(stringResource(R.string.boot_media_booter), booter, hint = stringResource(R.string.boot_media_booter_hint))
    BootMediaCard(stringResource(R.string.boot_media_iso), listOf(BootValue("", media.iso)))
    BootMediaCard(stringResource(R.string.boot_media_ipxe), listOf(BootValue("", media.ipxe)))
    BootMediaCard(
        stringResource(R.string.boot_media_pxe_files),
        listOf(BootValue("kernel", media.kernel), BootValue("initramfs", media.initramfs), BootValue("cmdline", media.cmdline)),
    )
    media.omni?.let { omni ->
        BootMediaCard(
            stringResource(R.string.boot_media_omni),
            listOf(BootValue("docker", omni.booterDocker), BootValue("podman", omni.booterPodman), BootValue("kernel args", omni.kernelArgs)),
            hint = stringResource(R.string.boot_media_omni_hint),
            sensitive = true,
        )
    }
}
