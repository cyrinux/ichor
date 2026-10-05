package name.levis.ichor.ui.workloads

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.HorizontalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.PagedLoad
import name.levis.ichor.model.isKubeForbidden
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox

/**
 * A Kubernetes tab: the search field and scope picker on top whatever the list's [state], so
 * a scope that fails (no access to that namespace) or is missing can be changed (L6), then
 * the list ([content]) once loaded. [namespaces]: those of the loaded rows, offered while the
 * cluster's are unknown.
 */
@Composable
internal fun <T> KubeListFrame(
    control: KubeScopeControl,
    state: UiState<PagedLoad<T>>,
    namespaces: (List<T>) -> List<String>,
    query: String,
    onQuery: (String) -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
    @StringRes placeholder: Int = R.string.workloads_search,
    content: @Composable ColumnScope.(UiState.Loaded<PagedLoad<T>>) -> Unit,
) {
    Column(modifier.fillMaxSize()) {
        val load = (state as? UiState.Loaded)?.data
        val loaded = remember(load) { load?.items?.let(namespaces).orEmpty() }
        KubeFilters(control, loaded, query, onQuery, placeholder)
        HorizontalDivider()
        val rest = Modifier.weight(1f)
        when {
            !control.ready -> EmptyText(stringResource(R.string.kube_scope_type_prompt), rest)
            state is UiState.Loading -> LoadingBox(rest)
            state is UiState.Failed -> ErrorBox(scopeError(state.message, control.scope), onRetry, rest)
            state is UiState.Loaded -> content(state)
        }
    }
}

/** [message], or for a namespace the credentials may not read, a hint to pick another one. */
internal fun scopeError(message: UiText, scope: KubeScope): UiText {
    val namespace = scope.namespace ?: return message
    val text = (message as? UiText.Raw)?.text ?: return message
    return if (isKubeForbidden(text)) UiText.Res(R.string.kube_scope_no_access, namespace) else message
}
