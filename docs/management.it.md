# Console di gestione e operazioni

[English](management.md) | [简体中文](management.zh-CN.md) | [Tiếng Việt](management.vi.md) | Italiano

Questo fork usa solo il canale gratuito anonimo di OpenCode Zen e serve tutto su **un'unica porta 8080**. Guida completa in inglese/cinese: [management.md](management.md), [management.zh-CN.md](management.zh-CN.md).

| Percorso | Scopo |
| --- | --- |
| `/v1/*` | API di inferenza. |
| `/web` | WebUI di gestione. |
| `/api/*` | API JSON di gestione. |

Non esistono più chiavi upstream, OAuth Go/Zen, quote o rotazione modelli; i campi vecchi nei file di configurazione vengono ignorati e rimossi al salvataggio.

## Avvio

```bash
cp config.example.json config.json
# modifica server_keys e webui.password
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-modules"
go build -trimpath -o opencode2api ./cmd/opencode2api
./opencode2api -config config.json
```

Servono `server_keys` (chiave locale) e `webui.password` (≥ 10 caratteri). `anonymous` è sempre `true`; le uscite provengono da `proxies`, `proxyfile` o `warp`. Per il solo uso locale imposta `listen: "127.0.0.1:8080"`. Al primo avvio la password viene convertita in hash Argon2id.

## Accesso e pagine

Apri `http://127.0.0.1:8080/web`. Accedi con `webui.username`/`webui.password`; la sessione usa un cookie `HttpOnly`, `SameSite=Strict`. Ogni API di gestione (tranne il login) richiede una sessione; le scritture richiedono `X-CSRF-Token` fornito dal login/sessione. Le `server_keys` non concedono accesso amministrativo.

Pagine: Panoramica, Diagnostica (rotte modelli, Playground, log live), Guida client, Gestione API key locali, Configurazione, Sicurezza account. `#availability` apre Panoramica; `#playground`/`#logs` aprono Diagnostica.

## Salvataggio configurazione

`GET /api/config` restituisce configurazione attiva, ID/valori mascherati e indirizzo effettivo. `PUT /api/config` accetta il **form completo**, non una PATCH; per conservare una chiave/proxy invia `{"id":"FINGERPRINT"}`, per un nuovo valore `{"value":"..."}`. Il salvataggio valida, costruisce il runtime, persiste e passa al nuovo; in caso di errore il runtime precedente continua. `listen` e `webui.enabled` richiedono riavvio. Dopo modifiche manuali al file usa `POST /api/config/reload`.

`POST /api/config/reveal` richiede sessione, CSRF e nuova verifica password; restituisce solo `server_keys` e proxy, mai password o hash.

## Modelli gratuiti

Solo i modelli gratuiti vengono instradati (ID contenente `free` o costo input/output zero). La disponibilità si apprende dal traffico reale; solo errori espliciti di modello inesistente/ritirato lo disabilitano. Abilita `models.active_probes` per test attivi (consumano quota anonima).

| Endpoint | Scopo |
| --- | --- |
| `GET /api/models/availability` | Stato dei modelli gratuiti. |
| `POST /api/models/probe` | Marca come dovuti e sveglia lo scheduler. |
| `POST /api/models/restore` | `{"model":"ID"}` ripristina e passa a controllo manuale. |
| `POST /api/models/manual` | `{"model":"ID","manual":true|false}`. |

## API key locali

`GET/POST /api/api-keys`, `PATCH/DELETE /api/api-keys/{id}`. La creazione restituisce la chiave completa una sola volta; gli elenchi mostrano fingerprint, nome, valore mascherato e stato. Deve restare almeno una chiave attiva.

## Proxy e WARP

`POST /api/proxies/check` richiede sessione + CSRF, con `{"id":"..."}` o `{"value":"..."}`. Timeout 20 secondi; chiama `upstream.anonymous` + `/v1/models` senza autenticazione, senza inferenza e senza modificare lo stato di produzione. Stati: `available`, `unavailable`, `limited`, `rejected`, `inconclusive`; solo `unavailable` prova un guasto.

Più uscite sono la leva principale di affidabilità. WARP è disattivato per default; richiede UDP 2408 verso Cloudflare, senza TUN. `warp.pool_size` 1–32 (default 2), `warp.stack` `ipv6`/`ipv4`/`dual`.

## Playground, log, monitoraggio

Playground chiama `POST /api/debug/inference` nel processo, non streaming, solo `{"protocol":"chat","key":{"mode":"auto"},"request":{...}}`; `relay` può fissare un proxy.

Log: `GET /api/logs?after=SEQ&limit=N` e `GET /api/logs/stream` (SSE con `Last-Event-ID`). Monitoraggio: `GET /api/monitor`, `GET /api/debug/models`. I token provengono solo dai dati upstream.

## Salute, dati, sicurezza

`GET /healthz` usa la stessa porta e non richiede autenticazione; 503 con catalogo vuoto/non pronto, WARP senza uscite o proxy richiesti non sani. Dati sensibili: `config.json`, `config.json.bak`, `*.availability.json`; sessioni, log e statistiche vivono in memoria e si azzerano al riavvio.

Usa loopback in locale; l'accesso di rete richiede HTTPS e restrizioni. Se pubblichi il servizio dietro un reverse proxy, elenca l'indirizzo del proxy in `webui.trusted_proxies` (IP o CIDR): i rate limit useranno l'IP reale del client (la voce più a destra di `X-Forwarded-For`) e `X-Forwarded-Proto` sarà accettato solo da quel proxy. Non allegare configurazione, cookie o contenuti di inferenza reali nelle segnalazioni.

## Verifica

```bash
go vet ./...
go build -trimpath -o opencode2api ./cmd/opencode2api
```

Accettazione: `/web` effettua il login, `/healthz` è pronto, `/v1/models` risponde con la chiave locale, non esiste la porta 8081 e un'inferenza reale attraverso proxy/WARP riesce.
