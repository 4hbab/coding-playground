# PyPlayground HTTP API

This is the specification of every endpoint the Go server exposes, as implemented today.
The browser app in `web/static/js/` is the only client; there is no versioned public API.

- Base URL: the site itself (e.g. `http://localhost:8080`). All paths below are relative to it.
- Request and response bodies are JSON unless stated otherwise.
- Times are RFC 3339 strings. `duration` in execution results is in nanoseconds.

## Authentication

Sign-in uses GitHub OAuth. On success the server sets an **HttpOnly** cookie,
`pyplayground_token`, holding a JWT signed with `JWT_SECRET` (HS256, 1 hour, no refresh:
sign in again when it expires). The cookie is `SameSite=Lax`, and `Secure` when `PUBLIC_URL`
is `https://`. The frontend never sees the token; it calls `GET /api/me` to learn who is signed in.

The `/auth/*` routes exist only when `JWT_SECRET`, `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`
are all set; `GET /api/me` exists whenever `JWT_SECRET` is set.

| Requirement | Meaning |
|---|---|
| none | Works for everyone |
| optional | Works for everyone; a valid cookie identifies the caller |
| required | `401` without a valid cookie |

## Rate limits

Every response carries the caller's current budget (per client IP, fixed one-minute window):

| Header | Meaning |
|---|---|
| `X-RateLimit-Limit` | Requests allowed per window |
| `X-RateLimit-Remaining` | Requests left in this window |
| `X-RateLimit-Reset` | When the window resets (Unix seconds) |

| Routes | Default limit | Setting |
|---|---|---|
| `POST /api/execute`, `/auth/*` | 10 / minute (separate budgets) | `RATE_LIMIT_STRICT` |
| everything else | 300 / minute | `RATE_LIMIT_DEFAULT` |

Over the limit: `429 Too Many Requests` with `Retry-After` (seconds) and
`{"error": "Too many requests. Please try again later."}`.

## Errors

Snippet endpoints return a consistent body:

```json
{ "error": "not_found", "message": "snippet not found with id cv37rs3pp9olc6atsptg" }
```

| Status | `error` | When |
|---|---|---|
| 400 | `invalid_json` | Body isn't valid JSON |
| 400 | `validation_error` | A field breaks a rule (see each endpoint) |
| 404 | `not_found` | No snippet with that ID |
| 500 | `internal_error` | Unexpected failure (details are logged, never returned) |

`POST /api/execute` and the auth routes return a **plain-text** message with the status code
(listed under each endpoint). `429` responses are always JSON (see above).

---

## Pages and health

### `GET /`
The playground page (HTML). Auth: none.

### `GET /static/*`
CSS and JavaScript files. Auth: none.

### `GET /healthz`
Liveness check used by Docker Compose. Auth: none.

`200` → `{"status": "ok"}`

---

## Configuration

### `GET /api/config`
Tells the frontend which features this server offers. Auth: none.

`200`
```json
{ "serverExecution": true }
```

| Field | Type | Meaning |
|---|---|---|
| `serverExecution` | boolean | `POST /api/execute` is available (Docker is running) |

---

## Code execution

### `POST /api/execute`
Runs Python code in a fresh, isolated Docker container. Auth: none. Rate limit: strict.
**Only registered when Docker is available**; otherwise `404`.

Request (at most 64 KB):
```json
{ "code": "print('hello')" }
```

`200` — the program ran (whatever its exit code):
```json
{
  "stdout": "hello\n",
  "stderr": "",
  "exitCode": 0,
  "duration": 92806875,
  "truncated": false
}
```

| Field | Type | Meaning |
|---|---|---|
| `stdout`, `stderr` | string | Program output, at most 64 KB each |
| `exitCode` | integer | `0` success, `1` uncaught exception, `124` time limit, `137` killed (memory limit) |
| `duration` | integer | Wall time in nanoseconds, including waiting for a container |
| `truncated` | boolean | `stdout` or `stderr` went over 64 KB and was cut |

On a timeout or memory kill, `stderr` ends with a line explaining why.

| Status | Body (plain text) | When |
|---|---|---|
| 400 | `invalid request configuration` | Body isn't valid JSON |
| 400 | `code cannot be empty` | `code` is missing or empty |
| 413 | `code is too large` | Body over 64 KB |
| 429 | JSON, see Rate limits | Too many runs from this IP |
| 503 | `all sandboxes are busy, try again in a few seconds` | No container freed up within 5 s; has `Retry-After: 5` |
| 500 | `internal server error during execution` | Docker failed |

The limits applied to every run are described in the README (Sandbox limits).

---

## Snippets

A snippet:

```json
{
  "id": "cv37rs3pp9olc6atsptg",
  "name": "fibonacci",
  "code": "def fib(n): ...",
  "description": "",
  "createdAt": "2026-09-29T18:31:30Z",
  "updatedAt": "2026-09-29T18:31:30Z"
}
```

IDs are [xid](https://github.com/rs/xid) strings (20 characters, sortable by creation time).

**Ownership.** Every snippet route identifies the caller from the session cookie, if any.

- A snippet saved while **signed in** belongs to that user and is private: only they can
  list, read, update or delete it.
- A snippet saved **without an account** has no owner and is shared: anyone can read, update
  or delete it, and editing it never makes it yours.

For anyone else, someone's private snippet behaves exactly like a missing one (`404 not_found`),
so an ID can't be used to find out that a snippet exists. The owner is never included in responses.

### `GET /api/snippets`
Lists snippets, newest first. Auth: optional. Signed in: your own snippets. Not signed in:
the shared snippets (saved without an account).

| Query | Default | Rule |
|---|---|---|
| `limit` | 20 | 1–100 (values outside are clamped) |
| `offset` | 0 | ≥ 0 |

`200` → array of snippets (empty array when there are none).

### `GET /api/snippets/{id}`
One snippet. Auth: optional. `200` → snippet · `404` → `not_found` (missing, or someone else's).

### `POST /api/snippets`
Creates a snippet. Auth: optional. Signed in, the snippet is yours and private; otherwise it is shared.

```json
{ "name": "fibonacci", "code": "def fib(n): ...", "description": "optional" }
```

| Field | Rule |
|---|---|
| `name` | Required, at most 100 characters (surrounding spaces are trimmed) |
| `code` | At most 100,000 characters |
| `description` | Optional |

`201` → the created snippet · `400` → `invalid_json` or `validation_error`.

### `PUT /api/snippets/{id}`
Updates a snippet. Auth: optional. Same body as create; an empty `name` keeps the current
name, while `code` and `description` are always replaced.

`200` → the updated snippet · `400` · `404` (missing, or someone else's).

### `DELETE /api/snippets/{id}`
Deletes a snippet. Auth: optional. `204` (no body) · `404` (missing, or someone else's).

---

## Account

### `GET /auth/github/login`
Starts sign-in. Sets a short-lived `oauth_state` cookie (CSRF protection) and redirects
(`307`) to GitHub. Auth: none. Rate limit: strict.

### `GET /auth/github/callback?code=…&state=…`
GitHub redirects here. The server checks `state` against the cookie, exchanges `code` for the
GitHub profile, creates or updates the user, sets `pyplayground_token`, and redirects (`307`) to `/`.

| Status | Body (plain text) | When |
|---|---|---|
| 400 | `Invalid OAuth state` | Missing cookie or `state` mismatch |
| 400 | `GitHub authentication failed: …` | The user denied access, or GitHub returned an error |
| 400 | `Missing authorization code` | No `code` |
| 500 | `Authentication failed` | Exchange with GitHub or saving the user failed |

### `POST /auth/logout`
Clears the session cookie. Auth: none. `200` → `{"message": "logged out"}`.

### `GET /api/me`
The signed-in user. Auth: required.

`200`
```json
{ "id": "cv3…", "login": "octocat", "email": "octocat@example.com", "avatarUrl": "https://avatars.githubusercontent.com/…" }
```

`401` → `{"error":"authentication required"}`, `{"error":"invalid or expired token"}` or
`{"error":"user not found"}`. `404` when `JWT_SECRET` is not set.
