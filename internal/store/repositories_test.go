package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestProfileAndSubscriptionCRUD(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	profileOne := testProfile("profile-1", "one", now)
	profileTwo := testProfile("profile-2", "two", now)
	profileTwo.Mode = domain.ProfileModeExternal
	profileTwo.ConfigPath = "/tmp/external.yaml"
	for _, profile := range []domain.Profile{profileOne, profileTwo} {
		if err := store.CreateProfile(ctx, profile); err != nil {
			t.Fatalf("create profile: %v", err)
		}
	}
	if err := store.SetActiveProfile(ctx, profileTwo.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("set active profile: %v", err)
	}
	got, err := store.GetProfile(ctx, profileTwo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Active || got.ConfigPath != profileTwo.ConfigPath {
		t.Fatalf("unexpected profile round trip: %+v", got)
	}
	got.Name = "updated"
	got.UpdatedAt = now.Add(2 * time.Second)
	updated, err := store.UpdateProfile(ctx, got)
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if updated.Revision != 3 || updated.Name != "updated" || !updated.Active {
		t.Fatalf("unexpected updated profile: %+v", updated)
	}
	if _, err := store.UpdateProfile(ctx, got); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v, want ErrConflict", err)
	}
	profiles, err := store.ListProfiles(ctx)
	if err != nil || len(profiles) != 2 {
		t.Fatalf("list profiles = %v, %v", profiles, err)
	}

	subscription := testSubscription("subscription-1", profileOne.ID, now)
	if err := store.CreateSubscription(ctx, subscription); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	gotSubscription, err := store.GetSubscription(ctx, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSubscription.URL != subscription.URL || !gotSubscription.Enabled {
		t.Fatalf("unexpected subscription round trip: %+v", gotSubscription)
	}
	secretURL := "https://example.invalid/private-token"
	duplicate := subscription
	duplicate.URL = secretURL
	if err := store.CreateSubscription(ctx, duplicate); !errors.Is(err, ErrConflict) || strings.Contains(err.Error(), secretURL) {
		t.Fatalf("duplicate subscription error = %q", err)
	}
	gotSubscription.Name = "updated source"
	gotSubscription.UpdatedAt = now.Add(time.Second)
	if err := store.UpdateSubscription(ctx, gotSubscription); err != nil {
		t.Fatalf("update subscription: %v", err)
	}
	listed, err := store.ListSubscriptions(ctx, profileOne.ID)
	if err != nil || len(listed) != 1 || listed[0].Name != "updated source" {
		t.Fatalf("list subscriptions = %+v, %v", listed, err)
	}
	if err := store.DeleteSubscription(ctx, subscription.ID); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}
	if _, err := store.GetSubscription(ctx, subscription.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted subscription error = %v, want ErrNotFound", err)
	}
}

func TestReplaceSubscriptionNodes_IsolatedAndAtomic(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	profile := testProfile("profile-1", "one", now)
	if err := store.CreateProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	first := testSubscription("subscription-1", profile.ID, now)
	second := testSubscription("subscription-2", profile.ID, now)
	for _, subscription := range []domain.Subscription{first, second} {
		if err := store.CreateSubscription(ctx, subscription); err != nil {
			t.Fatal(err)
		}
	}
	oldFirst := testNode("node-old", first.ID, "remote-old", 0, now)
	oldSecond := testNode("node-shared", second.ID, "remote-other", 0, now)
	if err := store.ReplaceSubscriptionNodes(ctx, first, []domain.Node{oldFirst}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSubscriptionNodes(ctx, second, []domain.Node{oldSecond}); err != nil {
		t.Fatal(err)
	}

	failedNode := testNode(oldSecond.ID.String(), first.ID, "remote-new", 0, now.Add(time.Second))
	if err := store.ReplaceSubscriptionNodes(ctx, first, []domain.Node{failedNode}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-source node conflict error = %v, want ErrConflict", err)
	}
	assertNodeIDs(t, store, first.ID, []domain.NodeID{oldFirst.ID})
	assertNodeIDs(t, store, second.ID, []domain.NodeID{oldSecond.ID})
	duplicateRemote := testNode("node-new", first.ID, "remote-old", 0, now.Add(time.Second))
	if err := store.ReplaceSubscriptionNodes(ctx, first, []domain.Node{duplicateRemote, duplicateRemote}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate remote key error = %v, want ErrInvalid", err)
	}
	assertNodeIDs(t, store, first.ID, []domain.NodeID{oldFirst.ID})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.ReplaceSubscriptionNodes(canceled, first, []domain.Node{testNode("node-canceled", first.ID, "remote-canceled", 0, now.Add(time.Second))}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replacement error = %v, want context.Canceled", err)
	}
	assertNodeIDs(t, store, first.ID, []domain.NodeID{oldFirst.ID})

	newNodes := []domain.Node{
		testNode("node-2", first.ID, "remote-2", 1, now.Add(2*time.Second)),
		testNode("node-1", first.ID, "remote-1", 0, now.Add(2*time.Second)),
	}
	successAt := now.Add(2 * time.Second)
	first.ETag = "etag-1"
	first.LastAttemptAt = &successAt
	first.LastSuccessAt = &successAt
	first.UpdatedAt = successAt
	if err := store.ReplaceSubscriptionNodes(ctx, first, newNodes); err != nil {
		t.Fatalf("replace first source: %v", err)
	}
	assertNodeIDs(t, store, first.ID, []domain.NodeID{"node-1", "node-2"})
	assertNodeIDs(t, store, second.ID, []domain.NodeID{oldSecond.ID})
	refreshed, err := store.GetSubscription(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ETag != "etag-1" || refreshed.LastSuccessAt == nil || !refreshed.LastSuccessAt.Equal(successAt) {
		t.Fatalf("refresh metadata not persisted: %+v", refreshed)
	}

	if err := store.ReplaceSubscriptionNodes(ctx, first, nil); err != nil {
		t.Fatalf("clear first source: %v", err)
	}
	assertNodeIDs(t, store, first.ID, nil)
	assertNodeIDs(t, store, second.ID, []domain.NodeID{oldSecond.ID})
	if err := store.DeleteSubscription(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	assertNodeIDs(t, store, second.ID, nil)
}

func TestOperationsSettingsAndProfileCascade(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	profile := testProfile("profile-1", "one", now)
	if err := store.CreateProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	subscription := testSubscription("subscription-1", profile.ID, now)
	if err := store.CreateSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSubscriptionNodes(ctx, subscription, []domain.Node{
		testNode("node-1", subscription.ID, "remote-1", 0, now),
	}); err != nil {
		t.Fatal(err)
	}
	profileID := profile.ID
	operation := domain.Operation{
		ID: "operation-1", ProfileID: &profileID, Kind: "profile.apply", State: domain.OperationStatePending,
		Phase: "queued", Recovery: json.RawMessage(`{"previous":"profile-0"}`), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateOperation(ctx, operation); err != nil {
		t.Fatalf("create operation: %v", err)
	}
	operation.State = domain.OperationStateRunning
	operation.Phase = "validate"
	operation.Attempt = 1
	operation.UpdatedAt = now.Add(time.Second)
	if err := store.UpdateOperation(ctx, operation); err != nil {
		t.Fatalf("update operation: %v", err)
	}
	unfinished, err := store.ListUnfinishedOperations(ctx)
	if err != nil || len(unfinished) != 1 || unfinished[0].Phase != "validate" {
		t.Fatalf("unfinished operations = %+v, %v", unfinished, err)
	}

	global := domain.Setting{Key: "telemetry", Value: json.RawMessage(`false`), UpdatedAt: now}
	profileSetting := domain.Setting{ProfileID: &profileID, Key: "routing.mode", Value: json.RawMessage(`"rule"`), UpdatedAt: now}
	for _, setting := range []domain.Setting{global, profileSetting} {
		if err := store.SetSetting(ctx, setting); err != nil {
			t.Fatalf("set setting: %v", err)
		}
	}
	global.Value = json.RawMessage(`true`)
	global.UpdatedAt = now.Add(time.Second)
	if err := store.SetSetting(ctx, global); err != nil {
		t.Fatalf("upsert global setting: %v", err)
	}
	globalSettings, err := store.ListSettings(ctx, nil)
	if err != nil || len(globalSettings) != 1 || string(globalSettings[0].Value) != "true" {
		t.Fatalf("global settings = %+v, %v", globalSettings, err)
	}
	gotProfileSetting, err := store.GetSetting(ctx, &profileID, profileSetting.Key)
	if err != nil || string(gotProfileSetting.Value) != `"rule"` {
		t.Fatalf("profile setting = %+v, %v", gotProfileSetting, err)
	}

	if err := store.DeleteProfile(ctx, profile.ID); err != nil {
		t.Fatalf("delete profile: %v", err)
	}
	gotOperation, err := store.GetOperation(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOperation.ProfileID != nil {
		t.Fatalf("operation profile id = %v, want nil after profile deletion", gotOperation.ProfileID)
	}
	if _, err := store.GetSetting(ctx, &profileID, profileSetting.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("profile setting after cascade error = %v", err)
	}
	if _, err := store.GetSetting(ctx, nil, global.Key); err != nil {
		t.Fatalf("global setting should survive profile deletion: %v", err)
	}
	if _, err := store.GetSubscription(ctx, subscription.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("subscription after profile cascade error = %v", err)
	}
	assertNodeIDs(t, store, subscription.ID, nil)
	if err := store.DeleteSetting(ctx, nil, global.Key); err != nil {
		t.Fatalf("delete global setting: %v", err)
	}
	if _, err := store.GetSetting(ctx, nil, global.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted global setting error = %v", err)
	}
}

func TestOperations_ListUnfinishedExcludesTerminalStates(t *testing.T) {
	store := openTestStore(t)
	now := time.Now().UTC()
	states := []domain.OperationState{
		domain.OperationStatePending,
		domain.OperationStateRunning,
		domain.OperationStateRollingBack,
		domain.OperationStateSucceeded,
		domain.OperationStateFailed,
		domain.OperationStateRolledBack,
	}
	for i, state := range states {
		operation := domain.Operation{
			ID: domain.OperationID(fmt.Sprintf("operation-%d", i)), Kind: "test", State: state,
			Phase: "phase", Recovery: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now,
		}
		if err := store.CreateOperation(context.Background(), operation); err != nil {
			t.Fatal(err)
		}
	}
	operations, err := store.ListUnfinishedOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 3 {
		t.Fatalf("unfinished operation count = %d, want 3", len(operations))
	}
}

func TestStore_ConcurrentWritesAndContextTimeout(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func(value int) {
			defer wait.Done()
			errorsSeen <- store.SetSetting(ctx, domain.Setting{
				Key: "concurrent", Value: json.RawMessage(fmt.Sprintf("%d", value)), UpdatedAt: now,
			})
		}(i)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent setting write: %v", err)
		}
	}
	settings, err := store.ListSettings(ctx, nil)
	if err != nil || len(settings) != 1 {
		t.Fatalf("concurrent settings = %+v, %v", settings, err)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	err = store.SetSetting(timeoutCtx, domain.Setting{Key: "blocked", Value: json.RawMessage(`true`), UpdatedAt: now})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write error = %v, want deadline exceeded", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSetting(ctx, nil, "blocked"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blocked setting error = %v, want ErrNotFound", err)
	}
}

func TestStore_UncommittedTransactionDoesNotSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO settings(profile_id, key, value, updated_at) VALUES (NULL, 'temporary', 'true', ?)`, formatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.GetSetting(context.Background(), nil, "temporary"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back setting error = %v, want ErrNotFound", err)
	}
}

func testProfile(id, name string, now time.Time) domain.Profile {
	return domain.Profile{
		ID: domain.ProfileID(id), Name: name, Mode: domain.ProfileModeManaged, CoreType: domain.CoreTypeMihomo,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func testSubscription(id string, profileID domain.ProfileID, now time.Time) domain.Subscription {
	return domain.Subscription{
		ID: domain.SubscriptionID(id), ProfileID: profileID, Name: id,
		URL: "https://example.invalid/subscription", Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
}

func testNode(id string, subscriptionID domain.SubscriptionID, remoteKey string, position int, now time.Time) domain.Node {
	return domain.Node{
		ID: domain.NodeID(id), SubscriptionID: subscriptionID, RemoteKey: remoteKey, Name: id,
		Protocol: "vless", Spec: json.RawMessage(`{"server":"example.invalid"}`), Position: position, CreatedAt: now, UpdatedAt: now,
	}
}

func assertNodeIDs(t *testing.T, store *Store, subscriptionID domain.SubscriptionID, want []domain.NodeID) {
	t.Helper()
	nodes, err := store.ListNodes(context.Background(), subscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != len(want) {
		t.Fatalf("node count for %s = %d, want %d", subscriptionID, len(nodes), len(want))
	}
	for i := range want {
		if nodes[i].ID != want[i] {
			t.Fatalf("node %d for %s = %s, want %s", i, subscriptionID, nodes[i].ID, want[i])
		}
	}
}
