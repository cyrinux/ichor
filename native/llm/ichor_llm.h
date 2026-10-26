// The on-device model engine of Ichor: llama.cpp behind a small C API, shared by the
// Android app (JNI, app/src/main/cpp) and the iOS app (bridging header). It runs one chat
// turn at a time on a model file the app downloaded or imported; the prompt, the masking
// and everything around the answer are the Go core's (go/ichorgo/ai_device.go).
#ifndef ICHOR_LLM_H
#define ICHOR_LLM_H

#include <stdbool.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct ichor_llm ichor_llm;

// Called with each piece of the answer, as UTF-8 that never ends inside a character.
typedef void (*ichor_llm_piece_fn)(const char *text, size_t len, void *user);

// Loads the model file at path with a context of context_tokens. threads is the number
// of CPU threads to use, gpu_layers how many layers go to the GPU (0: none, 999: all).
// Returns NULL on failure with a message in err: "out-of-memory" when the context could
// not be allocated, else why the file could not be loaded.
ichor_llm *ichor_llm_load(const char *path, int context_tokens, int threads, int gpu_layers,
                          char *err, size_t err_len);

// Answers the chat (n messages: roles "system", "user", "assistant"; the last one the
// user's) with at most answer_tokens tokens, writing assistant_prefix after the chat
// template's assistant turn. Blocks until done. Writes the outcome in status, in the Go
// core's words: "" on success or when cancelled, "too-long <prompt> <room>",
// "out-of-memory", or a message.
void ichor_llm_generate(ichor_llm *llm, const char *const *roles, const char *const *contents, size_t n,
                        const char *assistant_prefix, int answer_tokens, float temperature,
                        ichor_llm_piece_fn on_piece, void *user, char *status, size_t status_len);

// Stops the running generation soon; safe from any thread. The next generate starts afresh.
void ichor_llm_cancel(ichor_llm *llm);

// The context size the model was loaded with.
int ichor_llm_context_tokens(const ichor_llm *llm);

void ichor_llm_free(ichor_llm *llm);

#ifdef __cplusplus
}
#endif

#endif
