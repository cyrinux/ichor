package name.levis.ichor.ui.health

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
import name.levis.ichor.ui.components.ExplainCardContent
import name.levis.ichor.ui.components.ExplainState

/**
 * The health check helper, under a failed run when AI is enabled: explains the failure in
 * a few lines from what the check said, and leads to the full diagnosis.
 */
@Composable
fun HealthExplainCard(
    explain: ExplainState,
    onExplain: (language: String) -> Unit,
    onStop: () -> Unit,
    onContinue: () -> Unit,
) {
    ExplainCardContent(
        title = stringResource(R.string.ai_health_explain_title),
        hint = { provider, onPhone ->
            if (onPhone) stringResource(R.string.ai_health_explain_hint_device) else stringResource(R.string.ai_health_explain_hint, provider)
        },
        explain = explain,
        onExplain = onExplain,
        onStop = onStop,
        onContinue = onContinue,
    )
}
