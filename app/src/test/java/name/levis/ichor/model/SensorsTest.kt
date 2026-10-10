package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SensorsTest {

    @Test
    fun decodesGoJson() {
        val json = """
            {"temperatures":[{"chip":"coretemp","label":"Package id 0","celsius":61.3,"critCelsius":100,"maxCelsius":85}],
             "fans":[{"chip":"nct6775","label":"CPU fan","rpm":1180}],"voltages":[{"chip":"nct6775","label":"Vcore","volts":0.912}],
             "thermalZones":[{"type":"x86_pkg_temp","celsius":61}],
             "cpuFreq":[{"cpu":0,"currentMhz":2000,"minMhz":800,"maxMhz":3400,"governor":"performance"}],
             "throttle":{"coreEvents":15,"packageEvents":5},"throttled":true,
             "pci":[{"id":"0000:00:1f.6","class":"Network controller","subclass":"Ethernet controller","vendor":"Intel Corporation","product":"I219-LM","driver":"e1000e"}],
             "errors":{}}
        """.trimIndent()
        val s = TalosJson.decodeFromString(NodeSensors.serializer(), json)
        assertTrue(s.throttled)
        assertEquals(61.3, s.temperatures.single().celsius, 0.0)
        assertEquals(15L, s.throttle?.coreEvents)
        assertEquals("Network controller", s.pci.single().`class`)
        assertEquals("e1000e", s.pci.single().driver)
        assertFalse(s.sensorsEmpty)
        assertNull(s.sensorsError)
    }

    @Test
    fun vmIsEmpty() {
        val json = """{"temperatures":[],"fans":[],"voltages":[],"thermalZones":[],"cpuFreq":[],"throttle":null,"throttled":false,"pci":[],"errors":{"pci":"not available"}}"""
        val s = TalosJson.decodeFromString(NodeSensors.serializer(), json)
        assertTrue(s.sensorsEmpty)
        assertNull(s.throttle)
        // A PCI failure belongs to the PCI card, not the Sensors one.
        assertNull(s.sensorsError)
        assertEquals("boom", s.copy(errors = mapOf(SensorSection.CPU_FREQ to "boom")).sensorsError)
    }

    @Test
    fun temperatureLimits() {
        assertEquals(0.5f, TemperatureSensor(celsius = 42.5, maxCelsius = 85.0, critCelsius = 100.0).limitFraction!!, 0.001f)
        assertEquals(0.5f, TemperatureSensor(celsius = 50.0, critCelsius = 100.0).limitFraction!!, 0.001f)
        assertEquals(1f, TemperatureSensor(celsius = 120.0, maxCelsius = 85.0).limitFraction!!, 0.001f)
        assertNull(TemperatureSensor(celsius = 40.0).limitFraction)
        assertTrue(TemperatureSensor(celsius = 86.0, maxCelsius = 85.0).hot)
        assertTrue(TemperatureSensor(celsius = 100.0, critCelsius = 100.0).hot)
        assertFalse(TemperatureSensor(celsius = 84.0, maxCelsius = 85.0, critCelsius = 100.0).hot)
    }
}
