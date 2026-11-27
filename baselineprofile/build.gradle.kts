// Generates the app's baseline profile (app/src/main/generated/baselineProfiles/baseline-prof.txt)
// by walking the offline demo on a device: `just baseline-profile`, or the Baseline profile
// workflow. Never part of a release build: the profile it writes is committed.
plugins {
    alias(libs.plugins.android.test)
    alias(libs.plugins.androidx.baselineprofile)
}

android {
    namespace = "name.levis.ichor.baselineprofile"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        // Generating a profile needs API 33+, or a rooted device from API 28.
        minSdk = 28
        targetSdk = 37
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    targetProjectPath = ":app"

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

// The device or emulator adb sees (no Gradle managed device: no system image to download).
baselineProfile {
    useConnectedDevices = true
}

// The generator reads the app's id from here (the release build's, with its .foss suffix): the
// plugin does not pass it itself.
androidComponents {
    onVariants { variant ->
        val loader = variant.artifacts.getBuiltArtifactsLoader()
        variant.instrumentationRunnerArguments.put("targetAppId", variant.testedApks.map { loader.load(it)?.applicationId })
    }
}

dependencies {
    implementation(libs.androidx.test.ext.junit)
    implementation(libs.androidx.test.runner)
    implementation(libs.androidx.test.uiautomator)
    implementation(libs.androidx.benchmark.macro.junit4)
}
