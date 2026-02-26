// ===================================================================
// snippets.js — Server API Snippet Management
// ===================================================================
// This module handles saving/loading/deleting code snippets using
// the Go backend API instead of browser localStorage.
//
// MIGRATION FROM localStorage TO SERVER API:
// Before: snippets were stored in the browser's localStorage.
//   - Data was local only — different browsers/devices couldn't share
//   - Data could be lost if the user cleared browser data
//   - No collaboration possible
//
// After: snippets are stored in SQLite via the Go server API.
//   - Data persists on the server, survives browser clears
//   - Foundation for multi-user/multi-device access
//   - Same data available from any browser pointed at the server
//
// KEY CHANGE: All functions are now ASYNC.
// localStorage was synchronous (instant, blocking).
// fetch() is asynchronous (returns a Promise, non-blocking).
// Callers must use `await` or `.then()` to get results.
//
// THE fetch() API:
// fetch() is the modern way to make HTTP requests from JavaScript.
//   - Returns a Promise that resolves to a Response object
//   - Response.ok is true for 2xx status codes
//   - Response.json() parses the body as JSON (also returns a Promise!)
//   - By default, fetch() uses GET. Pass { method: 'POST', ... } for others.
// ===================================================================

const API_BASE = '/api';

/**
 * Get all saved snippets from the server.
 *
 * fetch() FLOW:
 * 1. Browser sends GET /api/snippets to the Go server
 * 2. Go handler calls service.List() → repository.List() → SQLite SELECT
 * 3. Server responds with JSON array of snippets
 * 4. We parse the JSON and return the array
 *
 * @returns {Promise<Array>} Array of snippet objects
 */
async function getSnippets() {
    try {
        const response = await fetch(`${API_BASE}/snippets`);

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.message || 'Failed to load snippets');
        }

        return await response.json();
    } catch (err) {
        console.error('Failed to fetch snippets:', err);
        return [];
    }
}

/**
 * Save a new snippet to the server.
 *
 * POST REQUEST:
 * To send data TO the server, we use POST with a JSON body.
 * The fetch() options object specifies:
 *   - method: 'POST' (default is GET)
 *   - headers: tells the server we're sending JSON
 *   - body: the actual JSON data (must be a string, hence JSON.stringify)
 *
 * @param {string} name - The snippet name
 * @param {string} code - The Python code
 * @returns {Promise<{success: boolean, snippet?: object, error?: string}>}
 */
async function saveSnippet(name, code) {
    try {
        if (!name || !name.trim()) {
            return { success: false, error: 'Snippet name cannot be empty' };
        }

        const response = await fetch(`${API_BASE}/snippets`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: name.trim(),
                code: code,
            }),
        });

        if (!response.ok) {
            const error = await response.json();
            return { success: false, error: error.message || 'Failed to save snippet' };
        }

        const snippet = await response.json();
        return { success: true, snippet };
    } catch (err) {
        return { success: false, error: `Failed to save: ${err.message}` };
    }
}

/**
 * Load a snippet by its ID from the server.
 *
 * URL PARAMETERS:
 * We append the ID to the URL: /api/snippets/{id}
 * The Go router extracts {id} using r.PathValue("id")
 *
 * @param {string} id - The snippet ID
 * @returns {Promise<object|null>}
 */
async function loadSnippet(id) {
    try {
        const response = await fetch(`${API_BASE}/snippets/${id}`);

        if (!response.ok) {
            if (response.status === 404) {
                console.warn(`Snippet ${id} not found`);
                return null;
            }
            throw new Error('Failed to load snippet');
        }

        return await response.json();
    } catch (err) {
        console.error('Failed to load snippet:', err);
        return null;
    }
}

/**
 * Delete a snippet by its ID.
 *
 * DELETE METHOD:
 * HTTP DELETE is the standard method for removing resources.
 * On success, the server returns 204 No Content (empty body).
 * We don't try to parse JSON from a 204 response — there is none.
 *
 * @param {string} id - The snippet ID
 * @returns {Promise<{success: boolean, error?: string}>}
 */
async function deleteSnippet(id) {
    try {
        const response = await fetch(`${API_BASE}/snippets/${id}`, {
            method: 'DELETE',
        });

        if (!response.ok) {
            // Don't try to parse body for 204 (no content)
            if (response.status !== 204) {
                const error = await response.json();
                return { success: false, error: error.message || 'Failed to delete snippet' };
            }
        }

        return { success: true };
    } catch (err) {
        return { success: false, error: `Failed to delete: ${err.message}` };
    }
}
