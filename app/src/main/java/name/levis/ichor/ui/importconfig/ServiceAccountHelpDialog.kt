package name.levis.ichor.ui.importconfig

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.TextAutoSize
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorAlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.DialogProperties
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichorgo.Ichorgo

/**
 * A least-privilege ServiceAccount for the phone, listed by the Go core (both apps show the
 * same): the [yaml] to apply with [apply], then [kubeconfig], the commands that turn its
 * token into a kubeconfig.
 */
@Serializable
data class ServiceAccountTemplate(val id: String, val yaml: String, val apply: String, val kubeconfig: String)

fun decodeServiceAccountTemplates(json: String): List<ServiceAccountTemplate> =
    TalosJson.decodeFromString(ListSerializer(ServiceAccountTemplate.serializer()), json)

/** Title and what a template grants; an unknown id (from a newer core) shows its id. */
internal enum class ServiceAccountRole(val id: String, @StringRes val label: Int, @StringRes val grants: Int) {
    READ("read", R.string.sa_help_template_read, R.string.sa_help_template_read_desc),
    OPERATE("operate", R.string.sa_help_template_operate, R.string.sa_help_template_operate_desc),
    NAMESPACE("namespace", R.string.sa_help_template_namespace, R.string.sa_help_template_namespace_desc),
    ;

    companion object {
        fun of(id: String): ServiceAccountRole? = entries.firstOrNull { it.id == id }
    }
}

/**
 * "How to create a service account": a dedicated ServiceAccount with a long-lived token
 * Secret and least-privilege RBAC, then a kubeconfig for it, for a cluster added with a token.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServiceAccountHelpDialog(onDismiss: () -> Unit) {
    // Fixed text from the core, no network.
    val templates = remember { runCatching { decodeServiceAccountTemplates(Ichorgo.serviceAccountTemplates()) }.getOrDefault(emptyList()) }
    var selected by rememberSaveable { mutableIntStateOf(0) }
    val template = templates.getOrNull(selected)

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
        properties = DialogProperties(usePlatformDefaultWidth = false),
        title = { Text(stringResource(R.string.sa_help_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.sa_help_intro), style = MaterialTheme.typography.bodyMedium)
                Text(
                    stringResource(R.string.sa_help_no_cluster_admin),
                    color = LocalStatusColors.current.warn,
                    style = MaterialTheme.typography.bodyMedium,
                )
                MutedText(stringResource(R.string.sa_help_token_expiry))
                TemplatePicker(templates, selected, onSelect = { selected = it })
                template?.let { TemplateSteps(it) }
            }
        },
        confirmButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.help_got_it)) } },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TemplatePicker(templates: List<ServiceAccountTemplate>, selected: Int, onSelect: (Int) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        templates.forEachIndexed { i, t ->
            SegmentedButton(
                selected = i == selected,
                onClick = { onSelect(i) },
                shape = SegmentedButtonDefaults.itemShape(i, templates.size),
                icon = {},
            ) {
                Text(
                    ServiceAccountRole.of(t.id)?.let { stringResource(it.label) } ?: t.id,
                    maxLines = 1,
                    softWrap = false,
                    autoSize = TextAutoSize.StepBased(minFontSize = 10.sp, maxFontSize = MaterialTheme.typography.labelLarge.fontSize),
                )
            }
        }
    }
}

@Composable
private fun TemplateSteps(template: ServiceAccountTemplate) {
    ServiceAccountRole.of(template.id)?.let { MutedText(stringResource(it.grants)) }
    Step(stringResource(R.string.sa_help_step_apply))
    Command(template.yaml, copyLabel = R.string.sa_help_copy_yaml)
    Command(template.apply)
    Step(stringResource(R.string.sa_help_step_kubeconfig))
    Command(template.kubeconfig)
    Step(stringResource(R.string.sa_help_step_import))
}
