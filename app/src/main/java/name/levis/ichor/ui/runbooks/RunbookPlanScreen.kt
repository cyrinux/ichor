package name.levis.ichor.ui.runbooks

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.RunbookValueMemory
import name.levis.ichor.model.RunbookParam
import name.levis.ichor.model.RunbookPlan
import name.levis.ichor.model.initialRunbookValues
import name.levis.ichor.model.runbookValuesLabel
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.userMessage

/**
 * The plan of the runbook [id] on the cluster on screen, after the values it asks for: each step's target, whether it is
 * destructive, the role it needs and whether the cluster allows it. Run stays off while a
 * refused step would stop the run, until the user lets the run go past it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RunbookPlanScreen(id: String, onBack: () -> Unit, onRunning: () -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val scope = rememberRunbookScope()
    var yamlText by remember(id) { mutableStateOf<String?>(null) }
    var params by remember(id) { mutableStateOf(emptyList<RunbookParam>()) }
    var values by remember(id) { mutableStateOf(emptyMap<String, String>()) }
    var choices by remember(id) { mutableStateOf(emptyMap<String, List<String>>()) }
    var plan by remember(id) { mutableStateOf<RunbookPlan?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var continueIds by remember(id) { mutableStateOf(emptySet<String>()) }
    val coroutines = rememberCoroutineScope()
    val memory = remember { RunbookValueMemory(app.getSharedPreferences(RunbookValueMemory.FILE, Context.MODE_PRIVATE)) }

    // The plan of the values given; what the core says of them stays on screen.
    suspend fun planWithValues(yaml: String) {
        try {
            plan = app.runbookRepository.plan(yaml, values)
            error = null
            if (params.isNotEmpty() && scope != null) memory.save(scope, id, values)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.userMessage()
        }
    }

    LaunchedEffect(scope, id) {
        if (scope == null) return@LaunchedEffect
        try {
            val yaml = app.runbookRepository.yaml(scope, id)
            yamlText = yaml
            params = app.runbookRepository.summary(yaml).params
            if (params.isEmpty()) {
                planWithValues(yaml)
            } else {
                values = initialRunbookValues(params, memory.load(scope, id))
                choices = runbookValueChoices(app, params)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.userMessage()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(title = { Text(plan?.name ?: stringResource(R.string.runbooks_title)) }, navigationIcon = { BackButton(onBack) })
        },
    ) { padding ->
        val yaml = yamlText
        val current = plan
        when {
            yaml != null && current == null && params.isNotEmpty() -> Column(
                Modifier.pageContent(padding).verticalScroll(rememberScrollState()).padding(16.dp),
            ) {
                RunbookParamsForm(
                    params = params,
                    values = values,
                    choices = choices,
                    error = error,
                    onChange = { name, value -> values = values + (name to value) },
                    onContinue = { coroutines.launch { planWithValues(yaml) } },
                )
            }
            yaml == null || current == null -> if (error != null) {
                EmptyText(error.orEmpty(), Modifier.pageContent(padding))
            } else {
                LoadingBox(Modifier.pageContent(padding))
            }
            else -> Column(
                Modifier.pageContent(padding).verticalScroll(rememberScrollState()).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                if (current.description.isNotEmpty()) MutedText(current.description)
                if (params.isNotEmpty()) {
                    MutedText(stringResource(R.string.runbook_params_values, runbookValuesLabel(values)))
                    TextButton(onClick = {
                        plan = null
                        continueIds = emptySet()
                    }) { Text(stringResource(R.string.runbook_params_change)) }
                }
                RunbookPlanSteps(current, continueIds, onToggleContinue = { stepId, on -> continueIds = if (on) continueIds + stepId else continueIds - stepId })
                if (!current.canRun(continueIds)) {
                    Text(stringResource(R.string.runbook_blocked), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                }
                error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                Button(
                    onClick = {
                        // The core refuses the demo before any step: its reason shows on the run screen.
                        runCatching { app.runbookManager.start(yaml, current, continueIds, values) }
                            .onSuccess { started -> if (started) onRunning() else error = app.getString(R.string.runbook_already_running) }
                            .onFailure { error = it.userMessage() }
                    },
                    enabled = current.canRun(continueIds),
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.runbook_run)) }
            }
        }
    }
}

/**
 * The steps of [plan], one block each. [onToggleContinue]: the "continue on error" switch of a
 * refused step (null on the import's consent, which only shows the plan).
 */
@Composable
internal fun RunbookPlanSteps(plan: RunbookPlan, continueIds: Set<String>, onToggleContinue: ((id: String, on: Boolean) -> Unit)?) {
    plan.steps.forEachIndexed { index, step ->
        if (index > 0) HorizontalDivider()
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            StepHeadline(step.action, step.id, step.destructive)
            Text(step.target.label, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
            if (step.detail.isNotEmpty()) MutedText(step.detail)
            if (step.role.isNotEmpty()) MutedText(stringResource(R.string.runbook_role, step.role))
            if (!step.allowed) {
                Text(stringResource(R.string.runbook_denied, step.denied), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                if (onToggleContinue != null && !step.continueOnError) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(stringResource(R.string.runbook_continue), modifier = Modifier.weight(1f))
                        Switch(checked = step.id in continueIds, onCheckedChange = { onToggleContinue(step.id, it) })
                    }
                }
            }
            if (step.continueOnError) MutedText(stringResource(R.string.runbook_continues))
        }
    }
}
