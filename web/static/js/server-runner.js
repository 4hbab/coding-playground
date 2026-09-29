// ===================================================================
// server-runner.js — Server-side Python Execution
// ===================================================================
// Talks to the Go server, which runs code in an isolated Docker container
// (see internal/executor/docker). This is the alternative to running Python
// in the browser with Pyodide (pyodide-worker.js).
//
// SERVER API:
//   GET  /api/config   → { serverExecution: true|false }
//   POST /api/execute  → { stdout, stderr, exitCode, duration, truncated }
// ===================================================================

/**
 * Ask the server which features are available.
 * Falls back to "no server execution" if the server can't be reached.
 *
 * @returns {Promise<{serverExecution: boolean}>}
 */
async function getServerConfig() {
    try {
        const response = await fetch('/api/config');
        if (!response.ok) return { serverExecution: false };
        return await response.json();
    } catch (err) {
        console.warn('Failed to load server config:', err);
        return { serverExecution: false };
    }
}

/**
 * Run code on the server.
 *
 * Returns { ok: true, result } on success, or { ok: false, error } with a
 * message explaining what went wrong (limits, busy server, network).
 *
 * @param {string} code
 * @returns {Promise<{ok: boolean, result?: object, error?: string}>}
 */
async function executeOnServer(code) {
    let response;
    try {
        response = await fetch('/api/execute', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code: code }),
        });
    } catch (err) {
        return { ok: false, error: 'Could not reach the server. Check your connection and try again.' };
    }

    if (response.ok) {
        return { ok: true, result: await response.json() };
    }

    // The server sends Retry-After with 429 and 503 responses
    const retryAfter = response.headers.get('Retry-After');
    const wait = retryAfter ? `${retryAfter} seconds` : 'a few seconds';

    switch (response.status) {
        case 413:
            return { ok: false, error: 'Your code is too large to run on the server. Try running it in the Browser.' };
        case 429:
            return { ok: false, error: `You've run code on the server too often. Try again in ${wait}, or run it in the Browser.` };
        case 503:
            return { ok: false, error: `All server sandboxes are busy. Try again in ${wait}.` };
        default:
            return { ok: false, error: `The server couldn't run your code (error ${response.status}). Try again, or run it in the Browser.` };
    }
}
