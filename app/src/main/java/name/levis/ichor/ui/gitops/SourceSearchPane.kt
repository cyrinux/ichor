package name.levis.ichor.ui.gitops

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.GitRepoSearch
import name.levis.ichor.model.GitSearchHit
import name.levis.ichor.model.groupSearchHits
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * Find in repository, in place of the directory listing: the field, then every matching line
 * by file; a line opens the file there ([onHit]). [onClose] goes back to the listing.
 */
@Composable
internal fun SourceSearchPane(search: SourceSearch, onSearch: (String) -> Unit, onClose: () -> Unit, onHit: (String, Int) -> Unit) {
    var text by rememberSaveable { mutableStateOf(search.query) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { if (search.result == null) focus.requestFocus() }
    Column(Modifier.fillMaxSize()) {
        OutlinedTextField(
            value = text,
            onValueChange = { text = it },
            singleLine = true,
            label = { Text(stringResource(R.string.source_search_field)) },
            leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
            trailingIcon = { TooltipIconButton(Icons.Outlined.Close, stringResource(R.string.source_search_close), onClick = onClose) },
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search, autoCorrectEnabled = false),
            keyboardActions = KeyboardActions(onSearch = { onSearch(text) }),
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp).focusRequester(focus),
        )
        when (val r = search.result) {
            null -> MutedText(stringResource(R.string.source_search_hint), Modifier.padding(horizontal = 16.dp))
            UiState.Loading -> Column(Modifier.padding(horizontal = 16.dp).semantics { liveRegion = LiveRegionMode.Polite }) {
                LinearProgressIndicator(Modifier.fillMaxWidth())
                MutedText(stringResource(R.string.source_search_running), Modifier.padding(top = 8.dp))
            }
            is UiState.Failed -> ErrorBox(r.message, { onSearch(search.query) })
            is UiState.Loaded -> SearchHits(r.data, search.query, onHit)
        }
    }
}

@Composable
private fun SearchHits(result: GitRepoSearch, query: String, onHit: (String, Int) -> Unit) {
    if (result.hits.isEmpty()) {
        EmptyState(Icons.Outlined.SearchOff, stringResource(R.string.source_search_none, query))
        return
    }
    val groups = remember(result) { groupSearchHits(result.hits) }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        item(key = "summary") {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp).semantics { liveRegion = LiveRegionMode.Polite }) {
                MutedText(stringResource(R.string.source_search_summary, result.hits.size, result.searched))
                if (result.truncated) MutedText(stringResource(R.string.source_search_truncated))
            }
        }
        groups.forEach { (path, hits) ->
            item(key = "f:$path") {
                Text(
                    path,
                    style = MaterialTheme.typography.labelLarge,
                    fontFamily = FontFamily.Monospace,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
                )
            }
            items(hits, key = { "h:${it.path}:${it.line}" }) { hit -> HitRow(hit) { onHit(hit.path, hit.line) } }
        }
    }
}

@Composable
private fun HitRow(hit: GitSearchHit, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClickLabel = stringResource(R.string.source_search_open_line), onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 6.dp),
    ) {
        MutedText("${hit.line}", Modifier.padding(end = 12.dp))
        Text(
            hit.text,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
    }
}
