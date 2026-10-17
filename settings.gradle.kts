pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "ichor"
include(":app")
// The widget's status rules and the phone-to-watch payload, shared by :app and :wear.
include(":core-widget")
// Wear OS tile and complication, fed by the phone's snapshot over the Data Layer.
include(":wear")
