# gomobile bindings are called through JNI. With -javapkg=name.levis they share the app's
# root package, so keep only the generated classes, not the whole package.
-keep class go.** { *; }
-keep class name.levis.talosmobile.Talosmobile { *; }
-keep class name.levis.talosmobile.HealthListener { *; }
-keep class name.levis.talosmobile.HealthRun { *; }
