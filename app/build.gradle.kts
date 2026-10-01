plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "dev.talos.viewer"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.talos.viewer"
        minSdk = 26
        targetSdk = 35
        // From scripts/version.sh (exported by build.sh); fallbacks for IDE builds.
        versionCode = System.getenv("TALOS_VIEWER_BUILD_NUMBER")?.toIntOrNull() ?: 1
        versionName = System.getenv("TALOS_VIEWER_VERSION") ?: "0.0.0-unknown"

        // The Go core is only built for these ABIs (see build.sh).
        ndk {
            abiFilters += listOf("arm64-v8a", "x86_64")
        }
    }

    // Release signing comes from env vars so CI (Forgejo) can inject secrets.
    val keystorePath = System.getenv("TALOS_KEYSTORE")
    signingConfigs {
        if (keystorePath != null) {
            create("release") {
                storeFile = file(keystorePath)
                storePassword = System.getenv("TALOS_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("TALOS_KEY_ALIAS")
                // Like lectarr: the key password defaults to the store password.
                keyPassword = System.getenv("TALOS_KEY_PASSWORD") ?: System.getenv("TALOS_KEYSTORE_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }
}

dependencies {
    implementation(files("libs/talosmobile.aar"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.navigation.compose)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.ui)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.camerax.camera2)
    implementation(libs.camerax.lifecycle)
    implementation(libs.camerax.view)
    implementation(libs.mlkit.barcode)
    implementation(libs.androidx.biometric)
    implementation(libs.androidx.fragment.ktx)
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.work.runtime)
    implementation(libs.androidx.glance.appwidget)
    implementation(libs.androidx.glance.material3)
    debugImplementation(libs.compose.ui.tooling)

    testImplementation(libs.junit)
}
