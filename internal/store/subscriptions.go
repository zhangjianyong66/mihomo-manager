package store

import (
	"context"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const subscriptionColumns = `id, profile_id, name, url, enabled, etag, last_modified, last_attempt_at, last_success_at, created_at, updated_at`

func (s *Store) CreateSubscription(ctx context.Context, subscription domain.Subscription) error {
	if err := subscription.Validate(); err != nil {
		return invalid("create subscription", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	_, err = db.ExecContext(ctx, `
		INSERT INTO subscriptions(
			id, profile_id, name, url, enabled, etag, last_modified,
			last_attempt_at, last_success_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		subscription.ID.String(), subscription.ProfileID.String(), subscription.Name, subscription.URL, subscription.Enabled,
		nullableText(subscription.ETag), nullableText(subscription.LastModified), nullableTime(subscription.LastAttemptAt),
		nullableTime(subscription.LastSuccessAt), formatTime(subscription.CreatedAt), formatTime(subscription.UpdatedAt),
	)
	return storeError("create subscription "+subscription.ID.String(), err)
}

func (s *Store) GetSubscription(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error) {
	if err := id.Validate(); err != nil {
		return domain.Subscription{}, invalid("get subscription", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return domain.Subscription{}, err
	}
	defer release()
	result, err := scanSubscription(db.QueryRowContext(ctx,
		"SELECT "+subscriptionColumns+" FROM subscriptions WHERE id = ?", id.String(),
	))
	if err != nil {
		return domain.Subscription{}, storeError("get subscription "+id.String(), err)
	}
	return result, nil
}

func (s *Store) ListSubscriptions(ctx context.Context, profileID domain.ProfileID) ([]domain.Subscription, error) {
	if err := profileID.Validate(); err != nil {
		return nil, invalid("list subscriptions", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	rows, err := db.QueryContext(ctx,
		"SELECT "+subscriptionColumns+" FROM subscriptions WHERE profile_id = ? ORDER BY name, id", profileID.String(),
	)
	if err != nil {
		return nil, storeError("list subscriptions for profile "+profileID.String(), err)
	}
	defer rows.Close()
	var subscriptions []domain.Subscription
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, storeError("list subscriptions for profile "+profileID.String(), err)
		}
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		return nil, storeError("list subscriptions for profile "+profileID.String(), err)
	}
	return subscriptions, nil
}

func (s *Store) UpdateSubscription(ctx context.Context, subscription domain.Subscription) error {
	if err := subscription.Validate(); err != nil {
		return invalid("update subscription", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	result, err := db.ExecContext(ctx, `
		UPDATE subscriptions SET
			profile_id = ?, name = ?, url = ?, enabled = ?, etag = ?, last_modified = ?,
			last_attempt_at = ?, last_success_at = ?, updated_at = ?
		WHERE id = ?`,
		subscription.ProfileID.String(), subscription.Name, subscription.URL, subscription.Enabled,
		nullableText(subscription.ETag), nullableText(subscription.LastModified), nullableTime(subscription.LastAttemptAt),
		nullableTime(subscription.LastSuccessAt), formatTime(subscription.UpdatedAt), subscription.ID.String(),
	)
	if err != nil {
		return storeError("update subscription "+subscription.ID.String(), err)
	}
	return requireAffected("update subscription "+subscription.ID.String(), result)
}

func (s *Store) DeleteSubscription(ctx context.Context, id domain.SubscriptionID) error {
	if err := id.Validate(); err != nil {
		return invalid("delete subscription", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	result, err := db.ExecContext(ctx, "DELETE FROM subscriptions WHERE id = ?", id.String())
	if err != nil {
		return storeError("delete subscription "+id.String(), err)
	}
	return requireAffected("delete subscription "+id.String(), result)
}
