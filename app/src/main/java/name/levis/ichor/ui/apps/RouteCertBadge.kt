package name.levis.ichor.ui.apps

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.model.RouteCertIssue
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.color

/** What is wrong with the certificate the route serves; nothing when it is fine or unknown. */
@Composable
fun RouteCertBadge(route: KubeRoute, modifier: Modifier = Modifier) {
    val health = route.certHealth ?: return
    val text = when (route.certIssue) {
        RouteCertIssue.EXPIRED -> stringResource(R.string.route_cert_expired)
        RouteCertIssue.HOST_MISMATCH -> stringResource(R.string.route_cert_host_mismatch)
        else -> stringResource(R.string.route_cert_expiring, route.certDaysLeft(System.currentTimeMillis() / 1000).coerceAtLeast(0).toInt())
    }
    StatusPill(text, health.color(), modifier)
}
