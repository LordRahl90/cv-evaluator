package telemetry_test

import (
	"context"
	"testing"

	"cv-evaluator/internal/telemetry"
	"cv-evaluator/internal/telemetry/telemetrytest"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type widget struct {
	ID    uint   `gorm:"primaryKey"`
	Email string `gorm:"uniqueIndex"`
}

const secretEmail = "private.person@example.com"

// openTraced opens a database with a widgets table and the plugin
// configured by opts.
func openTraced(t *testing.T, opts ...telemetry.GormOption) (*gorm.DB, string) {
	t.Helper()
	dsn := postgres.MustRun(t).ConnectionString()
	setup, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, setup.AutoMigrate(&widget{}))
	return reopen(t, dsn, opts...), dsn
}

func reopen(t *testing.T, dsn string, opts ...telemetry.GormOption) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.Use(telemetry.NewGormPlugin(opts...)))
	return db
}

func TestGormPlugin(t *testing.T) {
	db, _ := openTraced(t)
	p := telemetrytest.Install(t)

	ctx, end := telemetry.Start(context.Background(), "test.Root")
	require.NoError(t, db.WithContext(ctx).Create(&widget{Email: secretEmail}).Error)
	var w widget
	require.NoError(t, db.WithContext(ctx).Where("email = ?", secretEmail).First(&w).Error)
	require.NoError(t, db.WithContext(ctx).Model(&w).Update("email", "other@example.com").Error)
	err := db.WithContext(ctx).Where("email = ?", "nobody").First(&widget{}).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, db.WithContext(ctx).Exec("DELETE FROM widgets WHERE email = ?", "other@example.com").Error)
	end(nil)

	root := p.Span(t, "test.Root")
	var got []string
	for _, s := range p.SpansOfKind(trace.SpanKindClient) {
		telemetrytest.RequireChild(t, root, s)
		got = append(got, s.Name())
		attrs := telemetrytest.KeyValues(s.Attributes())
		assert.Equal(t, "postgresql", attrs["db.system"])
		assert.NotContains(t, attrs, "db.statement", "statements are off by default")
		for k, v := range attrs {
			assert.NotContains(t, v, "example.com", "%s on %s must not carry values", k, s.Name())
		}
	}
	assert.Equal(t, []string{
		"gorm.create widgets", "gorm.query widgets", "gorm.update widgets", "gorm.query widgets", "gorm.delete widgets",
	}, got)

	insert := p.Span(t, "gorm.create widgets")
	attrs := telemetrytest.KeyValues(insert.Attributes())
	assert.Equal(t, "create", attrs["db.operation"])
	assert.Equal(t, "widgets", attrs["db.sql.table"])
	assert.Equal(t, "1", attrs["db.rows_affected"])

	notFound := p.SpansOfKind(trace.SpanKindClient)[3]
	assert.Equal(t, codes.Unset, notFound.Status().Code, "not found is not a failure")
	assert.Equal(t, "true", telemetrytest.KeyValues(notFound.Attributes())["db.record_not_found"])
}

func TestGormPlugin_StatementWithPlaceholdersOnly(t *testing.T) {
	db, _ := openTraced(t, telemetry.WithDBStatement(true))
	p := telemetrytest.Install(t)

	ctx, end := telemetry.Start(context.Background(), "test.Root")
	require.NoError(t, db.WithContext(ctx).Create(&widget{Email: secretEmail}).Error)
	end(nil)

	stmt := telemetrytest.KeyValues(p.Span(t, "gorm.create widgets").Attributes())["db.statement"]
	assert.Contains(t, stmt, `INSERT INTO "widgets"`)
	assert.Contains(t, stmt, "$1")
	assert.NotContains(t, stmt, secretEmail)
}

func TestGormPlugin_ErrorRecordedOnceOnStatementSpan(t *testing.T) {
	db, _ := openTraced(t)
	p := telemetrytest.Install(t)
	require.NoError(t, db.Create(&widget{Email: secretEmail}).Error)

	create := func(ctx context.Context) (err error) {
		ctx, end := telemetry.Start(ctx, "test.Service.Create")
		defer end(&err)
		return db.WithContext(ctx).Create(&widget{Email: secretEmail}).Error
	}
	ctx, end := telemetry.Start(context.Background(), "test.Handler")
	err := create(ctx)
	require.Error(t, err)
	telemetry.RecordError(ctx, err) // the handler's 5xx path
	end(&err)

	stmt := p.Span(t, "gorm.create widgets")
	assert.Equal(t, 1, telemetrytest.Exceptions(stmt))
	assert.Equal(t, codes.Error, stmt.Status().Code)
	assert.Equal(t, "*pgconn.PgError", stmt.Status().Description)
	for _, msg := range stmt.Events()[0].Attributes {
		assert.NotContains(t, msg.Value.String(), secretEmail, "the database error message carries no values")
	}
	for _, name := range []string{"test.Service.Create", "test.Handler"} {
		s := p.Span(t, name)
		assert.Equal(t, codes.Error, s.Status().Code, name)
		assert.Zero(t, telemetrytest.Exceptions(s), name)
	}
	assert.True(t, telemetry.IsRecorded(err))
}

func TestGormPlugin_NoRootSpansByDefault(t *testing.T) {
	db, dsn := openTraced(t)
	p := telemetrytest.Install(t)

	require.NoError(t, db.WithContext(context.Background()).Create(&widget{Email: "a@b.c"}).Error)
	assert.Empty(t, p.Spans(), "a statement outside any trace does not start one")

	root := reopen(t, dsn, telemetry.WithRootDBSpans(true))
	require.NoError(t, root.Create(&widget{Email: "d@e.f"}).Error)
	assert.Equal(t, []string{"gorm.create widgets"}, p.Names())
}
