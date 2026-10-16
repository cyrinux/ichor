package name.levis.ichor.ui.apps

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle

/**
 * The URLs the app is served at, each opening in the browser and its object summary behind the
 * info button ([onObject]). Nothing at all when it has none (most apps are internal); a short
 * note while loading or on failure.
 */
fun LazyListScope.appRoutesSection(state: UiState<List<KubeRoute>>, onObject: (KubeObjectRef) -> Unit) {
    val routes = (state as? UiState.Loaded)?.data
    if (routes?.isEmpty() == true) return
    item { SectionTitle(stringResource(R.string.apps_detail_routes)) }
    when (state) {
        UiState.Loading -> item { RouteNote(stringResource(R.string.apps_detail_routes_loading)) }
        is UiState.Failed -> item { RouteNote(stringResource(R.string.apps_detail_routes_failed, state.message.asString())) }
        is UiState.Loaded -> items(state.data, key = { it.url }) { RouteRow(it, onObject) }
    }
}

@Composable
private fun RouteNote(text: String) {
    MutedText(text)
}

/** The URL, "Ingress · namespace/name", and an open icon; the row opens it, the info button its summary. */
@Composable
private fun RouteRow(route: KubeRoute, onObject: (KubeObjectRef) -> Unit) {
    val uriHandler = LocalUriHandler.current
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = stringResource(R.string.apps_detail_routes_open)) {
            runCatching { uriHandler.openUri(route.url) }
        }.padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                route.label,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.primary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            MutedText(
                "${route.kind} · ${route.namespace}/${route.name}",
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            RouteCertBadge(route, Modifier.padding(top = 2.dp))
        }
        route.objectRef?.let { ref ->
            IconButton(
                onClick = { onObject(ref) },
                modifier = Modifier.size(40.dp),
            ) {
                Icon(
                    Icons.Outlined.Info,
                    contentDescription = stringResource(R.string.apps_detail_routes_object),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            }
        }
        Icon(
            Icons.AutoMirrored.Outlined.OpenInNew,
            contentDescription = stringResource(R.string.apps_detail_routes_open),
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(start = 4.dp).size(20.dp),
        )
    }
}
