package name.levis.ichor.data

/** In-memory [SealedValue]; [failWrites] stands for a Keystore that cannot be used. */
internal class MemoryValue(var value: String? = null) : SealedValue {
    var failWrites = false

    override fun read(): String? = value
    override fun write(value: String) {
        check(!failWrites) { "Keystore unavailable" }
        this.value = value
    }
    override fun delete() {
        value = null
    }
}
