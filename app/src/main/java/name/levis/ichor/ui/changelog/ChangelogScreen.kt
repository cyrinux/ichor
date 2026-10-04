package name.levis.ichor.ui.changelog

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.LocalViewModelStoreOwner
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.security.findFragmentActivity
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.data.ChangelogRepository
import name.levis.ichor.model.Changelog
import name.levis.ichor.model.ChangelogRelease
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.InfoBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory

class ChangelogViewModel(private val changelog: ChangelogRepository) : LoadingViewModel<Changelog>() {
    override suspend fun fetch() = changelog.bundled()
}

/** The full release history bundled with the app. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChangelogScreen(
    onBack: () -> Unit,
    vm: ChangelogViewModel = viewModel(factory = factory { ChangelogViewModel(app.changelogRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.changelog_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        val releases = (state as? UiState.Loaded)?.data?.releases
        when {
            state == UiState.Loading -> LoadingBox(Modifier.padding(padding))
            // Missing or corrupt asset: nothing to show, and nothing the user can do about it.
            releases.isNullOrEmpty() -> InfoBox(stringResource(R.string.changelog_empty), Modifier.padding(padding))
            else -> LazyColumn(
                Modifier.padding(padding).fillMaxSize(),
                contentPadding = PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                items(releases, key = { "${it.buildNumber}|${it.version}" }) { release ->
                    ReleaseNotes(release)
                    HorizontalDivider(Modifier.padding(top = 12.dp))
                }
            }
        }
    }
}

/** Decides once per launch whether "What's new" is due; survives rotation. */
class WhatsNewViewModel(private val changelog: ChangelogRepository) : ViewModel() {
    private val _releases = MutableStateFlow<List<ChangelogRelease>?>(null)
    val releases: StateFlow<List<ChangelogRelease>?> = _releases.asStateFlow()

    init {
        viewModelScope.launch { _releases.value = changelog.pendingWhatsNew() }
    }

    /** Closes the dialog for good: this build is recorded as seen. */
    fun dismiss() {
        changelog.markSeen()
        _releases.value = null
    }
}

/**
 * Shows "What's new" once after an update. Placed on the overview, so it comes after the
 * app lock and never over the import screen.
 */
@Composable
fun WhatsNewHost(
    onFullChangelog: () -> Unit,
    vm: WhatsNewViewModel = viewModel(
        // Activity-scoped: asked once per launch, not each time the overview is re-entered.
        viewModelStoreOwner = LocalContext.current.findFragmentActivity() ?: checkNotNull(LocalViewModelStoreOwner.current),
        factory = factory { WhatsNewViewModel(app.changelogRepository) },
    ),
) {
    val releases by vm.releases.collectAsStateWithLifecycle()
    releases?.let {
        WhatsNewDialog(
            versionName = BuildConfig.VERSION_NAME,
            releases = it,
            onFullChangelog = {
                vm.dismiss()
                onFullChangelog()
            },
            onDismiss = vm::dismiss,
        )
    }
}
