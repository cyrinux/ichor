package name.levis.ichor.ui.workloads

import androidx.annotation.StringRes
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeNamespaces
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.SCOPE_CHIPS_MAX
import name.levis.ichor.model.matchingNamespaces
import name.levis.ichor.model.scopeChoices
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * The namespace the Kubernetes tabs list ([scope]), the cluster's namespaces when they could
 * be listed (null until then, or when that failed), and how to pick another one.
 */
@Stable
class KubeScopeControl(val scope: KubeScope, val namespaces: KubeNamespaces?, val onScope: (KubeScope) -> Unit)

/**
 * Search field and namespace picker, shared by the Workloads, Pods and CronJobs tabs: chips
 * ("All namespaces" first), a searchable sheet past [SCOPE_CHIPS_MAX] namespaces, or a field
 * to type one when the credentials cannot list them. [loaded]: namespaces of the rows on
 * screen, offered while the cluster's are unknown.
 */
@Composable
internal fun KubeFilters(
    control: KubeScopeControl,
    loaded: List<String>,
    query: String,
    onQuery: (String) -> Unit,
    @StringRes placeholder: Int = R.string.workloads_search,
) {
    val choices = remember(control.namespaces, loaded, control.scope) { scopeChoices(control.namespaces, loaded, control.scope) }
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SearchField(query, onQuery, stringResource(placeholder), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
        when {
            control.namespaces?.forbidden == true -> TypedNamespace(control)
            choices.size > SCOPE_CHIPS_MAX -> ScopeSheetButton(control, choices)
            else -> ScopeChips(control, choices)
        }
    }
}

@Composable
private fun ScopeChips(control: KubeScopeControl, choices: List<String>) {
    val selected = control.scope.namespace
    LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        item {
            FilterChip(
                selected = selected == null,
                onClick = { control.onScope(KubeScope(chosen = true)) },
                label = { Text(stringResource(R.string.workloads_all_namespaces)) },
            )
        }
        items(choices, key = { it }) { ns ->
            FilterChip(selected = selected == ns, onClick = { control.onScope(KubeScope(ns, chosen = true)) }, label = { Text(ns, fontFamily = FontFamily.Monospace) })
        }
    }
}

@Composable
private fun ScopeSheetButton(control: KubeScopeControl, choices: List<String>) {
    var open by rememberSaveable { mutableStateOf(false) }
    AssistChip(
        onClick = { open = true },
        label = { Text(control.scope.namespace ?: stringResource(R.string.workloads_all_namespaces), fontFamily = control.scope.namespace?.let { FontFamily.Monospace }) },
        leadingIcon = { Icon(Icons.Outlined.FilterList, contentDescription = null) },
        modifier = Modifier.padding(horizontal = 16.dp),
    )
    if (open) {
        NamespaceSheet(
            choices,
            selected = control.scope.namespace,
            onPick = {
                open = false
                control.onScope(KubeScope(it, chosen = true))
            },
            onDismiss = { open = false },
        )
    }
}

/** Every namespace, searchable; [onPick] null for every namespace. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NamespaceSheet(choices: List<String>, selected: String?, onPick: (String?) -> Unit, onDismiss: () -> Unit) {
    var search by rememberSaveable { mutableStateOf("") }
    val shown = remember(choices, search) { choices.matchingNamespaces(search) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxHeight(0.85f)) {
            SearchField(search, { search = it }, stringResource(R.string.kube_scope_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
            LazyColumn(Modifier.fillMaxWidth()) {
                if (search.isBlank()) {
                    item(key = "*") { NamespaceItem(stringResource(R.string.workloads_all_namespaces), mono = false, selected = selected == null) { onPick(null) } }
                }
                items(shown, key = { it }) { ns -> NamespaceItem(ns, mono = true, selected = selected == ns) { onPick(ns) } }
            }
        }
    }
}

@Composable
private fun NamespaceItem(label: String, mono: Boolean, selected: Boolean, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(label, fontFamily = if (mono) FontFamily.Monospace else null) },
        trailingContent = { if (selected) Icon(Icons.Outlined.Check, contentDescription = null) },
        modifier = Modifier.clickable(onClick = onClick),
    )
}

/** The credentials cannot list namespaces (L6): the user types the one to list, remembered. */
@Composable
private fun TypedNamespace(control: KubeScopeControl) {
    var text by rememberSaveable(control.scope) { mutableStateOf(control.scope.namespace.orEmpty()) }
    val apply = { control.onScope(KubeScope(text.trim().ifEmpty { null }, chosen = true)) }
    OutlinedTextField(
        value = text,
        onValueChange = { text = it },
        label = { Text(stringResource(R.string.kube_scope_typed_label)) },
        supportingText = { Text(stringResource(R.string.kube_scope_forbidden_hint)) },
        singleLine = true,
        textStyle = LocalTextStyle.current.copy(fontFamily = FontFamily.Monospace),
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { apply() }),
        trailingIcon = { TooltipIconButton(Icons.Outlined.Check, stringResource(R.string.kube_scope_apply), onClick = apply) },
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
    )
}
