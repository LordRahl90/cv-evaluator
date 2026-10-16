package telemetry

import (
	"errors"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// Attributes on database spans.
const (
	AttrDBSystem       = "db.system"
	AttrDBOperation    = "db.operation"
	AttrDBTable        = "db.sql.table"
	AttrDBStatement    = "db.statement"
	AttrDBRowsAffected = "db.rows_affected"
	AttrDBNotFound     = "db.record_not_found"

	dbSystemPostgres = "postgresql"
)

// GormOption configures NewGormPlugin.
type GormOption func(*gormPlugin)

// WithDBStatement records each statement's SQL text, with placeholders and
// never with values, as db.statement. It is off by default: even without
// values, statements make spans much larger.
func WithDBStatement(enabled bool) GormOption {
	return func(p *gormPlugin) { p.statement = enabled }
}

// WithRootDBSpans makes statements run outside any trace start their own
// trace. It is off by default, so migrations and the claim queries of idle
// pipeline workers (several per second) do not each produce a trace.
func WithRootDBSpans(enabled bool) GormOption {
	return func(p *gormPlugin) { p.rootSpans = enabled }
}

// NewGormPlugin returns a GORM plugin that wraps every statement (create,
// query, update, delete, row and raw) in a client span named
// "gorm.<operation> <table>", for example "gorm.query users", with
// db.system=postgresql, db.operation, db.sql.table and db.rows_affected.
// Statement values are never recorded (see WithDBStatement).
//
// A failing statement records its error on its own span (an Error status
// and one exception event) and returns it unchanged; the spans of the code
// that called it stay neutral (see Start). gorm.ErrRecordNotFound is not
// treated as a failure: it sets db.record_not_found instead.
//
// Register it once with db.Use(telemetry.NewGormPlugin()).
func NewGormPlugin(opts ...GormOption) gorm.Plugin {
	p := &gormPlugin{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type gormPlugin struct {
	statement bool
	rootSpans bool
}

const gormSpanKey = "cv-evaluator:otel-span"

func (p *gormPlugin) Name() string { return "cv-evaluator:otel" }

func (p *gormPlugin) Initialize(db *gorm.DB) error {
	cb := db.Callback()
	hooks := []struct {
		op     string
		before func(string, func(*gorm.DB)) error
		after  func(string, func(*gorm.DB)) error
	}{
		{"create", cb.Create().Before("gorm:create").Register, cb.Create().After("gorm:create").Register},
		{"query", cb.Query().Before("gorm:query").Register, cb.Query().After("gorm:query").Register},
		{"update", cb.Update().Before("gorm:update").Register, cb.Update().After("gorm:update").Register},
		{"delete", cb.Delete().Before("gorm:delete").Register, cb.Delete().After("gorm:delete").Register},
		{"row", cb.Row().Before("gorm:row").Register, cb.Row().After("gorm:row").Register},
		{"raw", cb.Raw().Before("gorm:raw").Register, cb.Raw().After("gorm:raw").Register},
	}
	for _, h := range hooks {
		if err := h.before("otel:before_"+h.op, p.before(h.op)); err != nil {
			return err
		}
		if err := h.after("otel:after_"+h.op, p.after(h.op)); err != nil {
			return err
		}
	}
	return nil
}

func (p *gormPlugin) before(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx := db.Statement.Context
		if ctx == nil || (!p.rootSpans && !trace.SpanContextFromContext(ctx).IsValid()) {
			return
		}
		// The table is known only after the statement is built; the span is
		// renamed in after.
		_, span := tracer().Start(ctx, "gorm."+op, trace.WithSpanKind(trace.SpanKindClient))
		db.InstanceSet(gormSpanKey, span)
	}
}

func (p *gormPlugin) after(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		v, ok := db.InstanceGet(gormSpanKey)
		if !ok {
			return
		}
		span, ok := v.(trace.Span)
		if !ok {
			return
		}
		defer span.End()

		sql := db.Statement.SQL.String()
		operation, table := op, tableName(db.Statement.Table)
		if op == "raw" || op == "row" {
			if verb, t := parseSQL(sql); verb != "" {
				operation = verb
				if table == "" {
					table = t
				}
			}
		}
		name := "gorm." + operation
		if table != "" {
			name += " " + table
		}
		span.SetName(name)

		attrs := []attribute.KeyValue{
			attribute.String(AttrDBSystem, dbSystemPostgres),
			attribute.String(AttrDBOperation, operation),
		}
		if n := db.Statement.RowsAffected; n >= 0 { // -1 when rows are streamed
			attrs = append(attrs, attribute.Int64(AttrDBRowsAffected, n))
		}
		if table != "" {
			attrs = append(attrs, attribute.String(AttrDBTable, table))
		}
		if p.statement && sql != "" {
			attrs = append(attrs, attribute.String(AttrDBStatement, sql))
		}
		span.SetAttributes(attrs...)

		switch err := db.Error; {
		case err == nil:
		case errors.Is(err, gorm.ErrRecordNotFound):
			span.SetAttributes(attribute.Bool(AttrDBNotFound, true))
		default:
			// The statement is where the error happens: it is recorded here,
			// and the error is returned to the caller unchanged.
			recordError(span, err)
		}
	}
}

// tableName drops an alias and quotes from a table expression
// ("search_results sr" becomes "search_results").
func tableName(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], "\"`")
}

// parseSQL returns the lowercased verb and the table of a raw statement
// ("update", "jobs" for "UPDATE jobs SET ..."), or "" for what it cannot
// tell. It looks only at the statement's leading keywords.
func parseSQL(sql string) (verb, table string) {
	words := strings.Fields(strings.ToLower(sql))
	if len(words) == 0 {
		return "", ""
	}
	verb = strings.Trim(words[0], "(")
	after := func(keyword string) string {
		for i, w := range words[:len(words)-1] {
			if w == keyword {
				return tableName(strings.Trim(words[i+1], "();,"))
			}
		}
		return ""
	}
	switch verb {
	case "update":
		table = after("update")
	case "insert":
		table = after("into")
	case "delete", "select":
		table = after("from")
	}
	return verb, table
}
