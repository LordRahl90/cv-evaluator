# Postgres test helper

Reusable Postgres testcontainer helper for Go tests.

## What it provides

- pgvector-enabled Postgres container (`pgvector/pgvector:pg16` by default)
- auto-creates requested extensions (`vector` by default)
- raw SQL connection helper
- GORM connection helper
- `testing.TB` convenience startup with automatic cleanup

## Example

```go
func TestSomething(t *testing.T) {
    ctx := context.Background()
    db := postgres.MustRun(ctx, t)

    gormDB, err := db.OpenGorm(nil)
    require.NoError(t, err)

    // use gormDB in your test
    _ = gormDB
}
```

