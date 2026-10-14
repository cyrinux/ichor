package name.levis.ichor.ui.policies

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KyvernoPolicy
import name.levis.ichor.model.POLICY_ACTION_AUDIT
import name.levis.ichor.model.POLICY_ACTION_ENFORCE
import name.levis.ichor.model.POLICY_ACTION_MIXED
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

/** Enforce stands out (it refuses requests), Audit is a warning, Mixed sits between. */
@Composable
fun ActionBadge(action: String, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val (label, color) = when (action) {
        POLICY_ACTION_ENFORCE -> stringResource(R.string.kpol_enforce) to colors.bad
        POLICY_ACTION_AUDIT -> stringResource(R.string.kpol_audit) to colors.warn
        POLICY_ACTION_MIXED -> stringResource(R.string.kpol_mixed) to MaterialTheme.colorScheme.tertiary
        else -> action to colors.muted
    }
    val description = stringResource(R.string.kpol_mode) + ": " + label
    TagBadge(label, color, modifier.semantics { contentDescription = description })
}

/** What the policy's mode does to a request that breaks it, in one line. */
@Composable
fun actionMeaning(action: String): String? = when (action) {
    POLICY_ACTION_ENFORCE -> stringResource(R.string.kpol_mode_enforce)
    POLICY_ACTION_AUDIT -> stringResource(R.string.kpol_mode_audit)
    POLICY_ACTION_MIXED -> stringResource(R.string.kpol_mode_mixed)
    else -> null
}

/** A small tinted count ("7 failing"), shown only when it is not zero. */
@Composable
private fun CountBadge(text: String, color: Color) {
    TagBadge(text, color, Modifier.semantics { contentDescription = text })
}

/** The failing, warning and passing counts, each only when it is not zero. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ResultCounts(fail: Int, warn: Int, pass: Int, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (fail > 0) CountBadge(stringResource(R.string.kpol_fail_count, fail.toString()), colors.bad)
        if (warn > 0) CountBadge(stringResource(R.string.kpol_warn_count, warn.toString()), colors.warn)
        if (pass > 0) CountBadge(stringResource(R.string.kpol_pass_count, pass.toString()), colors.ok)
    }
}

/** "Not ready" in the error colour, read out with its reason. */
@Composable
fun NotReadyBadge(policy: KyvernoPolicy) {
    val label = stringResource(R.string.kpol_not_ready)
    val description = listOf(label, policy.readyReason).filter { it.isNotEmpty() }.joinToString(": ")
    TagBadge(label, LocalStatusColors.current.bad, Modifier.semantics { contentDescription = description })
}

/** Namespace (or cluster-wide), category and severity, as the row's second line. */
@Composable
fun policyScopeText(policy: KyvernoPolicy): String =
    listOf(policy.namespace.ifEmpty { stringResource(R.string.netpol_cluster_wide) }, policy.category, policy.severity)
        .filter { it.isNotEmpty() }
        .joinToString("  ·  ")
