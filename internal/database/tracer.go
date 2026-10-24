package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
)

// queryTracer implements pgx.QueryTracer to log slow queries.
type queryTracer struct {
	logger zerolog.Logger
}

type traceKey struct{}

type traceData struct {
	sql       string
	args      []any
	startTime time.Time
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, traceKey{}, traceData{
		sql:       data.SQL,
		args:      data.Args,
		startTime: time.Now(),
	})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	td, ok := ctx.Value(traceKey{}).(traceData)
	if !ok {
		return
	}

	elapsed := time.Since(td.startTime)

	evt := t.logger.Debug()
	if elapsed > 500*time.Millisecond {
		// Slow query warning — log at warn level.
		evt = t.logger.Warn().Str("slow_query", "true")
	}

	evt.
		Str("sql", td.sql).
		Dur("duration_ms", elapsed).
		Err(data.Err).
		Msg("query executed")
}
