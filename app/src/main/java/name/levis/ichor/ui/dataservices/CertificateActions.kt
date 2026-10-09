package name.levis.ichor.ui.dataservices

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.model.CertDetails
import name.levis.ichor.model.Certificate
import name.levis.ichor.ui.KeyedActions
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.ResultToasts
import name.levis.ichor.ui.uiText

/** The open details of [cert]: its issuance chain, events and controller log lines. */
data class CertDetailsState(
    val cert: Certificate,
    val details: CertDetails? = null,
    /** The last load failed (the details, if any, are the previous ones). */
    val error: UiText? = null,
    val loading: Boolean = true,
)

/** Outcome of a forced renewal of the certificate [label], shown once. */
data class CertificateRenewResult(val label: String, val error: UiText?)

/**
 * Certificate details and forced renewals, run in [scope] (a ViewModel's). [onChanged] runs
 * after a renewal went through, to refresh the data services and show the issuance.
 */
class CertificateActions(
    private val scope: CoroutineScope,
    private val dataServices: DataServicesRepository,
    private val onChanged: () -> Unit,
) {
    private val actions = KeyedActions<CertificateRenewResult>(scope)
    /** Labels of the certificates with a renewal request in flight. */
    val busy: StateFlow<Set<String>> get() = actions.busy

    private val _sheet = MutableStateFlow<CertDetailsState?>(null)
    /** The open details, null when closed. */
    val sheet: StateFlow<CertDetailsState?> = _sheet.asStateFlow()
    private var loadJob: Job? = null

    val results: Flow<CertificateRenewResult> get() = actions.results

    fun renew(cert: Certificate) {
        val key = cert.label
        actions.launch(key, { dataServices.renewCertificate(cert.namespace, cert.name) }, { CertificateRenewResult(key, it.exceptionOrNull()?.uiText()) }) {
            onChanged()
            if (_sheet.value?.cert?.label == key) reload()
        }
    }

    fun openDetails(cert: Certificate) {
        _sheet.value = CertDetailsState(cert)
        reload()
    }

    fun closeDetails() {
        loadJob?.cancel()
        _sheet.value = null
    }

    /** Loads the open details again, keeping the previous ones on screen meanwhile. */
    fun reload() {
        val cert = _sheet.value?.cert ?: return
        loadJob?.cancel()
        _sheet.update { it?.copy(loading = true, error = null) }
        loadJob = scope.launch {
            try {
                val details = dataServices.certificateDetails(cert.namespace, cert.name)
                _sheet.update { it?.copy(details = details, loading = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _sheet.update { it?.copy(error = e.uiText(), loading = false) }
            }
        }
    }
}

/** A toast for each outcome of [results]. */
@Composable
fun CertificateRenewToasts(results: Flow<CertificateRenewResult>) = ResultToasts(results) { context, r ->
    val text = r.error?.resolve(context)?.let { context.getString(R.string.certmanager_renew_failed, r.label, it) }
        ?: context.getString(R.string.certmanager_renew_started, r.label)
    text to (r.error != null)
}

/** Confirms a forced renewal: an ACME issuer counts it against its rate limits. */
@Composable
fun RenewConfirmDialog(cert: Certificate, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.certmanager_renew_title, cert.label),
        text = stringResource(R.string.certmanager_renew_text),
        confirm = stringResource(R.string.certmanager_renew),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    )
}
