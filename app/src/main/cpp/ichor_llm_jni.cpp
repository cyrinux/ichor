// The JNI side of name.levis.ichor.localmodel.LlamaNative: the engine of native/llm, with
// text crossing as UTF-8 bytes both ways (JNI's own strings are "modified UTF-8", which
// mangles characters outside the BMP).
#include <jni.h>

#include <string>
#include <vector>

#include "ichor_llm.h"

namespace {

struct Callback {
    JNIEnv *env;
    jobject sink;
    jmethodID on_piece;
};

void on_piece(const char *text, size_t len, void *user) {
    auto *cb = static_cast<Callback *>(user);
    jbyteArray bytes = cb->env->NewByteArray(static_cast<jsize>(len));
    if (bytes == nullptr) {
        cb->env->ExceptionClear();
        return;
    }
    cb->env->SetByteArrayRegion(bytes, 0, static_cast<jsize>(len), reinterpret_cast<const jbyte *>(text));
    cb->env->CallVoidMethod(cb->sink, cb->on_piece, bytes);
    if (cb->env->ExceptionCheck()) {
        cb->env->ExceptionClear(); // the Kotlin side never throws; never let it unwind C++
    }
    cb->env->DeleteLocalRef(bytes);
}

std::string utf8(JNIEnv *env, jstring s) {
    if (s == nullptr) {
        return {};
    }
    const char *chars = env->GetStringUTFChars(s, nullptr);
    std::string out = chars == nullptr ? "" : chars;
    if (chars != nullptr) {
        env->ReleaseStringUTFChars(s, chars);
    }
    return out;
}

std::string bytes_utf8(JNIEnv *env, jbyteArray b) {
    if (b == nullptr) {
        return {};
    }
    jsize n = env->GetArrayLength(b);
    std::string out(static_cast<size_t>(n), '\0');
    env->GetByteArrayRegion(b, 0, n, reinterpret_cast<jbyte *>(out.data()));
    return out;
}

} // namespace

extern "C" {

JNIEXPORT jlong JNICALL
Java_name_levis_ichor_localmodel_LlamaNative_load(JNIEnv *env, jclass, jstring path, jint context_tokens, jint threads,
                                                   jobjectArray error) {
    char err[256] = {0};
    ichor_llm *llm = ichor_llm_load(utf8(env, path).c_str(), context_tokens, threads, 0, err, sizeof err);
    if (llm == nullptr && error != nullptr && env->GetArrayLength(error) > 0) {
        jstring msg = env->NewStringUTF(err);
        env->SetObjectArrayElement(error, 0, msg);
        env->DeleteLocalRef(msg);
    }
    return reinterpret_cast<jlong>(llm);
}

JNIEXPORT jstring JNICALL
Java_name_levis_ichor_localmodel_LlamaNative_generate(JNIEnv *env, jclass, jlong handle, jobjectArray roles,
                                                       jobjectArray contents, jbyteArray assistant_prefix,
                                                       jint answer_tokens, jfloat temperature, jobject sink) {
    auto *llm = reinterpret_cast<ichor_llm *>(handle);
    jsize n = env->GetArrayLength(roles);
    auto count = static_cast<size_t>(n);

    std::vector<std::string> role_text(count), content_text(count);
    std::vector<const char *> role_ptr(count), content_ptr(count);
    for (jsize i = 0; i < n; i++) {
        auto role = static_cast<jstring>(env->GetObjectArrayElement(roles, i));
        auto content = static_cast<jbyteArray>(env->GetObjectArrayElement(contents, i));
        role_text[static_cast<size_t>(i)] = utf8(env, role);
        content_text[static_cast<size_t>(i)] = bytes_utf8(env, content);
        env->DeleteLocalRef(role);
        env->DeleteLocalRef(content);
    }
    for (size_t i = 0; i < count; i++) {
        role_ptr[i] = role_text[i].c_str();
        content_ptr[i] = content_text[i].c_str();
    }

    jclass cls = env->GetObjectClass(sink);
    Callback cb{env, sink, env->GetMethodID(cls, "onPiece", "([B)V")};
    env->DeleteLocalRef(cls);
    if (cb.on_piece == nullptr) {
        env->ExceptionClear();
        return env->NewStringUTF("the app's engine binding is broken (onPiece)");
    }

    std::string prefix = bytes_utf8(env, assistant_prefix);
    char status[256] = {0};
    ichor_llm_generate(llm, role_ptr.data(), content_ptr.data(), count, prefix.c_str(), answer_tokens, temperature,
                       on_piece, &cb, status, sizeof status);
    return env->NewStringUTF(status);
}

JNIEXPORT void JNICALL
Java_name_levis_ichor_localmodel_LlamaNative_cancel(JNIEnv *, jclass, jlong handle) {
    ichor_llm_cancel(reinterpret_cast<ichor_llm *>(handle));
}

JNIEXPORT void JNICALL
Java_name_levis_ichor_localmodel_LlamaNative_free(JNIEnv *, jclass, jlong handle) {
    ichor_llm_free(reinterpret_cast<ichor_llm *>(handle));
}

} // extern "C"
