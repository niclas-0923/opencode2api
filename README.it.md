# opencode2api (fork: solo canale gratuito anonimo)

[English](README.md) | [简体中文](README.zh-CN.md) | [Tiếng Việt](README.vi.md) | Italiano

Gateway Go che traduce tra OpenAI Chat Completions, OpenAI Responses e Anthropic Messages, usando **solo il canale gratuito anonimo** di OpenCode Zen. Nessuna API key upstream, nessun OAuth, nessun piano Go o percorso a pagamento. Servizio, API di inferenza, WebUI e API di gestione condividono **un'unica porta 8080**:

| Percorso | Scopo |
| --- | --- |
| `/v1/*` | API di inferenza compatibile OpenAI/Anthropic. |
| `/web` | WebUI di gestione inclusa nell'eseguibile. |
| `/api/*` | API JSON di gestione (sessione + CSRF). |

Documentazione completa: [README cinese](README.zh-CN.md), [guida alla gestione](docs/management.it.md).

## Avvio rapido

Serve **Go 1.24 o superiore**, oppure un binario della release/Docker.

```bash
if [ ! -f config.json ]; then
  cp config.example.json config.json
fi
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-modules"
go build -trimpath -o opencode2api ./cmd/opencode2api
```

Prima dell'avvio modifica `config.json`:

1. Sostituisci `server_keys` con la tua chiave locale (i client usano questa, non una chiave upstream).
2. Imposta `webui.password` (almeno 10 caratteri) e, se vuoi, `webui.username`.
3. Non servono `zen_keys`/`go_keys`; `anonymous` è sempre `true`.
4. Configura più uscite con `proxies`, `proxyfile` o attivando `warp` per aumentare le probabilità di successo.

```bash
./opencode2api -config config.json
```

Apri `http://127.0.0.1:8080/web` per il login amministrativo e usa `http://127.0.0.1:8080/v1` come base URL nei client. Windows: compila `opencode2api.exe` ed esegui `.\opencode2api.exe -config .\config.json`.

## Configurazione principale

| Chiave | Significato |
| --- | --- |
| `listen` | Unico indirizzo per API + WebUI + gestione, predefinito `0.0.0.0:8080`; usa `127.0.0.1:8080` per il solo uso locale. |
| `server_keys` | Chiavi locali con cui i client accedono al gateway. |
| `anonymous` | Sempre `true`; imposto dal backend, i valori nei vecchi file vengono ignorati. |
| `proxies`, `proxyfile` | Proxy HTTP/SOCKS per le uscite anonime. |
| `upstream.anonymous` | URL del canale gratuito, predefinito `https://opencode.ai/inference`. |
| `warp.enabled`, `warp.pool_size`, `warp.stack` | Pool di uscite Cloudflare WARP nel processo (1–32, predefinito 2; `ipv6`). |
| `models.free_only` | Espone solo modelli gratuiti (consigliato `true`). |
| `models.active_probes` | Test attivi (predefinito `false`), possono consumare quota anonima. |
| `webui.enabled`, `webui.username`, `webui.password` | Abilitazione WebUI e account amministratore. |

I vecchi campi `zen_keys`, `go_keys`, `go_oauth`, `zen_oauth`, `prefer`, `upstream.zen`, `upstream.go` restano caricabili per compatibilità ma vengono ignorati e rimossi al salvataggio. `webui.listen` non è più usato.

## Chiamare l'API

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer $LOCAL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Ciao"}]}'
```

Tre protocolli condividono base URL e chiave locale: `/v1/chat/completions`, `/v1/responses`, `/v1/messages`. Lo streaming SSE è supportato. `GET /v1/models` elenca i modelli gratuiti disponibili.

Tutta l'inferenza passa dal canale anonimo; quando un'uscita è limitata, il gateway ne sceglie un'altra (proxy/WARP) in base allo stato di salute. Non esistono ripieghi su chiavi, OAuth o piani a pagamento.

## Docker

```bash
docker build -t opencode2api .
docker run --rm -p 127.0.0.1:8080:8080 \
  -v "$PWD/config.json:/data/config.json" \
  opencode2api
```

Si pubblica solo la porta 8080. Variabili: `OPENCODE2API_PORT` (predefinita 8080) e `OPENCODE2API_LISTEN` (opzionale). `OPENCODE2API_WEBUI_PORT` e `WEBUI_LISTEN` non esistono più.

## Salute e sviluppo

`GET /healthz` non richiede autenticazione: restituisce 503 se il catalogo non è pronto, WARP non ha uscite attive o i proxy richiesti non hanno nodi sani; 200 (possibilmente `degraded`) quando è utilizzabile.

```bash
go vet ./...
go build -trimpath -o opencode2api ./cmd/opencode2api
for file in webui/js/*.js webui/js/pages/*.js; do
  node --input-type=module --check < "$file" || exit 1
done
```

Non committare `config.json`, backup o file di stato: contengono chiavi locali e password dei proxy.
