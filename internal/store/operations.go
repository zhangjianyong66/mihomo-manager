package store

import (
	"context"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const operationColumns = `id, profile_id, kind, state, phase, attempt, recovery, error_code, created_at, updated_at`

func (s *Store) CreateOperation(ctx context.Context, operation domain.Operation) error {
	if err := operation.Validate(); err != nil {
		return invalid("create operation", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	_, err = db.ExecContext(ctx, `
		INSERT INTO operations(id, profile_id, kind, state, phase, attempt, recovery, error_code, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		operation.ID.String(), nullableProfileID(operation.ProfileID), operation.Kind, operation.State.String(), operation.Phase,
		operation.Attempt, string(operation.Recovery), nullableText(operation.ErrorCode), formatTime(operation.CreatedAt), formatTime(operation.UpdatedAt),
	)
	return storeError("create operation "+operation.ID.String(), err)
}

func (s *Store) GetOperation(ctx context.Context, id domain.OperationID) (domain.Operation, error) {
	if err := id.Validate(); err != nil {
		return domain.Operation{}, invalid("get operation", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return domain.Operation{}, err
	}
	defer release()
	operation, err := scanOperation(db.QueryRowContext(ctx,
		"SELECT "+operationColumns+" FROM operations WHERE id = ?", id.String(),
	))
	if err != nil {
		return domain.Operation{}, storeError("get operation "+id.String(), err)
	}
	return operation, nil
}

func (s *Store) UpdateOperation(ctx context.Context, operation domain.Operation) error {
	if err := operation.Validate(); err != nil {
		return invalid("update operation", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	result, err := db.ExecContext(ctx, `
		UPDATE operations SET profile_id = ?, kind = ?, state = ?, phase = ?, attempt = ?, recovery = ?, error_code = ?, updated_at = ?
		WHERE id = ?`,
		nullableProfileID(operation.ProfileID), operation.Kind, operation.State.String(), operation.Phase, operation.Attempt,
		string(operation.Recovery), nullableText(operation.ErrorCode), formatTime(operation.UpdatedAt), operation.ID.String(),
	)
	if err != nil {
		return storeError("update operation "+operation.ID.String(), err)
	}
	return requireAffected("update operation "+operation.ID.String(), result)
}

func (s *Store) ListUnfinishedOperations(ctx context.Context) ([]domain.Operation, error) {
	db, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	rows, err := db.QueryContext(ctx, "SELECT "+operationColumns+` FROM operations
		WHERE state IN ('pending', 'running', 'rolling_back') ORDER BY updated_at, id`)
	if err != nil {
		return nil, storeError("list unfinished operations", err)
	}
	defer rows.Close()
	var operations []domain.Operation
	for rows.Next() {
		operation, err := scanOperation(rows)
		if err != nil {
			return nil, storeError("list unfinished operations", err)
		}
		operations = append(operations, operation)
	}
	if err := rows.Err(); err != nil {
		return nil, storeError("list unfinished operations", err)
	}
	return operations, nil
}
