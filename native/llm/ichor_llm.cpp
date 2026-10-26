// See ichor_llm.h.
#include "ichor_llm.h"

#if __has_include(<llama/llama.h>)
#include <llama/llama.h> // the iOS xcframework
#else
#include "llama.h"
#endif

#include <algorithm>
#include <atomic>
#include <cstdio>
#include <cstring>
#include <mutex>
#include <string>
#include <vector>

struct ichor_llm {
    llama_model *model = nullptr;
    llama_context *ctx = nullptr;
    int context_tokens = 0;
    std::atomic<bool> cancel{false};
    std::mutex run; // one generation at a time
};

namespace {

constexpr int kBatch = 512;

void set_text(char *buf, size_t len, const std::string &text) {
    if (buf == nullptr || len == 0) {
        return;
    }
    size_t n = std::min(text.size(), len - 1);
    std::memcpy(buf, text.data(), n);
    buf[n] = '\0';
}

// The model's logs would go to stderr, which nobody reads on a phone: drop them.
void quiet(enum ggml_log_level, const char *, void *) {}

void init_backend() {
    static std::once_flag once;
    std::call_once(once, [] {
        llama_log_set(quiet, nullptr);
        llama_backend_init();
    });
}

bool aborted(void *data) {
    return static_cast<std::atomic<bool> *>(data)->load();
}

// The length of the longest prefix of s that ends on a whole UTF-8 character.
size_t complete_utf8(const std::string &s) {
    size_t i = s.size();
    // Look back over at most 3 continuation bytes for the start of the last character.
    for (size_t back = 1; back <= 4 && back <= s.size(); back++) {
        unsigned char c = static_cast<unsigned char>(s[s.size() - back]);
        if ((c & 0xC0) == 0x80) {
            continue; // a continuation byte
        }
        size_t need = c < 0x80 ? 1 : (c & 0xE0) == 0xC0 ? 2 : (c & 0xF0) == 0xE0 ? 3 : (c & 0xF8) == 0xF0 ? 4 : 1;
        i = back >= need ? s.size() : s.size() - back;
        break;
    }
    return i;
}

bool apply_template(const llama_model *model, const std::vector<llama_chat_message> &chat, std::string &out) {
    const char *tmpl = llama_model_chat_template(model, nullptr);
    size_t chars = 0;
    for (const auto &m : chat) {
        chars += std::strlen(m.content) + 16;
    }
    std::vector<char> buf(chars * 2 + 256);
    int32_t n = llama_chat_apply_template(tmpl, chat.data(), chat.size(), true, buf.data(), static_cast<int32_t>(buf.size()));
    if (n > static_cast<int32_t>(buf.size())) {
        buf.resize(static_cast<size_t>(n));
        n = llama_chat_apply_template(tmpl, chat.data(), chat.size(), true, buf.data(), static_cast<int32_t>(buf.size()));
    }
    if (n < 0) {
        return false;
    }
    out.assign(buf.data(), static_cast<size_t>(n));
    return true;
}

bool tokenize(const llama_vocab *vocab, const std::string &text, std::vector<llama_token> &out) {
    int32_t n = -llama_tokenize(vocab, text.data(), static_cast<int32_t>(text.size()), nullptr, 0, true, true);
    if (n <= 0) {
        return false;
    }
    out.resize(static_cast<size_t>(n));
    return llama_tokenize(vocab, text.data(), static_cast<int32_t>(text.size()), out.data(), n, true, true) == n;
}

} // namespace

extern "C" ichor_llm *ichor_llm_load(const char *path, int context_tokens, int threads, int gpu_layers, char *err, size_t err_len) {
    init_backend();

    llama_model_params mparams = llama_model_default_params();
    mparams.n_gpu_layers = gpu_layers;
    llama_model *model = llama_model_load_from_file(path, mparams);
    if (model == nullptr) {
        set_text(err, err_len, "could not load the model file (damaged, or a format this version does not read)");
        return nullptr;
    }

    llama_context_params cparams = llama_context_default_params();
    cparams.n_ctx = static_cast<uint32_t>(context_tokens);
    cparams.n_batch = kBatch;
    cparams.n_ubatch = kBatch;
    cparams.n_seq_max = 1;
    cparams.n_threads = threads;
    cparams.n_threads_batch = threads;
    cparams.no_perf = true;
    llama_context *ctx = llama_init_from_model(model, cparams);
    if (ctx == nullptr) {
        llama_model_free(model);
        set_text(err, err_len, "out-of-memory");
        return nullptr;
    }

    auto *llm = new ichor_llm;
    llm->model = model;
    llm->ctx = ctx;
    llm->context_tokens = static_cast<int>(llama_n_ctx(ctx));
    llama_set_abort_callback(ctx, aborted, &llm->cancel);
    return llm;
}

extern "C" void ichor_llm_generate(ichor_llm *llm, const char *const *roles, const char *const *contents, size_t n,
                                   const char *assistant_prefix, int answer_tokens, float temperature,
                                   ichor_llm_piece_fn on_piece, void *user, char *status, size_t status_len) {
    std::lock_guard<std::mutex> lock(llm->run);
    llm->cancel.store(false);
    set_text(status, status_len, "");

    std::vector<llama_chat_message> chat;
    chat.reserve(n);
    for (size_t i = 0; i < n; i++) {
        chat.push_back({roles[i], contents[i]});
    }

    std::string prompt;
    if (!apply_template(llm->model, chat, prompt)) {
        set_text(status, status_len, "the model file has no chat template this version knows");
        return;
    }
    if (assistant_prefix != nullptr) {
        prompt += assistant_prefix;
    }

    const llama_vocab *vocab = llama_model_get_vocab(llm->model);
    std::vector<llama_token> tokens;
    if (!tokenize(vocab, prompt, tokens)) {
        set_text(status, status_len, "could not read the question");
        return;
    }

    int room = llm->context_tokens - answer_tokens;
    if (static_cast<int>(tokens.size()) > room) {
        set_text(status, status_len, "too-long " + std::to_string(tokens.size()) + " " + std::to_string(std::max(room, 0)));
        return;
    }

    llama_memory_clear(llama_get_memory(llm->ctx), true);

    // Read the prompt, a batch at a time.
    for (size_t i = 0; i < tokens.size(); i += kBatch) {
        int32_t count = static_cast<int32_t>(std::min(tokens.size() - i, static_cast<size_t>(kBatch)));
        int32_t rc = llama_decode(llm->ctx, llama_batch_get_one(tokens.data() + i, count));
        if (rc == 2 || llm->cancel.load()) {
            return; // cancelled
        }
        if (rc != 0) {
            set_text(status, status_len, rc == 1 ? "out-of-memory" : "the model could not read the question (error " + std::to_string(rc) + ")");
            return;
        }
    }

    llama_sampler *smpl = llama_sampler_chain_init(llama_sampler_chain_default_params());
    llama_sampler_chain_add(smpl, llama_sampler_init_top_k(40));
    llama_sampler_chain_add(smpl, llama_sampler_init_top_p(0.95f, 1));
    llama_sampler_chain_add(smpl, llama_sampler_init_min_p(0.05f, 1));
    llama_sampler_chain_add(smpl, llama_sampler_init_temp(temperature));
    llama_sampler_chain_add(smpl, llama_sampler_init_dist(LLAMA_DEFAULT_SEED));

    std::string pending;
    char piece[256];
    for (int i = 0; i < answer_tokens && !llm->cancel.load(); i++) {
        llama_token tok = llama_sampler_sample(smpl, llm->ctx, -1);
        if (llama_vocab_is_eog(vocab, tok)) {
            break;
        }

        int32_t len = llama_token_to_piece(vocab, tok, piece, sizeof piece, 0, false);
        if (len > 0) {
            pending.append(piece, static_cast<size_t>(len));
            size_t whole = complete_utf8(pending);
            if (whole > 0) {
                on_piece(pending.data(), whole, user);
                pending.erase(0, whole);
            }
        }

        int32_t rc = llama_decode(llm->ctx, llama_batch_get_one(&tok, 1));
        if (rc == 2) {
            break; // cancelled
        }
        if (rc != 0) {
            set_text(status, status_len, rc == 1 ? "out-of-memory" : "the model stopped with error " + std::to_string(rc));
            break;
        }
    }
    llama_sampler_free(smpl);
}

extern "C" void ichor_llm_cancel(ichor_llm *llm) {
    llm->cancel.store(true);
}

extern "C" int ichor_llm_context_tokens(const ichor_llm *llm) {
    return llm->context_tokens;
}

extern "C" void ichor_llm_free(ichor_llm *llm) {
    if (llm == nullptr) {
        return;
    }
    llm->cancel.store(true);
    std::lock_guard<std::mutex> lock(llm->run);
    llama_free(llm->ctx);
    llama_model_free(llm->model);
    delete llm;
}
