package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.HelmRollbackHook
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

/** The chart's pre- and post-rollback hooks, in the order Helm runs them; nothing when none. */
@Composable
internal fun HelmRollbackHooksSection(hooks: List<HelmRollbackHook>) {
    if (hooks.isEmpty()) return
    SectionTitle(stringResource(R.string.helm_rollback_hooks))
    hooks.forEach { HookRow(it) }
}

@Composable
private fun HookRow(hook: HelmRollbackHook) {
    val colors = LocalStatusColors.current
    val weight = if (hook.weight != 0) " (" + stringResource(R.string.helm_rollback_hook_weight, hook.weight.toString()) + ")" else ""
    val what = "${hook.kind} ${hook.name}$weight"
    val line = stringResource(if (hook.isPre) R.string.helm_rollback_hook_before else R.string.helm_rollback_hook_after, what)
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(line, style = MaterialTheme.typography.bodySmall)
        if (hook.error.isNotEmpty()) Text(hook.error, style = MaterialTheme.typography.bodySmall, color = colors.bad)
    }
}
