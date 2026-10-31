package name.levis.ichor.data

/**
 * The cloud providers' API tokens, one per provider and project (a label the person gives: the
 * API does not name the project). One sealed file, protected like the stored configs and never
 * part of a backup: a write token can delete a whole project. A file that cannot be read is
 * never overwritten.
 */
class CloudProviderStore(private val file: SealedValue) {
    private var tokens: Map<String, String>? = null

    /** The projects with a token, sorted. */
    @Synchronized
    fun projects(provider: String): List<String> =
        read().orEmpty().keys.filter { it.startsWith("$provider:") }.map { it.removePrefix("$provider:") }.sorted()

    @Synchronized
    fun token(provider: String, project: String): String = read()?.get("$provider:$project").orEmpty()

    /** A blank token removes the project. Returns false when the store cannot be read. */
    @Synchronized
    fun save(provider: String, project: String, token: String): Boolean {
        val current = read() ?: return false
        val updated = withAuthState(current, "$provider:$project", token.trim())
        tokens = updated
        runCatching { if (updated.isEmpty()) file.delete() else file.write(encodeAuthStates(updated)) }
        return true
    }

    /** Forgets every token, with the file. */
    @Synchronized
    fun clear() {
        runCatching { file.delete() }
        tokens = emptyMap()
    }

    /** Seals or unseals the file for the security-key mode just set (see SecureStore.reseal). */
    @Synchronized
    fun reseal() {
        file.reseal()
    }

    private fun read(): Map<String, String>? {
        tokens?.let { return it }
        return runCatching { decodeAuthStates(file.read()) }.getOrNull()?.also { tokens = it }
    }

    companion object {
        const val FILE = "cloudproviders.enc"
        const val KEY_ALIAS = "cloudproviders"
        const val HETZNER = "hetzner"
    }
}
