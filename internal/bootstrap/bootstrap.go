package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

func SeedCatalog(ctx context.Context, store *storesqlite.Store, now time.Time) error {
	property := domain.Property{ID: "prop_lushan_main", Name: "庐山悠宿", Zone: "牯岭-山南梯度旅居带", Timezone: "Asia/Shanghai", Status: domain.PropertyOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if _, err := store.GetProperty(ctx, property.ID); err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("check seed property: %w", err)
		}
		if err := store.CreateProperty(ctx, property); err != nil {
			return fmt.Errorf("seed property: %w", err)
		}
	}
	resources := []domain.Resource{
		{ID: "res_villa_101", PropertyID: property.ID, Code: "YS-V101", Name: "云栖别墅 101", Kind: domain.ResourceVillaRoom, Capacity: 4, BasePriceCents: 128000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "res_mountain_201", PropertyID: property.ID, Code: "YS-M201", Name: "松涛山居 201", Kind: domain.ResourceMountainRoom, Capacity: 2, BasePriceCents: 68000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "res_rv_01", PropertyID: property.ID, Code: "YS-RV01", Name: "云海房车泊位 01", Kind: domain.ResourceRVBert, Capacity: 5, BasePriceCents: 36000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "res_camp_01", PropertyID: property.ID, Code: "YS-C01", Name: "松林营位 01", Kind: domain.ResourceCampSite, Capacity: 4, BasePriceCents: 22000, Status: domain.ResourceOpen, Version: 1, CreatedAt: now, UpdatedAt: now},
	}
	for _, resource := range resources {
		if _, err := store.GetResource(ctx, resource.ID); err == nil {
			continue
		} else if !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("check seed resource %s: %w", resource.ID, err)
		}
		if err := store.CreateResource(ctx, resource); err != nil {
			return fmt.Errorf("seed resource %s: %w", resource.ID, err)
		}
	}
	return nil
}
