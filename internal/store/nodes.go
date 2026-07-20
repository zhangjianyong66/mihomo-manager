package store

import (
	"context"
	"fmt"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const nodeColumns = `id, subscription_id, remote_key, name, protocol, spec, position, created_at, updated_at`

func (s *Store) ListNodes(ctx context.Context, subscriptionID domain.SubscriptionID) ([]domain.Node, error) {
	if err := subscriptionID.Validate(); err != nil {
		return nil, invalid("list subscription nodes", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	rows, err := db.QueryContext(ctx,
		"SELECT "+nodeColumns+" FROM nodes WHERE subscription_id = ? ORDER BY position, id", subscriptionID.String(),
	)
	if err != nil {
		return nil, storeError("list nodes for subscription "+subscriptionID.String(), err)
	}
	defer rows.Close()
	var nodes []domain.Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, storeError("list nodes for subscription "+subscriptionID.String(), err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, storeError("list nodes for subscription "+subscriptionID.String(), err)
	}
	return nodes, nil
}

func (s *Store) ReplaceSubscriptionNodes(ctx context.Context, subscription domain.Subscription, nodes []domain.Node) error {
	if err := subscription.Validate(); err != nil {
		return invalid("replace subscription nodes", err)
	}
	remoteKeys := make(map[string]struct{}, len(nodes))
	nodeIDs := make(map[domain.NodeID]struct{}, len(nodes))
	for _, node := range nodes {
		if err := node.Validate(); err != nil {
			return invalid("replace subscription nodes", err)
		}
		if node.SubscriptionID != subscription.ID {
			return invalid("replace subscription nodes", fmt.Errorf("node %s belongs to another subscription", node.ID))
		}
		if _, exists := remoteKeys[node.RemoteKey]; exists {
			return invalid("replace subscription nodes", fmt.Errorf("duplicate remote key for node %s", node.ID))
		}
		if _, exists := nodeIDs[node.ID]; exists {
			return invalid("replace subscription nodes", fmt.Errorf("duplicate node id %s", node.ID))
		}
		remoteKeys[node.RemoteKey] = struct{}{}
		nodeIDs[node.ID] = struct{}{}
	}

	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin subscription node replacement", err)
	}
	defer tx.Rollback()
	var profileID string
	if err := tx.QueryRowContext(ctx, "SELECT profile_id FROM subscriptions WHERE id = ?", subscription.ID.String()).Scan(&profileID); err != nil {
		return storeError("find subscription "+subscription.ID.String(), err)
	}
	if profileID != subscription.ProfileID.String() {
		return invalid("replace subscription nodes", fmt.Errorf("subscription %s belongs to another profile", subscription.ID))
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM nodes WHERE subscription_id = ?", subscription.ID.String()); err != nil {
		return storeError("clear nodes for subscription "+subscription.ID.String(), err)
	}
	for _, node := range nodes {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO nodes(id, subscription_id, remote_key, name, protocol, spec, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			node.ID.String(), node.SubscriptionID.String(), node.RemoteKey, node.Name, node.Protocol, string(node.Spec),
			node.Position, formatTime(node.CreatedAt), formatTime(node.UpdatedAt),
		); err != nil {
			return storeError("insert node "+node.ID.String(), err)
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET etag = ?, last_modified = ?, last_attempt_at = ?, last_success_at = ?, updated_at = ?
		WHERE id = ?`,
		nullableText(subscription.ETag), nullableText(subscription.LastModified), nullableTime(subscription.LastAttemptAt),
		nullableTime(subscription.LastSuccessAt), formatTime(subscription.UpdatedAt), subscription.ID.String(),
	)
	if err != nil {
		return storeError("update subscription refresh metadata "+subscription.ID.String(), err)
	}
	if err := requireAffected("update subscription refresh metadata "+subscription.ID.String(), result); err != nil {
		return err
	}
	return storeError("commit subscription node replacement", tx.Commit())
}
