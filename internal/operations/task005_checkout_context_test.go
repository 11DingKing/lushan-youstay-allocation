package operations_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/operations"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

func TestCancelledCheckoutLeavesStayAndTasksUnchanged(t *testing.T) {
	store, err := storesqlite.Open(context.Background(), filepath.Join(t.TempDir(), "checkout.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	if err=store.CreateUser(context.Background(), auth.User{ID:"g",Username:"guest",PasswordHash:auth.HashPassword("secret"),DisplayName:"Guest",Role:auth.RoleGuest,Active:true,CreatedAt:now,UpdatedAt:now}); err!=nil { t.Fatal(err) }
	property := domain.Property{ID:"p", Name:"YouStay", Zone:"Lushan", Timezone:"Asia/Shanghai", Status:domain.PropertyOpen, Version:1, CreatedAt:now, UpdatedAt:now}
	resource := domain.Resource{ID:"r", PropertyID:"p", Code:"V1", Name:"Villa", Kind:domain.ResourceVillaRoom, Capacity:2, BasePriceCents:50000, Status:domain.ResourceOccupied, Version:1, CreatedAt:now, UpdatedAt:now}
	if err=store.CreateProperty(context.Background(), property); err!=nil { t.Fatal(err) }
	if err=store.CreateResource(context.Background(), resource); err!=nil { t.Fatal(err) }
	stay := domain.Stay{ID:"s", GuestID:"g", ResourceID:"r", Status:domain.StayCheckedIn, CheckInDate:"2026-08-25", CheckOutDate:"2026-08-26", PartySize:1, QuotedCents:50000, IdempotencyKey:"k", Version:1, CreatedAt:now, UpdatedAt:now}
	if _,err=store.DB().Exec(`INSERT INTO stays(id,guest_id,resource_id,status,check_in_date,check_out_date,party_size,quoted_cents,guarantee_cents,idempotency_key,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,stay.ID,stay.GuestID,stay.ResourceID,stay.Status,stay.CheckInDate,stay.CheckOutDate,stay.PartySize,stay.QuotedCents,0,stay.IdempotencyKey,stay.Version,now.Format(time.RFC3339Nano),now.Format(time.RFC3339Nano)); err!=nil { t.Fatal(err) }
	service := operations.NewService(store, clock.NewManual(now))
	ctx,cancel := context.WithCancel(context.Background()); cancel()
	_,err=service.Checkout(ctx,stay.ID,"desk","req")
	if !errors.Is(err,context.Canceled) { t.Fatalf("cancelled checkout error=%v",err) }
	loaded,_:=store.GetStay(context.Background(),stay.ID)
	count,_:=store.CountTable(context.Background(),"operational_tasks")
	if loaded.Status!=domain.StayCheckedIn || count!=0 { t.Fatalf("cancelled checkout changed status=%s tasks=%d",loaded.Status,count) }
}
