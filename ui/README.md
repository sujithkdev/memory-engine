# Memory Studio

A local chat interface for the existing Go Memory Engine and your installed Ollama models. No npm packages or build step are required. Requires Node.js 22+.

## Start

1. Start PostgreSQL and the Go API using the parent project instructions (the Go module requires Go 1.26.2). From `memory-engine`: `docker compose up -d postgres`, then `go run ./cmd/server --config configs/config.yaml`.
2. Start Ollama with your installed chat model. If its desktop app is already running, do not start a second server.
3. From this directory, run `node server.mjs`.
4. Open http://127.0.0.1:3000. Open **Connection settings**, select or create a profile, then select your model from the chat toolbar.

The UI can be previewed without either service; it shows their actual connection status. Chat and memory operations require the Go API and database. There are no simulated replies or sample memories.

Optional PowerShell environment overrides before starting:

```powershell
$env:MEMORY_API_URL = "http://127.0.0.1:8080"
$env:OLLAMA_URL = "http://127.0.0.1:11434"
$env:UI_PORT = "3000"
node server.mjs
```

---

## ⚡ OpenAI Drop-In Proxy (`/v1/chat/completions`)

You can use Memory Engine as a **drop-in OpenAI replacement** with automatic long-term memory for any external client, including **Cursor**, **Continue.dev**, **Open WebUI**, **LibreChat**, **Cline**, or Python scripts.

* **Base URL:** `http://127.0.0.1:3000/v1`
* **API Key:** Any string (e.g., `sk-memory-engine` or your user UUID)
* **Custom Headers (Optional):**
  * `X-User-Id: <uuid>` (links memory retrieval directly to a specific profile)
  * `X-Namespace: CODING` (retrieves from specific workspaces like `CODING`, `WORK`, `PERSONAL`)

### Example curl request

```bash
curl http://127.0.0.1:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test" \
  -d '{
    "model": "llama3.2",
    "messages": [
      {"role": "user", "content": "What is my tech stack and preferences?"}
    ],
    "stream": false
  }'
```

The proxy automatically retrieves matching memories from PostgreSQL/pgvector, injects them into the prompt, and returns the response in standard OpenAI JSON or SSE stream format.

---

## Memory behavior

- **Auto-save facts** is on by default. After a completed reply, the selected model extracts up to three durable personal facts from your message with structured output. Each fact must have an exact supporting quote. Facts are saved in PostgreSQL with the original message ID, and embedded when a real embedding provider is available. The model can still make extraction errors: review facts in **Memory library**, or turn automatic saving off. Assistant answers are not automatically learned. **Add a memory** and **Save as memory** remain available.
- Before generation, the bridge retrieves up to five active memories and up to three relevant older (`STALE`) memories and supplies them to Ollama along with up to twenty recent messages. Archived, deleted and expired memories are excluded. Context is capped at eight memories of 750 characters each (approximately 1,500 tokens; not a tokenizer guarantee). General recall questions with no search hits receive a bounded profile recap. The right panel displays the retrieved context. The model is asked to cite memories as `[1]`, `[2]`, etc.; citation correctness depends on the model.
- With the backend's default `null-embedder`, the bridge uses real PostgreSQL keyword search and avoids pseudorandom vectors.
- For semantic retrieval, install the embedding model configured in `configs/config.yaml`, change `embeddings.provider` to `ollama`, and restart the Go API. New UI memories are embedded immediately. Existing memories need re-embedding with the real provider; use the existing embedding endpoint or worker queue. The chat model and embedding model are separate.
- Turning memory off disables retrieval and automatic fact saving for that reply. Conversation history is still sent.
- Completed replies are saved before optional memory extraction. Stopping generation cancels the upstream request; partial replies are not saved. Stopping the memory check after completion preserves the saved reply. A user message may already have been saved before an interrupted or failed generation.
- Chat history and facts are stored in PostgreSQL and survive restarts. Browser local storage contains profile, namespace, model selection, automatic-saving preference and the last conversation ID. The last chat reopens automatically. Model changes share the same profile memories. The sidebar shows the latest 100 conversations; the memory library is paginated and includes an **Older memories** filter.
- Each completed reply has a **Memory map**: question → retrieved facts → reply, plus links to original messages for auto-saved facts. Map records are stored in `ui/data/traces/` and restored on restart. They contain IDs only; text is fetched from the database, so forgotten facts are not exposed from snapshots. Older replies created before this feature have no saved map. Retrieval means the fact was supplied, not proof that the model used it.

## Returning after weeks or months

Start PostgreSQL, the Go server, Ollama, and this UI server again. Use the same profile and workspace. Keep the PostgreSQL data volume and `ui/data/traces/`; do not run `docker compose down -v` if you want to preserve your database. If browser storage was cleared, select your existing profile in Connection settings. Memory records created here have no expiry by default. Keyword recall is weaker than semantic recall for differently phrased questions; configure a real embedding model for better matching.

The model picker supports search, arrow keys, Enter and Escape and remembers your choice. This is a web app styled like a personal browser assistant, not a Chrome extension.

## Architecture and limits

Browser → local Node bridge → Go Memory Engine (retrieval and persistence) + Ollama (streaming generation).

The bridge binds to loopback and rejects cross-origin browser requests. This remains a single-user local development application: the existing API has no authentication. Do not expose either server publicly. Selecting profiles is not authentication. Model output is rendered as text, preventing generated HTML from executing.

The Go API must be available even with memory toggled off because it stores conversation history. This UI does not repair unrelated existing backend bugs or provide document ingestion. History is limited by message count/length, not a model-specific tokenizer; smaller model context windows may require lowering these limits.

## Verify

`node --test` runs bridge tests against mock Memory Engine and Ollama responses, plus real disk persistence tests for memory maps and a bridge restart scenario. Live end-to-end verification additionally requires PostgreSQL, the Go API, and an installed Ollama chat model.

API references: https://docs.ollama.com/api/chat and https://docs.ollama.com/api/tags.
