// ===================================================================
// editor.js — Monaco Editor Setup
// ===================================================================
// This module initialises Monaco Editor (the same editor engine powered
// by VS Code) and exposes helper functions for the rest of the app.
//
// Monaco is loaded from a CDN via AMD (Asynchronous Module Definition).
// The loader.min.js script we included in base.html sets up `require`
// which we use here to load the full editor.
// ===================================================================

// Module-level variable to hold the editor instance
let editor = null;

// Default Python code shown when the editor loads
const DEFAULT_CODE = `# Welcome to PyPlayground! 🐍
# Write your Python code here and press Ctrl+Enter to run it.

def greet(name):
    """A simple greeting function."""
    return f"Hello, {name}! Welcome to PyPlayground!"

# Try running this:
message = greet("Developer")
print(message)
print()
print("Let's do some math:")
for i in range(1, 6):
    print(f"  {i}² = {i**2}")
`;

/**
 * Initialises the Monaco Editor inside the given container element.
 * Returns a Promise that resolves when the editor is ready.
 *
 * @param {string} containerId - The DOM element ID to mount the editor in
 * @returns {Promise<void>}
 */
function initEditor(containerId) {
    return new Promise((resolve, reject) => {
        // Tell Monaco's AMD loader where to find its files
        require.config({
            paths: {
                vs: 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs'
            }
        });

        // Load the editor module
        require(['vs/editor/editor.main'], function () {
            const container = document.getElementById(containerId);
            if (!container) {
                reject(new Error(`Container #${containerId} not found`));
                return;
            }

            // Hide the loading indicator
            const loading = document.getElementById('editor-loading');
            if (loading) loading.style.display = 'none';

            // Define a custom dark theme matching our CSS
            monaco.editor.defineTheme('pyplayground-dark', {
                base: 'vs-dark',
                inherit: true,
                rules: [
                    { token: 'comment', foreground: '8b949e', fontStyle: 'italic' },
                    { token: 'keyword', foreground: 'ff7b72' },
                    { token: 'string', foreground: 'a5d6ff' },
                    { token: 'number', foreground: '79c0ff' },
                    { token: 'type', foreground: 'ffa657' },
                    { token: 'function', foreground: 'd2a8ff' },
                    { token: 'variable', foreground: 'ffa657' },
                    { token: 'operator', foreground: 'ff7b72' },
                ],
                colors: {
                    'editor.background': '#0d1117',
                    'editor.foreground': '#e6edf3',
                    'editor.lineHighlightBackground': '#161b2280',
                    'editor.selectionBackground': '#264f7840',
                    'editorCursor.foreground': '#58a6ff',
                    'editorLineNumber.foreground': '#484f58',
                    'editorLineNumber.activeForeground': '#8b949e',
                    'editor.inactiveSelectionBackground': '#264f7820',
                    'editorIndentGuide.background1': '#30363d',
                    'editorIndentGuide.activeBackground1': '#484f58',
                }
            });

            // Define a custom light theme
            monaco.editor.defineTheme('pyplayground-light', {
                base: 'vs',
                inherit: true,
                rules: [
                    { token: 'comment', foreground: '6a737d', fontStyle: 'italic' },
                    { token: 'keyword', foreground: 'cf222e' },
                    { token: 'string', foreground: '0a3069' },
                    { token: 'number', foreground: '0550ae' },
                    { token: 'type', foreground: '953800' },
                    { token: 'function', foreground: '8250df' },
                ],
                colors: {
                    'editor.background': '#ffffff',
                    'editor.foreground': '#1f2328',
                    'editor.lineHighlightBackground': '#f6f8fa',
                    'editor.selectionBackground': '#0969da20',
                    'editorCursor.foreground': '#0969da',
                    'editorLineNumber.foreground': '#8b949e',
                    'editorLineNumber.activeForeground': '#1f2328',
                }
            });

            // Create the editor
            const currentTheme = document.documentElement.getAttribute('data-theme');
            editor = monaco.editor.create(container, {
                value: DEFAULT_CODE,
                language: 'python',
                theme: currentTheme === 'light' ? 'pyplayground-light' : 'pyplayground-dark',
                fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Consolas', monospace",
                fontSize: 14,
                lineHeight: 22,
                tabSize: 4,
                insertSpaces: true,
                wordWrap: 'on',
                minimap: { enabled: false },
                scrollBeyondLastLine: false,
                automaticLayout: true,
                padding: { top: 12, bottom: 12 },
                smoothScrolling: true,
                cursorBlinking: 'smooth',
                cursorSmoothCaretAnimation: 'on',
                renderWhitespace: 'selection',
                bracketPairColorization: { enabled: true },
                guides: {
                    bracketPairs: true,
                    indentation: true,
                },
                suggest: {
                    showKeywords: true,
                    showSnippets: true,
                },
            });

            resolve();
        });
    });
}

/**
 * Returns the current code from the editor.
 * @returns {string}
 */
function getEditorCode() {
    return editor ? editor.getValue() : '';
}

/**
 * Sets the code in the editor.
 * @param {string} code
 */
function setEditorCode(code) {
    if (editor) {
        editor.setValue(code);
    }
}

/**
 * Returns the Monaco editor instance (for advanced operations).
 * @returns {object|null}
 */
function getEditorInstance() {
    return editor;
}

/**
 * Switches the editor theme.
 * @param {'dark'|'light'} theme
 */
function setEditorTheme(theme) {
    if (editor) {
        monaco.editor.setTheme(
            theme === 'light' ? 'pyplayground-light' : 'pyplayground-dark'
        );
    }
}
