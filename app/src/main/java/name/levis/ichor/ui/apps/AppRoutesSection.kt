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
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.SectionTitle

/**
 * The URLs the app is served at, each opening in the browser. Nothing at all when it has none
 * (most apps are internal); a short note while loading or on failure.
 */
fun LazyListScope.appRoutesSection(state: UiState<List<KubeRoute>>) {
    val routes = (state as? UiState.Loaded)?.data
    if (routes?.isEmpty() == true) return
    item { SectionTitle(stringResource(R.string.apps_detail_routes)) }
    when (state) {
        UiState.Loading -> item { RouteNote(stringResource(R.string.apps_detail_routes_loading)) }
        is UiState.Failed -> item { RouteNote(stringResource(R.string.apps_detail_routes_failed, state.message.asString())) }
        is UiState.Loaded -> items(state.data, key = { it.url }) { RouteRow(it) }
    }
}

@Composable
private fun RouteNote(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** The URL, "Ingress · namespace/name", and an open icon; the whole row opens it. */
@Composable
private fun RouteRow(route: KubeRoute) {
    val uriHandler = LocalUriHandler.current
    Row(
        Modifier.fillMaxWidth().clickable { runCatching { uriHandler.openUri(route.url) } }.padding(vertical = 6.dp),
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
            Text(
                "${route.kind} · ${route.namespace}/${route.name}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Icon(
            Icons.AutoMirrored.Outlined.OpenInNew,
            contentDescription = stringResource(R.string.apps_detail_routes_open),
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(start = 12.dp).size(20.dp),
        )
    }
}
