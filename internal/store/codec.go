package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type rowScanner interface {
	Scan(...any) error
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time: %w", err)
	}
	return parsed.UTC(), nil
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func nullableProfileID(value *domain.ProfileID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func parseProfileID(value sql.NullString) *domain.ProfileID {
	if !value.Valid {
		return nil
	}
	id := domain.ProfileID(value.String)
	return &id
}

func copyJSON(value string) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func scanProfile(row rowScanner) (domain.Profile, error) {
	var profile domain.Profile
	var configPath sql.NullString
	var active int
	var createdAt, updatedAt string
	if err := row.Scan(
		&profile.ID, &profile.Name, &profile.Mode, &profile.CoreType, &configPath,
		&active, &profile.Revision, &createdAt, &updatedAt,
	); err != nil {
		return domain.Profile{}, err
	}
	profile.ConfigPath = configPath.String
	profile.Active = active == 1
	var err error
	profile.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Profile{}, err
	}
	profile.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Profile{}, err
	}
	return profile, nil
}

func scanSubscription(row rowScanner) (domain.Subscription, error) {
	var subscription domain.Subscription
	var etag, lastModified, lastAttempt, lastSuccess sql.NullString
	var enabled int
	var createdAt, updatedAt string
	if err := row.Scan(
		&subscription.ID, &subscription.ProfileID, &subscription.Name, &subscription.URL,
		&enabled, &etag, &lastModified, &lastAttempt, &lastSuccess, &createdAt, &updatedAt,
	); err != nil {
		return domain.Subscription{}, err
	}
	subscription.Enabled = enabled == 1
	subscription.ETag = etag.String
	subscription.LastModified = lastModified.String
	var err error
	subscription.LastAttemptAt, err = parseNullableTime(lastAttempt)
	if err != nil {
		return domain.Subscription{}, err
	}
	subscription.LastSuccessAt, err = parseNullableTime(lastSuccess)
	if err != nil {
		return domain.Subscription{}, err
	}
	subscription.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Subscription{}, err
	}
	subscription.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Subscription{}, err
	}
	return subscription, nil
}

func scanNode(row rowScanner) (domain.Node, error) {
	var node domain.Node
	var spec, createdAt, updatedAt string
	if err := row.Scan(
		&node.ID, &node.SubscriptionID, &node.RemoteKey, &node.Name, &node.Protocol,
		&spec, &node.Position, &createdAt, &updatedAt,
	); err != nil {
		return domain.Node{}, err
	}
	node.Spec = copyJSON(spec)
	var err error
	node.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Node{}, err
	}
	node.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

func scanOperation(row rowScanner) (domain.Operation, error) {
	var operation domain.Operation
	var profileID, errorCode sql.NullString
	var recovery, createdAt, updatedAt string
	if err := row.Scan(
		&operation.ID, &profileID, &operation.Kind, &operation.State, &operation.Phase,
		&operation.Attempt, &recovery, &errorCode, &createdAt, &updatedAt,
	); err != nil {
		return domain.Operation{}, err
	}
	operation.ProfileID = parseProfileID(profileID)
	operation.Recovery = copyJSON(recovery)
	operation.ErrorCode = errorCode.String
	var err error
	operation.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Operation{}, err
	}
	operation.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Operation{}, err
	}
	return operation, nil
}

func scanSetting(row rowScanner) (domain.Setting, error) {
	var setting domain.Setting
	var profileID sql.NullString
	var value, updatedAt string
	if err := row.Scan(&profileID, &setting.Key, &value, &updatedAt); err != nil {
		return domain.Setting{}, err
	}
	setting.ProfileID = parseProfileID(profileID)
	setting.Value = copyJSON(value)
	var err error
	setting.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.Setting{}, err
	}
	return setting, nil
}
