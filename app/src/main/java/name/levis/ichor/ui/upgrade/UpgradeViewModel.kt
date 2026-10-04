package name.levis.ichor.ui.upgrade

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.data.UpgradeManager
import name.levis.ichor.model.TalosRelease
import name.levis.ichor.model.UpgradePlan
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText

class UpgradePlanViewModel(private val upgrades: UpgradeManager, private val node: String) : LoadingViewModel<UpgradePlan>() {
    override suspend fun fetch() = upgrades.plan(node)
}

/**
 * The installer image for the typed version, or why it cannot be built, and the [risk] of
 * going to that version (to acknowledge before starting; "" when fine).
 */
data class TargetImage(
    val version: String = "",
    val image: String = "",
    val error: UiText? = null,
    val pending: Boolean = false,
    val risk: String = "",
)

/** Debounce of the image computation while typing a version. */
private const val IMAGE_CHECK_MS = 300L

class UpgradeTargetViewModel(private val upgrades: UpgradeManager) : ViewModel() {
    private val _releases = MutableStateFlow<UiState<List<TalosRelease>>>(UiState.Loading)
    val releases: StateFlow<UiState<List<TalosRelease>>> = _releases.asStateFlow()

    private val _target = MutableStateFlow(TargetImage())
    val target: StateFlow<TargetImage> = _target.asStateFlow()
    private var imageJob: Job? = null

    init {
        viewModelScope.launch {
            _releases.value = uiStateOf { upgrades.releases() }
        }
    }

    /**
     * Recomputes the target image for [version] from [currentImage] (same registry and
     * schematic) and the risk of going there from [currentVersion].
     */
    fun setVersion(currentImage: String, currentVersion: String, version: String) {
        imageJob?.cancel()
        val v = version.trim()
        if (v.isEmpty()) {
            _target.value = TargetImage()
            return
        }
        _target.value = TargetImage(v, pending = true)
        imageJob = viewModelScope.launch {
            delay(IMAGE_CHECK_MS)
            _target.value = try {
                val image = withContext(Dispatchers.IO) { upgrades.image(currentImage, v) }
                // The core returns "" for anything that is not a Talos version.
                if (image.isEmpty()) {
                    TargetImage(v, error = UiText.Res(R.string.upgrade_bad_version))
                } else {
                    TargetImage(v, image = image, risk = upgrades.versionRisk(currentVersion, v))
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                TargetImage(v, error = e.uiText())
            }
        }
    }
}
