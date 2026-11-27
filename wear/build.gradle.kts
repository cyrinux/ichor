// The Wear OS app: an activity listing the clusters and their nodes, tiles and complications showing the cluster's health from the snapshot the
// phone's background monitor sends over the Data Layer (the Play phone build's
// src/play/.../wear/WearSnapshotSyncs.kt). No talosconfig or kubeconfig on the watch.
plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

android {
    namespace = "name.levis.ichor.wear"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        // The Data Layer only links apps with the same id and signing key: the Play phone build's.
        applicationId = "name.levis.ichor"
        minSdk = 30
        targetSdk = 37
        versionCode = System.getenv("ICHOR_BUILD_NUMBER")?.toIntOrNull() ?: 1
        versionName = System.getenv("ICHOR_VERSION") ?: "0.0.0-unknown"
    }

    // The phone's release key (see app/build.gradle.kts); unsigned without it.
    val keystorePath = System.getenv("ICHOR_KEYSTORE")
    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = System.getenv("ICHOR_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("ICHOR_KEY_ALIAS")
                keyPassword = System.getenv("ICHOR_KEY_PASSWORD") ?: System.getenv("ICHOR_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            // As the phone app: no VCS info file, so a rebuild of the tag gives the same APK.
            vcsInfo.include = false
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
            signingConfig = signingConfigs.findByName("release")
        }
    }

    buildFeatures {
        compose = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    // As the phone app: Lint in `./build.sh check` (:wear:lintDebug) against lint-baseline.xml.
    lint {
        baseline = file("lint-baseline.xml")
        abortOnError = true
        warningsAsErrors = false
        checkDependencies = false
    }
}

dependencies {
    implementation(project(":core-widget"))
    implementation(libs.androidx.core.ktx)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.play.services.wearable)
    implementation(libs.wear.tiles)
    implementation(libs.wear.protolayout)
    implementation(libs.wear.complications.data.source)
    implementation(libs.androidx.concurrent.futures)
    implementation(libs.androidx.activity.compose)
    implementation(libs.wear.compose.material3)
    implementation(libs.wear.compose.foundation)
    implementation(libs.wear.compose.navigation)

    testImplementation(libs.junit)
}
