package embedding

// #cgo pkg-config: llama
// #include <llama.h>
// #include <stdlib.h>
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

type Embedder struct {
	model *C.struct_llama_model
	ctx   *C.struct_llama_context
	vocab *C.struct_llama_vocab
	nEmbd int
}

func NewEmbedder(modelPath string) (*Embedder, error) {
	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	mp := C.llama_model_default_params()
	model := C.llama_model_load_from_file(cPath, mp)
	if model == nil {
		return nil, fmt.Errorf("failed to load model: %s", modelPath)
	}

	cp := C.llama_context_default_params()
	cp.n_ctx = 512
	cp.n_batch = 512
	cp.n_ubatch = 512
	cp.embeddings = true

	ctx := C.llama_init_from_model(model, cp)
	if ctx == nil {
		C.llama_model_free(model)
		return nil, fmt.Errorf("failed to create context")
	}

	vocab := C.llama_model_get_vocab(model)
	nEmbd := int(C.llama_model_n_embd(model))

	return &Embedder{
		model: model,
		ctx:   ctx,
		vocab: vocab,
		nEmbd: nEmbd,
	}, nil
}

func (e *Embedder) Embed(text string) ([]float32, error) {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	textLen := C.int(len(text))

	// Tokenize: first call with 0 buffer to get required size
	maxTokens := 512
	tokens := make([]C.llama_token, maxTokens)

	nTokens := C.llama_tokenize(
		e.vocab,
		cText,
		textLen,
		&tokens[0],
		C.int(maxTokens),
		true,  // add_special (BOS)
		true,  // parse_special
	)

	if nTokens < 0 {
		// Need more space, try with the required size
		maxTokens = int(-nTokens)
		tokens = make([]C.llama_token, maxTokens)
		nTokens = C.llama_tokenize(
			e.vocab,
			cText,
			textLen,
			&tokens[0],
			C.int(maxTokens),
			true,
			true,
		)
		if nTokens < 0 {
			return nil, fmt.Errorf("tokenization failed")
		}
	}

	// Use llama_batch_init so we can set seq_id and logits per token.
	// llama_batch_get_one does not populate these, which causes
	// llama_get_embeddings_seq to return zeros.
	batch := C.llama_batch_init(nTokens, 0, 1)
	defer C.llama_batch_free(batch)

	batch.n_tokens = nTokens
	tokenSlice := unsafe.Slice(batch.token, int(nTokens))
	posSlice := unsafe.Slice(batch.pos, int(nTokens))
	nSeqSlice := unsafe.Slice(batch.n_seq_id, int(nTokens))
	seqSlice := unsafe.Slice(batch.seq_id, int(nTokens))
	logitsSlice := unsafe.Slice(batch.logits, int(nTokens))

	for i := C.int(0); i < nTokens; i++ {
		tokenSlice[i] = tokens[i]
		posSlice[i] = C.llama_pos(i)
		nSeqSlice[i] = 1
		*seqSlice[i] = 0
		logitsSlice[i] = 0
	}
	// Mark last token for output
	logitsSlice[nTokens-1] = 1

	if ret := C.llama_encode(e.ctx, batch); ret != 0 {
		return nil, fmt.Errorf("llama_encode failed: %d", ret)
	}

	// Use pooled sequence embedding for embedding models
	embPtr := C.llama_get_embeddings_seq(e.ctx, 0)
	if embPtr == nil {
		// Fall back to non-pooled embeddings
		embPtr = C.llama_get_embeddings(e.ctx)
	}
	if embPtr == nil {
		return nil, fmt.Errorf("llama_get_embeddings returned nil")
	}

	result := make([]float32, e.nEmbd)
	cSlice := unsafe.Slice(embPtr, e.nEmbd)
	for i := 0; i < e.nEmbd; i++ {
		result[i] = float32(cSlice[i])
	}

	// L2-normalize so cosine similarity works correctly
	var norm float64
	for _, v := range result {
		norm += float64(v) * float64(v)
	}
	if norm = math.Sqrt(norm); norm > 0 {
		for i := range result {
			result[i] = float32(float64(result[i]) / norm)
		}
	}

	return result, nil
}

func (e *Embedder) EmbeddingDim() int {
	return e.nEmbd
}

func (e *Embedder) Close() {
	if e.ctx != nil {
		C.llama_free(e.ctx)
		e.ctx = nil
	}
	if e.model != nil {
		C.llama_model_free(e.model)
		e.model = nil
	}
}
