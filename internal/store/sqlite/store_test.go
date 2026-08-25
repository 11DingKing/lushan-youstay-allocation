package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

func openStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	store, err := storesqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func createUser(t *testing.T, store *storesqlite.Store, id, username string, role auth.Role) auth.User {
	t.Helper()
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	user := auth.User{ID: id, Username: username, PasswordHash: auth.HashPassword("secret"), DisplayName: username, Role: role, Active: true, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	return user
}

func createCatalog(t *testing.T, store *storesqlite.Store) (domain.Property, domain.Resource) {
	t.Helper()
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	property := domain.Property{ID: "p1", Name: "YouStay", Zone: "Lushan", Timezone: "Asia/Shanghai", Status: domain.PropertyOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProperty(context.Background(), property); err != nil {
		t.Fatalf("CreateProperty() error = %v", err)
	}
	resource := domain.Resource{ID: "r1", PropertyID: property.ID, Code: "R1", Name: "Room", Kind: domain.ResourceMountainRoom, Capacity: 2, BasePriceCents: 50_000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateResource(context.Background(), resource); err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	return property, resource
}

func heldStay(id, key, guestID, resourceID string, now time.Time) domain.Stay {
	expires := now.Add(15 * time.Minute)
	return domain.Stay{ID: id, GuestID: guestID, ResourceID: resourceID, Status: domain.StayHeld, CheckInDate: "2026-09-01", CheckOutDate: "2026-09-03", PartySize: 2, QuotedCents: 100_000, HoldExpiresAt: &expires, IdempotencyKey: key, Version: 1, CreatedAt: now, UpdatedAt: now}
}

func audit(now time.Time, objectID string) storesqlite.AuditEvent {
	actor := "u1"
	return storesqlite.AuditEvent{ActorID: &actor, RequestID: "req-test", Action: "test", ObjectType: "stay", ObjectID: objectID, Result: "succeeded", DetailJSON: "{}", OccurredAt: now}
}

func TestMigrationsCreateAllRelationalTablesAndAreRepeatable(t *testing.T) {
	store := openStore(t)
	tables := []string{"users", "sessions", "properties", "resources", "stays", "stay_nights", "identity_checks", "payments", "operational_tasks", "damage_claims", "refund_settlements", "audit_events", "outbox_events"}
	for _, table := range tables {
		count, err := store.CountTable(context.Background(), table)
		if err != nil {
			t.Errorf("CountTable(%s) error = %v", table, err)
		}
		if count < 0 {
			t.Errorf("CountTable(%s) = %d", table, count)
		}
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	var migrationCount int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 2 {
		t.Fatalf("migration count = %d, want 2", migrationCount)
	}
}

func TestUserAndSessionPersistenceLifecycle(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	found, err := store.FindUserByUsername(context.Background(), user.Username)
	if err != nil {
		t.Fatalf("FindUserByUsername() error = %v", err)
	}
	if found.ID != user.ID || found.Role != auth.RoleGuest || !found.Active {
		t.Fatalf("found user = %#v", found)
	}
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	session := auth.Session{ID: "s1", UserID: user.ID, TokenHash: "digest", ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now}
	if err := store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	loaded, err := store.FindSessionByTokenHash(context.Background(), "digest")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash() error = %v", err)
	}
	if !loaded.Active(now) {
		t.Fatal("fresh session is not active")
	}
	seen := now.Add(time.Minute)
	if err := store.TouchSession(context.Background(), loaded.ID, seen); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSession(context.Background(), loaded.ID, seen); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.FindSessionByTokenHash(context.Background(), "digest")
	if err != nil || revoked.RevokedAt == nil {
		t.Fatalf("revoked session = %#v, %v", revoked, err)
	}
}

func TestExpiredSessionCleanupKeepsCurrentSessions(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	sessions := []auth.Session{
		{ID: "expired", UserID: user.ID, TokenHash: "expired", ExpiresAt: now.Add(-time.Second), CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour)},
		{ID: "active", UserID: user.ID, TokenHash: "active", ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now},
	}
	for _, session := range sessions {
		if err := store.CreateSession(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	count, err := store.DeleteExpiredSessions(context.Background(), now)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("deleted = %d, want 1", count)
	}
	if _, err := store.FindSessionByTokenHash(context.Background(), "active"); err != nil {
		t.Fatalf("active session missing: %v", err)
	}
	if _, err := store.FindSessionByTokenHash(context.Background(), "expired"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired lookup error = %v", err)
	}
}

func TestCatalogPaginationFiltersAndSorts(t *testing.T) {
	store := openStore(t)
	property, _ := createCatalog(t, store)
	now := time.Now().UTC()
	for index := 2; index <= 7; index++ {
		resource := domain.Resource{ID: fmt.Sprintf("r%d", index), PropertyID: property.ID, Code: fmt.Sprintf("R%d", index), Name: "Room", Kind: domain.ResourceCampSite, Capacity: 4, BasePriceCents: 20_000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := store.CreateResource(context.Background(), resource); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := store.ListResources(context.Background(), storesqlite.ResourceFilter{PropertyID: property.ID, Kind: domain.ResourceCampSite, Status: domain.ResourceOpen, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if total != 6 {
		t.Fatalf("total = %d, want 6", total)
	}
	if len(items) != 2 || items[0].Code != "R3" || items[1].Code != "R4" {
		t.Fatalf("items = %#v", items)
	}
}

func TestOptimisticResourceUpdateRejectsStaleVersion(t *testing.T) {
	store := openStore(t)
	_, resource := createCatalog(t, store)
	now := time.Now().UTC()
	if err := store.UpdateResourceStatus(context.Background(), resource.ID, domain.ResourceOpen, domain.ResourceHeld, 1, now); err != nil {
		t.Fatal(err)
	}
	err := store.UpdateResourceStatus(context.Background(), resource.ID, domain.ResourceOpen, domain.ResourceFaulted, 1, now)
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	loaded, err := store.GetResource(context.Background(), resource.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domain.ResourceHeld || loaded.Version != 2 {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestCreateHeldStayPersistsNightsAndAuditAtomically(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	_, resource := createCatalog(t, store)
	now := time.Now().UTC()
	stay := heldStay("stay1", "key1", user.ID, resource.ID, now)
	if err := store.CreateHeldStay(context.Background(), stay, []string{"2026-09-01", "2026-09-02"}, audit(now, stay.ID)); err != nil {
		t.Fatalf("CreateHeldStay() error = %v", err)
	}
	loaded, err := store.GetStay(context.Background(), stay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domain.StayHeld || loaded.QuotedCents != stay.QuotedCents {
		t.Fatalf("loaded = %#v", loaded)
	}
	for _, table := range []string{"stays", "stay_nights", "audit_events"} {
		count, err := store.CountTable(context.Background(), table)
		if err != nil || count == 0 {
			t.Errorf("%s count = %d, %v", table, count, err)
		}
	}
}

func TestCreateHeldStayRollsBackWhenOneNightConflicts(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	_, resource := createCatalog(t, store)
	now := time.Now().UTC()
	first := heldStay("stay1", "key1", user.ID, resource.ID, now)
	if err := store.CreateHeldStay(context.Background(), first, []string{"2026-09-01", "2026-09-02"}, audit(now, first.ID)); err != nil {
		t.Fatal(err)
	}
	second := heldStay("stay2", "key2", user.ID, resource.ID, now)
	err := store.CreateHeldStay(context.Background(), second, []string{"2026-08-31", "2026-09-01"}, audit(now, second.ID))
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting hold error = %v", err)
	}
	if _, err := store.GetStay(context.Background(), second.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("partial stay remained: %v", err)
	}
	var leaked int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights WHERE stay_id=?`, second.ID).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("leaked nights = %d", leaked)
	}
}

func TestConcurrentNightAllocationHasSingleWinner(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	_, resource := createCatalog(t, store)
	now := time.Now().UTC()
	start := make(chan struct{})
	var successes atomic.Int32
	var conflicts atomic.Int32
	var wg sync.WaitGroup
	for index := 0; index < 8; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			stay := heldStay(fmt.Sprintf("stay%d", index), fmt.Sprintf("key%d", index), user.ID, resource.ID, now)
			err := store.CreateHeldStay(context.Background(), stay, []string{"2026-10-01"}, audit(now, stay.ID))
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, domain.ErrConflict):
				conflicts.Add(1)
			default:
				t.Errorf("CreateHeldStay() unexpected error = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successes = %d, want 1", successes.Load())
	}
	if conflicts.Load() != 7 {
		t.Fatalf("conflicts = %d, want 7", conflicts.Load())
	}
}

func TestGuaranteeStayCommitsIdentityPaymentNightsAndAudit(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	verifier := createUser(t, store, "desk1", "desk", auth.RoleFrontdesk)
	_, resource := createCatalog(t, store)
	now := time.Now().UTC()
	stay := heldStay("stay1", "key1", user.ID, resource.ID, now)
	if err := store.CreateHeldStay(context.Background(), stay, []string{"2026-09-01", "2026-09-02"}, audit(now, stay.ID)); err != nil {
		t.Fatal(err)
	}
	updated, _ := stay.Transition(domain.StayGuaranteed, now)
	updated.GuaranteeCents = 30_000
	check := domain.IdentityCheck{ID: "check1", StayID: stay.ID, GuestID: user.ID, DocumentDigest: "digestdigestdigest", Status: domain.IdentityVerified, VerifiedBy: &verifier.ID, VerifiedAt: &now, CreatedAt: now}
	payment := domain.Payment{ID: "pay1", StayID: stay.ID, Provider: "mock", ProviderEventID: "evt1", AmountCents: 30_000, Kind: domain.PaymentGuarantee, Status: domain.PaymentSucceeded, OccurredAt: now, CreatedAt: now}
	if err := store.GuaranteeStay(context.Background(), updated, check, payment, audit(now, stay.ID)); err != nil {
		t.Fatalf("GuaranteeStay() error = %v", err)
	}
	loaded, _ := store.GetStay(context.Background(), stay.ID)
	if loaded.Status != domain.StayGuaranteed || loaded.HoldExpiresAt != nil {
		t.Fatalf("loaded = %#v", loaded)
	}
	for _, table := range []string{"identity_checks", "payments"} {
		count, _ := store.CountTable(context.Background(), table)
		if count != 1 {
			t.Errorf("%s count = %d", table, count)
		}
	}
	var reserved int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights WHERE stay_id=? AND status='reserved'`, stay.ID).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 2 {
		t.Fatalf("reserved nights = %d", reserved)
	}
}

func TestDuplicatePaymentNotificationIsIdempotent(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	_, resource := createCatalog(t, store)
	now := time.Now().UTC()
	stay := heldStay("stay1", "key1", user.ID, resource.ID, now)
	if err := store.CreateHeldStay(context.Background(), stay, []string{"2026-09-01"}, audit(now, stay.ID)); err != nil {
		t.Fatal(err)
	}
	payment := domain.Payment{ID: "pay1", StayID: stay.ID, Provider: "wechat", ProviderEventID: "same-event", AmountCents: 10_000, Kind: domain.PaymentCharge, Status: domain.PaymentSucceeded, OccurredAt: now, CreatedAt: now}
	created, err := store.RecordPayment(context.Background(), payment)
	if err != nil || !created {
		t.Fatalf("first RecordPayment() = %v, %v", created, err)
	}
	payment.ID = "pay2"
	created, err = store.RecordPayment(context.Background(), payment)
	if err != nil || created {
		t.Fatalf("duplicate RecordPayment() = %v, %v", created, err)
	}
	count, _ := store.CountTable(context.Background(), "payments")
	if count != 1 {
		t.Fatalf("payments count = %d", count)
	}
}

func TestExpireDueHoldsReleasesOnlyExpiredInventory(t *testing.T) {
	store := openStore(t)
	user := createUser(t, store, "guest1", "guest", auth.RoleGuest)
	property, firstResource := createCatalog(t, store)
	now := time.Now().UTC()
	secondResource := domain.Resource{ID: "r2", PropertyID: property.ID, Code: "R2", Name: "Room2", Kind: domain.ResourceMountainRoom, Capacity: 2, BasePriceCents: 50_000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateResource(context.Background(), secondResource); err != nil {
		t.Fatal(err)
	}
	expired := heldStay("expired", "expired-key", user.ID, firstResource.ID, now.Add(-time.Hour))
	active := heldStay("active", "active-key", user.ID, secondResource.ID, now)
	if err := store.CreateHeldStay(context.Background(), expired, []string{"2026-09-01"}, audit(now, expired.ID)); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateHeldStay(context.Background(), active, []string{"2026-09-01"}, audit(now, active.ID)); err != nil {
		t.Fatal(err)
	}
	count, err := store.ExpireDueHolds(context.Background(), now, 10)
	if err != nil || count != 1 {
		t.Fatalf("ExpireDueHolds() = %d, %v", count, err)
	}
	expiredLoaded, _ := store.GetStay(context.Background(), expired.ID)
	activeLoaded, _ := store.GetStay(context.Background(), active.ID)
	if expiredLoaded.Status != domain.StayExpired {
		t.Errorf("expired status = %s", expiredLoaded.Status)
	}
	if activeLoaded.Status != domain.StayHeld {
		t.Errorf("active status = %s", activeLoaded.Status)
	}
	var released int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM stay_nights WHERE stay_id=? AND status='released'`, expired.ID).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if released != 1 {
		t.Fatalf("released nights = %d", released)
	}
}

func TestDatabaseStateSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	store, err := storesqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	property := domain.Property{ID: "persistent", Name: "Persistent", Zone: "Lushan", Timezone: "Asia/Shanghai", Status: domain.PropertyOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProperty(ctx, property); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storesqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetProperty(ctx, property.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != property.Name || loaded.Version != 1 {
		t.Fatalf("reopened property = %#v", loaded)
	}
}
