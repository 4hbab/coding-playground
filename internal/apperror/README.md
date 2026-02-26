## Running Tests

Running tests with `go test ./internal/apperror/ -v` give output
```
=== RUN   TestErrorsIs
=== RUN   TestErrorsIs/NotFound_wraps_ErrNotFound
=== RUN   TestErrorsIs/ValidationFailed_wraps_ErrValidation
=== RUN   TestErrorsIs/Conflict_wraps_ErrConflict
=== RUN   TestErrorsIs/NotFound_does_NOT_match_ErrValidation
=== RUN   TestErrorsIs/ValidationFailed_does_NOT_match_ErrNotFound
--- PASS: TestErrorsIs (0.00s)
    --- PASS: TestErrorsIs/NotFound_wraps_ErrNotFound (0.00s)
    --- PASS: TestErrorsIs/ValidationFailed_wraps_ErrValidation (0.00s)
    --- PASS: TestErrorsIs/Conflict_wraps_ErrConflict (0.00s)
    --- PASS: TestErrorsIs/NotFound_does_NOT_match_ErrValidation (0.00s)
    --- PASS: TestErrorsIs/ValidationFailed_does_NOT_match_ErrNotFound (0.00s)
=== RUN   TestErrorMessages
=== RUN   TestErrorMessages/NotFound_message_includes_resource_and_id
=== RUN   TestErrorMessages/ValidationFailed_uses_custom_message
=== RUN   TestErrorMessages/Conflict_message_includes_resource_and_id
--- PASS: TestErrorMessages (0.00s)
    --- PASS: TestErrorMessages/NotFound_message_includes_resource_and_id (0.00s)
    --- PASS: TestErrorMessages/ValidationFailed_uses_custom_message (0.00s)
    --- PASS: TestErrorMessages/Conflict_message_includes_resource_and_id (0.00s)
=== RUN   TestUnwrap
--- PASS: TestUnwrap (0.00s)
=== RUN   TestValidationFailedField
--- PASS: TestValidationFailedField (0.00s)
PASS
ok
```