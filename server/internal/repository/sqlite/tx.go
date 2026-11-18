package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// TxRepository выполняет операции репозиториев в общей транзакции SQLite.
type TxRepository struct {
	db *Database
}

// NewTxRepository создаёт репозиторий, использующий db.
func NewTxRepository(db *Database) *TxRepository {
	return &TxRepository{
		db: db,
	}
}

type txKey struct{}

// withTx возвращает производный контекст, содержащий транзакцию tx.
func withTx(ctx context.Context, tx *sqlx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// txFromContext возвращает транзакцию из ctx или nil, если она не задана.
func txFromContext(ctx context.Context) *sqlx.Tx {
	if tx, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return tx
	}
	return nil
}

// WithTransaction выполняет fn в собственной транзакции.
// Передаёт fn контекст транзакции для вызовов репозиториев.
// Если ctx уже содержит транзакцию, возвращает ошибку, не вызывая fn.
// При ошибке fn выполняет попытку отката; ошибка отката
// может быть присоединена к исходной через errors.Join.
// При панике выполняет попытку отката, не перехватывая панику.
// Возвращает nil только после успешной фиксации транзакции.
func (r *TxRepository) WithTransaction(
	ctx context.Context,
	fn func(ctx context.Context) error,
) error {
	if txFromContext(ctx) != nil {
		return errors.New("begin transaction: an existing transaction is not supported")
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return wrapDBError("begin transaction", err)
	}

	// Страховочный откат, в том числе при панике.
	// После commit или явного rollback вернёт sql.ErrTxDone.
	defer func() {
		_ = tx.Rollback()
	}()

	if err := fn(withTx(ctx, tx)); err != nil {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(
				err,
				wrapDBError("rollback transaction", rollbackErr),
			)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return wrapDBError("commit transaction", err)
	}

	return nil
}
