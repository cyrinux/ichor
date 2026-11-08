// The widget's status rules and the payload the phone sends the watch, shared by :app and
// :wear so the tile and the home-screen widget never disagree. Plain Kotlin and
// kotlinx.serialization only: it is part of the open-source phone build.
plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "name.levis.ichor.widget.core"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        minSdk = 26
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    api(libs.kotlinx.serialization.json)

    testImplementation(libs.junit)
}
