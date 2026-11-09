package name.levis.ichor.ui.workloads

import androidx.compose.foundation.clickable
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import name.levis.ichor.R
import name.levis.ichor.ui.nav.LocalOpenPod

/**
 * A pod named "namespace/name" (an image scan report's pods): split on the first "/"; null when
 * a side is empty or the name holds another "/" (no pod is named so), which stays plain text.
 */
fun podRef(value: String): Pair<String, String>? {
    val namespace = value.substringBefore('/', missingDelimiterValue = "")
    val name = value.substringAfter('/', missingDelimiterValue = "")
    return if (namespace.isEmpty() || name.isEmpty() || '/' in name) null else namespace to name
}

/**
 * A pod's name ([label], the name by default) that opens it (its logs and actions on the
 * Kubernetes screen) when tapped; plain text where nothing can open a pod ([LocalOpenPod] not
 * provided).
 */
@Composable
fun PodLinkText(
    namespace: String,
    name: String,
    modifier: Modifier = Modifier,
    style: TextStyle = MaterialTheme.typography.bodySmall,
    label: String = name,
) {
    val open = LocalOpenPod.current
    val tap = if (open == null) {
        Modifier
    } else {
        Modifier.clickable(onClickLabel = stringResource(R.string.pod_open), role = Role.Button) { open(namespace, name) }
    }
    Text(
        label,
        modifier = modifier.then(tap),
        style = style,
        fontFamily = FontFamily.Monospace,
        color = if (open == null) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.primary,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}
