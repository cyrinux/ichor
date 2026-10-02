package name.levis.ichor.ui.backup

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import name.levis.ichor.R
import name.levis.ichor.data.BackupManager
import name.levis.ichor.data.BackupPassphraseException
import name.levis.ichor.data.RestoreOutcome
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

sealed interface BackupState {
    data object Idle : BackupState

    /** Asking the passphrase that will seal a new backup. */
    data object NewPassphrase : BackupState
    data object Sealing : BackupState

    /** Sealed, the save picker is to be opened (once: [Saving] while it shows). */
    class ReadyToSave(val file: ByteArray) : BackupState

    /** The save picker is open. */
    class Saving(val file: ByteArray) : BackupState
    data object Saved : BackupState

    /** A picked backup file, waiting for its passphrase; [error] after a wrong one. */
    class Passphrase(val file: ByteArray, val error: UiText? = null) : BackupState
    data object Restoring : BackupState
    /** Restored, the screen is to act on [outcome] (once: [RestoreDone] after). */
    data class Restored(val outcome: RestoreOutcome) : BackupState
    data object RestoreDone : BackupState

    data class Failed(val message: UiText) : BackupState
}

/** Backing up (passphrase, seal, save) and restoring (pick, passphrase, restore); the file I/O stays in the UI. */
class BackupViewModel(private val manager: BackupManager) : ViewModel() {
    private val _state = MutableStateFlow<BackupState>(BackupState.Idle)
    val state: StateFlow<BackupState> = _state.asStateFlow()

    fun startBackup() {
        _state.value = BackupState.NewPassphrase
    }

    fun seal(passphrase: String) {
        _state.value = BackupState.Sealing
        viewModelScope.launch {
            _state.value = runCatching { manager.create(passphrase) }.fold(
                onSuccess = { BackupState.ReadyToSave(it) },
                onFailure = { BackupState.Failed(it.uiText()) },
            )
        }
    }

    /** The save picker opens for the sealed file; false when it already did (recomposition). */
    fun openSaver(): Boolean {
        val ready = _state.value as? BackupState.ReadyToSave ?: return false
        _state.value = BackupState.Saving(ready.file)
        return true
    }

    /** The restore's outcome, for the screen to act on once (recomposition, recreate()). */
    fun takeRestored(): RestoreOutcome? {
        val restored = _state.value as? BackupState.Restored ?: return null
        _state.value = BackupState.RestoreDone
        return restored.outcome
    }

    fun saved() {
        _state.value = BackupState.Saved
    }

    fun picked(file: ByteArray) {
        _state.value = BackupState.Passphrase(file)
    }

    fun restore(passphrase: String) {
        val file = (_state.value as? BackupState.Passphrase)?.file ?: return
        _state.value = BackupState.Restoring
        viewModelScope.launch {
            _state.value = runCatching { manager.restore(file, passphrase) }.fold(
                onSuccess = { BackupState.Restored(it) },
                onFailure = {
                    if (it is BackupPassphraseException) BackupState.Passphrase(file, it.text)
                    else BackupState.Failed(it.uiText())
                },
            )
        }
    }

    fun fail(message: UiText = UiText.Res(R.string.backup_err_open)) {
        _state.value = BackupState.Failed(message)
    }

    fun reset() {
        _state.value = BackupState.Idle
    }
}
