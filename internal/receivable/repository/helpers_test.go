package repository_test

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type contextT = context.Context

func pgxNoRows() error { return pgx.ErrNoRows }
