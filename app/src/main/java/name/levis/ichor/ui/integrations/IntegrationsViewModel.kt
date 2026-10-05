package name.levis.ichor.ui.integrations

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichor.BuildConfig
import name.levis.ichor.data.REPO_URL_BASE
import name.levis.ichor.data.TalosJson
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.IntegrationFamily
import name.levis.ichor.model.IntegrationReport
import name.levis.ichor.model.picking
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichorgo.Ichorgo

/** The issue form a request opens, with nothing filled in for "something else". */
val INTEGRATION_FORM_URL = "$REPO_URL_BASE${BuildConfig.UPDATE_REPO}/issues/new?template=integration.yml"

/**
 * The operators this cluster runs that Ichor does not show, and the API groups the user picked
 * for a request (none at first: nothing is named unless chosen). Loaded on demand, never polled.
 */
class IntegrationsViewModel(private val talos: TalosRepository) : LoadingViewModel<IntegrationReport>() {
    private val _picked = MutableStateFlow<Set<String>>(emptySet())
    /** Names of the groups ticked, across families (a group belongs to one family). */
    val picked: StateFlow<Set<String>> = _picked.asStateFlow()

    private var source: Any? = null

    override suspend fun fetch() = talos.integrations()

    /** Loads once per [key] (context, config generation); [refresh] forces it. */
    fun load(key: Any) {
        if (key == source) return
        source = key
        _picked.value = emptySet()
        refresh(reset = true)
    }

    fun toggle(group: String) = _picked.update { if (group in it) it - group else it + group }

    /** The pre-filled GitHub issue for [family], with only the groups picked. */
    fun issueUrl(family: IntegrationFamily): String {
        val json = TalosJson.encodeToString(IntegrationFamily.serializer(), family.picking(picked.value))
        return Ichorgo.integrationIssueURL(BuildConfig.UPDATE_REPO, json, "Ichor ${BuildConfig.VERSION_NAME} (Android)")
    }

    /** The requests already open for [family], to 👍 rather than duplicate. */
    fun searchUrl(family: IntegrationFamily): String = Ichorgo.integrationSearchURL(BuildConfig.UPDATE_REPO, family.id)
}
