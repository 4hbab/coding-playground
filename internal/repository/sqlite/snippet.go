package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/xid"
	"github.com/sakif/coding-playground/internal/apperror"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// COMPILE-TIME INTERFACE CHECK:
// This line verifies AT COMPILE TIME that *DB implements repository.SnippetRepository.
//
// How it works:
//   - `var _ X = (*Y)(nil)` creates a nil pointer of type *Y
//   - It assigns it to a variable of type X (the interface)
//   - If *Y doesn't implement X, the compiler errors immediately
//   - The `_` means we don't actually use the variable — it's just a check
//
// Without this, you'd only discover a missing method when you try to pass
// *DB to something that expects SnippetRepository — which could be much later.
// This is a Go best practice for any interface implementation.
var _ repository.SnippetRepository = (*DB)(nil)

// Create inserts a new snippet into the database.
//
// KEY CONCEPTS:
//
// 1. ID GENERATION WITH xid:
//    xid generates globally unique IDs that are:
//    - 20 chars, URL-safe (no special characters)
//    - Sortable by creation time (they start with a timestamp)
//    - Example: "cv37rs3pp9olc6atsptg"
//    Compare to UUID (36 chars, with dashes): "550e8400-e29b-41d4-a716-446655440000"
//
// 2. POINTER RECEIVER (*model.Snippet):
//    We take a pointer so we can MODIFY the original struct.
//    After Create(), the caller's snippet has the generated ID and timestamps.
//    If we took a value (model.Snippet), changes would be lost.
//
// 3. ExecContext vs QueryContext:
//    - ExecContext: for INSERT, UPDATE, DELETE (no rows returned)
//    - QueryContext: for SELECT (rows returned)
//    Both accept context as the first arg for cancellation support.
//
// 4. PARAMETERIZED QUERIES (the ? placeholders):
//    NEVER build SQL strings with fmt.Sprintf or string concatenation!
//    That creates SQL injection vulnerabilities:
//      BAD:  "WHERE id = '" + userInput + "'"   ← attacker sends: ' OR 1=1 --
//      GOOD: "WHERE id = ?", userInput           ← driver safely escapes the value
func (db *DB) Create(ctx context.Context, snippet *model.Snippet) error {
	// Generate a unique ID for this snippet
	snippet.ID = xid.New().String()

	// Set timestamps
	now := time.Now()
	snippet.CreatedAt = now
	snippet.UpdatedAt = now

	// INSERT the snippet into the database.
	// The ? placeholders are filled in order by the arguments after the SQL string.
	// The driver handles escaping to prevent SQL injection.
	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO snippets (id, name, code, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		snippet.ID,
		snippet.Name,
		snippet.Code,
		snippet.Description,
		snippet.CreatedAt,
		snippet.UpdatedAt,
	)
	if err != nil {
		// ERROR WRAPPING:
		// fmt.Errorf("context: %w", err) wraps the original error.
		// The %w verb (not %v!) preserves the error chain so callers can use
		// errors.Is() to check the underlying cause.
		// The prefix "sqlite: creating snippet:" tells us WHERE the error happened
		// when reading logs.
		return fmt.Errorf("sqlite: creating snippet: %w", err)
	}

	return nil
}

// GetByID retrieves a single snippet by its ID.
//
// KEY CONCEPTS:
//
// 1. QueryRowContext:
//    Use this when you expect EXACTLY ONE row (or zero rows).
//    It returns a *sql.Row which you then .Scan() into Go variables.
//    If the query returns no rows, Scan() returns sql.ErrNoRows.
//
// 2. .Scan() — THE BRIDGE BETWEEN SQL AND GO:
//    Scan reads column values into Go variables. You MUST:
//    - Pass pointers (&snippet.ID, not snippet.ID)
//    - Match the ORDER of columns in your SELECT statement
//    - Match the TYPES (TEXT→string, DATETIME→time.Time, INTEGER→int)
//
// 3. sql.ErrNoRows:
//    This is NOT really an error — it just means "no matching row exists."
//    We translate it to our app's NotFound error so the handler knows to return 404.
//    This is a common pattern: translate database errors into domain errors.
func (db *DB) GetByID(ctx context.Context, id string) (*model.Snippet, error) {
	var snippet model.Snippet

	// QueryRowContext runs a SELECT and returns at most one row.
	// The Scan() call reads column values into our struct fields.
	err := db.conn.QueryRowContext(ctx,
		`SELECT id, name, code, description, created_at, updated_at
		 FROM snippets
		 WHERE id = ?`,
		id,
	).Scan(
		&snippet.ID,
		&snippet.Name,
		&snippet.Code,
		&snippet.Description,
		&snippet.CreatedAt,
		&snippet.UpdatedAt,
	)

	if err != nil {
		// CHECK FOR "NOT FOUND":
		// sql.ErrNoRows is a sentinel error — we check with ==
		// (not errors.Is, because database/sql doesn't wrap it)
		if err == sql.ErrNoRows {
			return nil, apperror.NotFound("snippet", id)
		}
		// Any other error is a real database problem
		return nil, fmt.Errorf("sqlite: getting snippet %s: %w", id, err)
	}

	return &snippet, nil
}

// List retrieves multiple snippets with pagination.
//
// KEY CONCEPTS:
//
// 1. QueryContext (not QueryRowContext):
//    Use this when you expect MULTIPLE rows.
//    It returns *sql.Rows — an iterator you loop over with rows.Next().
//
// 2. defer rows.Close() — ABSOLUTELY CRITICAL:
//    sql.Rows holds a database connection from the pool.
//    If you forget to Close(), that connection is never returned to the pool.
//    After enough leaked connections, your app runs out and hangs forever.
//    The defer ensures Close() runs even if your loop panics.
//
// 3. rows.Next() + rows.Scan() pattern:
//    rows.Next() advances to the next row and returns false when done.
//    rows.Scan() reads the current row's values into Go variables.
//    Always check rows.Err() after the loop — it catches errors that
//    happened DURING iteration (network issues, etc.).
//
// 4. LIMIT/OFFSET pagination:
//    LIMIT N = return at most N rows
//    OFFSET M = skip the first M rows
//    Example: page 3 with 20 items per page → LIMIT 20 OFFSET 40
//    NOTE: OFFSET pagination is simple but slow for large datasets.
//    In Phase 6, you'll upgrade to cursor-based pagination.
func (db *DB) List(ctx context.Context, opts repository.ListOptions) ([]model.Snippet, error) {
	// Apply defaults if not specified
	limit := opts.Limit
	if limit <= 0 {
		limit = 20 // Default page size
	}
	if limit > 100 {
		limit = 100 // Maximum page size — prevent fetching entire DB
	}

	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	// ORDER BY created_at DESC = newest first
	rows, err := db.conn.QueryContext(ctx,
		`SELECT id, name, code, description, created_at, updated_at
		 FROM snippets
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing snippets: %w", err)
	}
	// CRITICAL: always close rows when done!
	defer rows.Close()

	// PRE-ALLOCATE THE SLICE:
	// make([]model.Snippet, 0, limit) creates a slice with:
	//   - length 0 (no elements yet)
	//   - capacity `limit` (pre-allocated memory for up to `limit` elements)
	// This avoids repeated memory allocations as we append in the loop.
	// Without the capacity hint, Go would double the slice size each time
	// it runs out of space (1→2→4→8→16...), wasting memory and CPU.
	snippets := make([]model.Snippet, 0, limit)

	for rows.Next() {
		var s model.Snippet
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Code, &s.Description,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("sqlite: scanning snippet row: %w", err)
		}
		snippets = append(snippets, s)
	}

	// CHECK FOR ITERATION ERRORS:
	// rows.Err() returns any error that occurred during Next() calls.
	// This catches things like the database connection dropping mid-iteration.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterating snippets: %w", err)
	}

	return snippets, nil
}

// Update modifies an existing snippet in the database.
//
// KEY CONCEPTS:
//
// 1. CHECKING IF THE ROW EXISTS:
//    ExecContext returns a sql.Result with RowsAffected().
//    If no rows were affected, the snippet doesn't exist → return NotFound.
//    This is more efficient than doing a SELECT + UPDATE (one query vs two).
//
// 2. UPDATING ONLY CHANGED FIELDS:
//    We update name, code, description, and updated_at.
//    We do NOT update id or created_at (those are immutable).
//    updated_at is always set to "now" so we know when it was last modified.
func (db *DB) Update(ctx context.Context, snippet *model.Snippet) error {
	// Set the updated timestamp
	snippet.UpdatedAt = time.Now()

	result, err := db.conn.ExecContext(ctx,
		`UPDATE snippets
		 SET name = ?, code = ?, description = ?, updated_at = ?
		 WHERE id = ?`,
		snippet.Name,
		snippet.Code,
		snippet.Description,
		snippet.UpdatedAt,
		snippet.ID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: updating snippet %s: %w", snippet.ID, err)
	}

	// RowsAffected() tells us how many rows were changed by the UPDATE.
	// If 0 rows were affected, the WHERE clause didn't match anything → not found.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return apperror.NotFound("snippet", snippet.ID)
	}

	return nil
}

// Delete removes a snippet from the database by its ID.
//
// Same pattern as Update — check RowsAffected to detect "not found".
func (db *DB) Delete(ctx context.Context, id string) error {
	result, err := db.conn.ExecContext(ctx,
		`DELETE FROM snippets WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: deleting snippet %s: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return apperror.NotFound("snippet", id)
	}

	return nil
}
