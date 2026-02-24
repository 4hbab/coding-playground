// ===================================================================
// snippets.js — Local Storage Snippet Management
// ===================================================================
// This module handles saving/loading/deleting code snippets using
// the browser's localStorage API.
//
// localStorage:
// - Key-value store in the browser (max ~5-10MB depending on browser)
// - Data persists across page reloads and browser restarts
// - Synchronous API (fast for small data)
// - Scoped to the origin (protocol + domain + port)
//
// We store all snippets under a single key as a JSON array.
// ===================================================================

const STORAGE_KEY = 'pyplayground_snippets';

/**
 * Get all saved snippets from localStorage.
 * @returns {Array<{id: string, name: string, code: string, createdAt: string, updatedAt: string}>}
 */
function getSnippets() {
    try {
        const data = localStorage.getItem(STORAGE_KEY);
        return data ? JSON.parse(data) : [];
    } catch (err) {
        console.error('Failed to read snippets:', err);
        return [];
    }
}

/**
 * Save a snippet to localStorage.
 * If a snippet with the same name exists, it will be updated.
 *
 * @param {string} name - The snippet name
 * @param {string} code - The Python code
 * @returns {{success: boolean, error?: string}}
 */
function saveSnippet(name, code) {
    try {
        if (!name || !name.trim()) {
            return { success: false, error: 'Snippet name cannot be empty' };
        }

        const snippets = getSnippets();
        const now = new Date().toISOString();

        // Check if snippet with this name already exists
        const existing = snippets.findIndex(s => s.name === name.trim());

        if (existing >= 0) {
            // Update existing snippet
            snippets[existing].code = code;
            snippets[existing].updatedAt = now;
        } else {
            // Create new snippet with a unique ID
            snippets.push({
                id: generateId(),
                name: name.trim(),
                code: code,
                createdAt: now,
                updatedAt: now,
            });
        }

        localStorage.setItem(STORAGE_KEY, JSON.stringify(snippets));
        return { success: true };
    } catch (err) {
        // Handle localStorage quota exceeded
        if (err.name === 'QuotaExceededError' || err.code === 22) {
            return { success: false, error: 'Storage is full! Please delete some snippets to free up space.' };
        }
        return { success: false, error: `Failed to save: ${err.message}` };
    }
}

/**
 * Load a snippet by its ID.
 * @param {string} id - The snippet ID
 * @returns {object|null}
 */
function loadSnippet(id) {
    const snippets = getSnippets();
    return snippets.find(s => s.id === id) || null;
}

/**
 * Delete a snippet by its ID.
 * @param {string} id - The snippet ID
 * @returns {{success: boolean, error?: string}}
 */
function deleteSnippet(id) {
    try {
        let snippets = getSnippets();
        const before = snippets.length;
        snippets = snippets.filter(s => s.id !== id);

        if (snippets.length === before) {
            return { success: false, error: 'Snippet not found' };
        }

        localStorage.setItem(STORAGE_KEY, JSON.stringify(snippets));
        return { success: true };
    } catch (err) {
        return { success: false, error: `Failed to delete: ${err.message}` };
    }
}

/**
 * Generate a short unique ID.
 * Uses a combination of timestamp and random characters.
 * @returns {string}
 */
function generateId() {
    return Date.now().toString(36) + Math.random().toString(36).substr(2, 5);
}
