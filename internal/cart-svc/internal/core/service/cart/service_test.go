package cart

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
)

func TestGetActiveCartRecalculatesTotals(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	item, err := domain.NewCartItem("SKU-1", "Item 1", 1, 500, "USD", now, now)
	require.NoError(t, err)

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, error) {
			require.Equal(t, userID, gotUserID)

			return domain.Cart{
				ID:       cartID,
				UserID:   userID,
				Status:   domain.CartStatusActive,
				Currency: "USD",
			}, nil
		},
	}

	items := &stubCartItemRepository{
		listByCartIDFn: func(_ context.Context, gotCartID uuid.UUID) ([]domain.CartItem, error) {
			require.Equal(t, cartID, gotCartID)
			item.Quantity = 3
			item.LineTotal = 0

			return []domain.CartItem{item}, nil
		},
	}

	svc := NewCartService(carts, items, &stubProductSnapshotRepository{})

	got, err := svc.GetActiveCart(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, int64(1500), got.TotalAmount)
	require.Equal(t, int64(1500), got.Items[0].LineTotal)
	require.Equal(t, int64(3), got.Items[0].Quantity)
}

func TestAddCartItemCreatesActiveCartWhenMissing(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{}, outbound.ErrCartNotFound
		},
		createActiveFn: func(_ context.Context, gotUserID uuid.UUID, gotCurrency string) (domain.Cart, error) {
			require.Equal(t, userID, gotUserID)
			require.Equal(t, "USD", gotCurrency)

			return domain.Cart{
				ID:       cartID,
				UserID:   userID,
				Status:   domain.CartStatusActive,
				Currency: "USD",
			}, nil
		},
	}

	items := &stubCartItemRepository{
		insertFn: func(_ context.Context, params outbound.CartItemInsertParams) (domain.CartItem, error) {
			require.Equal(t, cartID, params.CartID)
			require.Equal(t, "SKU-1", params.SKU)
			require.Equal(t, int64(2), params.Quantity)
			require.Equal(t, int64(750), params.UnitPrice)
			require.Equal(t, "USD", params.Currency)
			require.Equal(t, "Product 1", params.ProductName)

			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			now := time.Now().UTC()
			item, err := domain.NewCartItem("SKU-1", "Product 1", 2, 750, "USD", now, now)
			require.NoError(t, err)

			return []domain.CartItem{item}, nil
		},
	}

	snapshots := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, gotSKU string) (domain.ProductSnapshot, error) {
			require.Equal(t, "SKU-1", gotSKU)

			now := time.Now().UTC()
			snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 750, "USD", now, now)
			require.NoError(t, err)

			return snapshot, nil
		},
	}

	svc := NewCartService(carts, items, snapshots)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{
		UserID:   userID,
		SKU:      " SKU-1 ",
		Quantity: 2,
	})
	require.NoError(t, err)
	require.Equal(t, cartID, got.ID)
	require.Equal(t, int64(1500), got.TotalAmount)
	require.Equal(t, 1, carts.createActiveCalls)
}

func TestAddCartItemInvalidQuantity(t *testing.T) {
	t.Parallel()

	svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{
		UserID:   uuid.New(),
		SKU:      "SKU-1",
		Quantity: 0,
	})
	require.ErrorIs(t, err, ErrInvalidQuantity)
	require.Equal(t, domain.Cart{}, got)
}

func TestAddCartItemProductSnapshotNotFound(t *testing.T) {
	t.Parallel()

	svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		},
	})

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{
		UserID:   uuid.New(),
		SKU:      "SKU-1",
		Quantity: 1,
	})
	require.ErrorIs(t, err, ErrProductSnapshotNotFound)
	require.Equal(t, domain.Cart{}, got)
}

func TestAddCartItemFallbackToCatalogOnSnapshotMiss(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	productID := uuid.New()

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	items := &stubCartItemRepository{
		insertFn: func(_ context.Context, params outbound.CartItemInsertParams) (domain.CartItem, error) {
			require.Equal(t, "SKU-1", params.SKU)
			require.Equal(t, "Product 1", params.ProductName)
			require.Equal(t, int64(1200), params.UnitPrice)
			require.Equal(t, "USD", params.Currency)
			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			now := time.Now().UTC()
			item, err := domain.NewCartItem("SKU-1", "Product 1", 1, 1200, "USD", now, now)
			require.NoError(t, err)

			return []domain.CartItem{item}, nil
		},
	}

	snapshotRepo := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		},
		upsertFn: func(_ context.Context, snapshot domain.ProductSnapshot) (domain.ProductSnapshot, error) {
			require.NotNil(t, snapshot.ProductID)
			require.Equal(t, productID, *snapshot.ProductID)
			return snapshot, nil
		},
	}

	catalogReader := &stubCatalogReader{
		getProductBySKUFn: func(_ context.Context, sku string) (outbound.CatalogProduct, error) {
			require.Equal(t, "SKU-1", sku)
			return outbound.CatalogProduct{
				ProductID:   productID.String(),
				SKU:         "SKU-1",
				Name:        "Product 1",
				Price:       1200,
				Currency:    "USD",
				IsPublished: true,
			}, nil
		},
	}

	svc := NewCartServiceWithCatalog(carts, items, snapshotRepo, catalogReader)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1200), got.TotalAmount)
	require.Equal(t, 1, catalogReader.getProductBySKUCalls)
	require.Equal(t, 1, snapshotRepo.upsertCalls)
}

func TestAddCartItemFallbackNotFoundMapsToMissingProduct(t *testing.T) {
	t.Parallel()

	snapshotRepo := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		},
	}

	catalogReader := &stubCatalogReader{
		getProductBySKUFn: func(_ context.Context, _ string) (outbound.CatalogProduct, error) {
			return outbound.CatalogProduct{}, outbound.ErrProductNotFound
		},
	}

	svc := NewCartServiceWithCatalog(&stubCartRepository{}, &stubCartItemRepository{}, snapshotRepo, catalogReader)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.New(), SKU: "SKU-404", Quantity: 1})
	require.ErrorIs(t, err, ErrProductSnapshotNotFound)
	require.Equal(t, domain.Cart{}, got)
	require.Equal(t, 1, catalogReader.getProductBySKUCalls)
	require.Equal(t, 0, snapshotRepo.upsertCalls)
}

func TestAddCartItemFallbackSnapshotUpsertFailure(t *testing.T) {
	t.Parallel()

	upsertErr := errors.New("write failed")

	snapshotRepo := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		},
		upsertFn: func(_ context.Context, _ domain.ProductSnapshot) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, upsertErr
		},
	}

	catalogReader := &stubCatalogReader{
		getProductBySKUFn: func(_ context.Context, _ string) (outbound.CatalogProduct, error) {
			return outbound.CatalogProduct{ProductID: uuid.NewString(), SKU: "SKU-1", Name: "Product 1", Price: 100, Currency: "USD", IsPublished: true}, nil
		},
	}

	svc := NewCartServiceWithCatalog(&stubCartRepository{}, &stubCartItemRepository{}, snapshotRepo, catalogReader)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.New(), SKU: "SKU-1", Quantity: 1})
	require.ErrorContains(t, err, "upsert product snapshot")
	require.ErrorIs(t, err, upsertErr)
	require.Equal(t, domain.Cart{}, got)
	require.Equal(t, 1, catalogReader.getProductBySKUCalls)
	require.Equal(t, 1, snapshotRepo.upsertCalls)
}

func TestAddCartItemFallbackRejectsNonPublishedProduct(t *testing.T) {
	t.Parallel()

	snapshotRepo := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		},
	}

	catalogReader := &stubCatalogReader{
		getProductBySKUFn: func(_ context.Context, _ string) (outbound.CatalogProduct, error) {
			return outbound.CatalogProduct{
				ProductID:   uuid.NewString(),
				SKU:         "SKU-1",
				Name:        "Product 1",
				Price:       100,
				Currency:    "USD",
				IsPublished: false,
			}, nil
		},
	}

	svc := NewCartServiceWithCatalog(&stubCartRepository{}, &stubCartItemRepository{}, snapshotRepo, catalogReader)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.New(), SKU: "SKU-1", Quantity: 1})
	require.ErrorIs(t, err, ErrProductSnapshotNotFound)
	require.Equal(t, domain.Cart{}, got)
	require.Equal(t, 1, catalogReader.getProductBySKUCalls)
	require.Equal(t, 0, snapshotRepo.upsertCalls)
}

func TestAddCartItemFallbackRejectsNilProductID(t *testing.T) {
	t.Parallel()

	snapshotRepo := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		},
	}

	catalogReader := &stubCatalogReader{
		getProductBySKUFn: func(_ context.Context, _ string) (outbound.CatalogProduct, error) {
			return outbound.CatalogProduct{
				ProductID:   uuid.Nil.String(),
				SKU:         "SKU-1",
				Name:        "Product 1",
				Price:       100,
				Currency:    "USD",
				IsPublished: true,
			}, nil
		},
	}

	svc := NewCartServiceWithCatalog(&stubCartRepository{}, &stubCartItemRepository{}, snapshotRepo, catalogReader)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.New(), SKU: "SKU-1", Quantity: 1})
	require.ErrorIs(t, err, ErrInvalidCatalogProductID)
	require.Equal(t, domain.Cart{}, got)
	require.Equal(t, 1, catalogReader.getProductBySKUCalls)
	require.Equal(t, 0, snapshotRepo.upsertCalls)
}

func TestAddCartItemInputAndErrorBranches(t *testing.T) {
	t.Parallel()

	t.Run("nil user id", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.Nil, SKU: "SKU-1", Quantity: 1})
		require.ErrorIs(t, err, ErrInvalidUserID)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("blank sku", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.New(), SKU: "   ", Quantity: 1})
		require.ErrorIs(t, err, ErrInvalidSKU)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("generic snapshot repository failure", func(t *testing.T) {
		t.Parallel()

		repoErr := errors.New("snapshot storage unavailable")
		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{
			getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
				return domain.ProductSnapshot{}, repoErr
			},
		})

		got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: uuid.New(), SKU: "SKU-1", Quantity: 1})
		require.ErrorContains(t, err, "get product snapshot by sku")
		require.ErrorIs(t, err, repoErr)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("insert maps outbound cart not found", func(t *testing.T) {
		t.Parallel()

		userID := uuid.New()
		cartID := uuid.New()
		svc := NewCartService(
			&stubCartRepository{
				getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
					return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
				},
			},
			&stubCartItemRepository{
				insertFn: func(_ context.Context, _ outbound.CartItemInsertParams) (domain.CartItem, error) {
					return domain.CartItem{}, outbound.ErrCartNotFound
				},
			},
			&stubProductSnapshotRepository{
				getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
					now := time.Now().UTC()
					snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 100, "USD", now, now)
					require.NoError(t, err)

					return snapshot, nil
				},
			},
		)

		got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
		require.ErrorIs(t, err, ErrCartNotFound)
		require.NotErrorIs(t, err, outbound.ErrCartNotFound)
		require.Equal(t, domain.Cart{}, got)
	})
}

func TestUpdateCartItemReturnsMissingItem(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()

	svc := NewCartService(
		&stubCartRepository{
			getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
				return domain.Cart{
					ID:       cartID,
					UserID:   userID,
					Status:   domain.CartStatusActive,
					Currency: "USD",
				}, nil
			},
		},
		&stubCartItemRepository{
			listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
				return nil, nil
			},
			updateQuantityFn: func(_ context.Context, gotCartID uuid.UUID, gotSKU string, gotQty int64) (domain.CartItem, error) {
				require.Equal(t, cartID, gotCartID)
				require.Equal(t, "SKU-404", gotSKU)
				require.Equal(t, int64(2), gotQty)

				return domain.CartItem{}, outbound.ErrCartItemNotFound
			},
		},
		&stubProductSnapshotRepository{},
	)

	got, err := svc.UpdateCartItem(context.Background(), UpdateCartItemInput{
		UserID:   userID,
		SKU:      "SKU-404",
		Quantity: 2,
	})
	require.ErrorIs(t, err, ErrCartItemNotFound)
	require.Equal(t, domain.Cart{}, got)
}

func TestRemoveCartItemReturnsMissingItem(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()

	svc := NewCartService(
		&stubCartRepository{
			getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
				return domain.Cart{
					ID:       cartID,
					UserID:   userID,
					Status:   domain.CartStatusActive,
					Currency: "USD",
				}, nil
			},
		},
		&stubCartItemRepository{
			listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
				return nil, nil
			},
			deleteFn: func(_ context.Context, gotCartID uuid.UUID, gotSKU string) error {
				require.Equal(t, cartID, gotCartID)
				require.Equal(t, "SKU-404", gotSKU)

				return outbound.ErrCartItemNotFound
			},
		},
		&stubProductSnapshotRepository{},
	)

	got, err := svc.RemoveCartItem(context.Background(), RemoveCartItemInput{
		UserID: userID,
		SKU:    "SKU-404",
	})
	require.ErrorIs(t, err, ErrCartItemNotFound)
	require.Equal(t, domain.Cart{}, got)
}

func TestAddCartItemUsesExistingCartWithoutCreate(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{
				ID:       cartID,
				UserID:   userID,
				Status:   domain.CartStatusActive,
				Currency: "USD",
			}, nil
		},
	}

	items := &stubCartItemRepository{
		insertFn: func(_ context.Context, _ outbound.CartItemInsertParams) (domain.CartItem, error) {
			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			now := time.Now().UTC()
			item, err := domain.NewCartItem("SKU-1", "Product 1", 1, 100, "USD", now, now)
			require.NoError(t, err)

			return []domain.CartItem{item}, nil
		},
	}

	snapshots := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			now := time.Now().UTC()
			snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 100, "USD", now, now)
			require.NoError(t, err)

			return snapshot, nil
		},
	}

	svc := NewCartService(carts, items, snapshots)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
	require.NoError(t, err)
	require.Equal(t, int64(100), got.TotalAmount)
	require.Equal(t, 0, carts.createActiveCalls)
}

func TestAddCartItemExistingCartCurrencyMismatchPreventsInsert(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	insertCalls := 0

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{
				ID:       cartID,
				UserID:   userID,
				Status:   domain.CartStatusActive,
				Currency: "EUR",
			}, nil
		},
	}

	items := &stubCartItemRepository{
		insertFn: func(_ context.Context, _ outbound.CartItemInsertParams) (domain.CartItem, error) {
			insertCalls++
			return domain.CartItem{}, nil
		},
	}

	snapshots := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			now := time.Now().UTC()
			snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 100, "USD", now, now)
			require.NoError(t, err)

			return snapshot, nil
		},
	}

	svc := NewCartService(carts, items, snapshots)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
	require.ErrorIs(t, err, ErrCartCurrencyMismatch)
	require.Equal(t, domain.Cart{}, got)
	require.Equal(t, 0, insertCalls)
	require.Equal(t, 0, carts.createActiveCalls)
}

func TestAddCartItemReadsCartAfterCreateConflict(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	getCalls := 0

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			getCalls++
			if getCalls == 1 {
				return domain.Cart{}, outbound.ErrCartNotFound
			}

			return domain.Cart{
				ID:       cartID,
				UserID:   userID,
				Status:   domain.CartStatusActive,
				Currency: "USD",
			}, nil
		},
		createActiveFn: func(_ context.Context, _ uuid.UUID, _ string) (domain.Cart, error) {
			return domain.Cart{}, outbound.ErrCartAlreadyExists
		},
	}

	items := &stubCartItemRepository{
		insertFn: func(_ context.Context, _ outbound.CartItemInsertParams) (domain.CartItem, error) {
			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			now := time.Now().UTC()
			item, err := domain.NewCartItem("SKU-1", "Product 1", 1, 100, "USD", now, now)
			require.NoError(t, err)

			return []domain.CartItem{item}, nil
		},
	}

	snapshots := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			now := time.Now().UTC()
			snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 100, "USD", now, now)
			require.NoError(t, err)

			return snapshot, nil
		},
	}

	svc := NewCartService(carts, items, snapshots)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
	require.NoError(t, err)
	require.Equal(t, cartID, got.ID)
	require.Equal(t, 1, carts.createActiveCalls)
	require.Equal(t, 2, carts.getActiveByUserIDCalls)
}

func TestAddCartItemMapsDuplicateItemError(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()

	svc := NewCartService(
		&stubCartRepository{
			getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
				return domain.Cart{
					ID:       cartID,
					UserID:   userID,
					Status:   domain.CartStatusActive,
					Currency: "USD",
				}, nil
			},
		},
		&stubCartItemRepository{
			insertFn: func(_ context.Context, _ outbound.CartItemInsertParams) (domain.CartItem, error) {
				return domain.CartItem{}, outbound.ErrCartItemAlreadyExists
			},
		},
		&stubProductSnapshotRepository{
			getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
				now := time.Now().UTC()
				snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 100, "USD", now, now)
				require.NoError(t, err)

				return snapshot, nil
			},
		},
	)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
	require.ErrorIs(t, err, ErrCartItemAlreadyExists)
	require.NotErrorIs(t, err, outbound.ErrCartItemAlreadyExists)
	require.Equal(t, domain.Cart{}, got)
}

func TestGetActiveCartInputAndGenericErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid user id", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.GetActiveCart(context.Background(), uuid.Nil)
		require.ErrorIs(t, err, ErrInvalidUserID)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("generic carts repo error", func(t *testing.T) {
		t.Parallel()

		repoErr := errors.New("db unavailable")
		svc := NewCartService(
			&stubCartRepository{
				getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
					return domain.Cart{}, repoErr
				},
			},
			&stubCartItemRepository{},
			&stubProductSnapshotRepository{},
		)

		got, err := svc.GetActiveCart(context.Background(), uuid.New())
		require.ErrorContains(t, err, "get active cart")
		require.ErrorIs(t, err, repoErr)
		require.Equal(t, domain.Cart{}, got)
	})
}

func TestUpdateCartItemInputAndGenericErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid user id", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.UpdateCartItem(context.Background(), UpdateCartItemInput{UserID: uuid.Nil, SKU: "SKU-1", Quantity: 1})
		require.ErrorIs(t, err, ErrInvalidUserID)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("invalid sku", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.UpdateCartItem(context.Background(), UpdateCartItemInput{UserID: uuid.New(), SKU: "   ", Quantity: 1})
		require.ErrorIs(t, err, ErrInvalidSKU)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("generic update repo error", func(t *testing.T) {
		t.Parallel()

		userID := uuid.New()
		cartID := uuid.New()
		repoErr := errors.New("update failed")
		svc := NewCartService(
			&stubCartRepository{
				getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
					return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
				},
			},
			&stubCartItemRepository{
				listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) { return nil, nil },
				updateQuantityFn: func(_ context.Context, _ uuid.UUID, _ string, _ int64) (domain.CartItem, error) {
					return domain.CartItem{}, repoErr
				},
			},
			&stubProductSnapshotRepository{},
		)

		got, err := svc.UpdateCartItem(context.Background(), UpdateCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 1})
		require.ErrorContains(t, err, "update cart item quantity")
		require.ErrorIs(t, err, repoErr)
		require.Equal(t, domain.Cart{}, got)
	})
}

func TestRemoveCartItemInputAndGenericErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid user id", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.RemoveCartItem(context.Background(), RemoveCartItemInput{UserID: uuid.Nil, SKU: "SKU-1"})
		require.ErrorIs(t, err, ErrInvalidUserID)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("invalid sku", func(t *testing.T) {
		t.Parallel()

		svc := NewCartService(&stubCartRepository{}, &stubCartItemRepository{}, &stubProductSnapshotRepository{})

		got, err := svc.RemoveCartItem(context.Background(), RemoveCartItemInput{UserID: uuid.New(), SKU: ""})
		require.ErrorIs(t, err, ErrInvalidSKU)
		require.Equal(t, domain.Cart{}, got)
	})

	t.Run("generic delete repo error", func(t *testing.T) {
		t.Parallel()

		userID := uuid.New()
		cartID := uuid.New()
		repoErr := errors.New("delete failed")
		svc := NewCartService(
			&stubCartRepository{
				getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
					return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
				},
			},
			&stubCartItemRepository{
				listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) { return nil, nil },
				deleteFn: func(_ context.Context, _ uuid.UUID, _ string) error {
					return repoErr
				},
			},
			&stubProductSnapshotRepository{},
		)

		got, err := svc.RemoveCartItem(context.Background(), RemoveCartItemInput{UserID: userID, SKU: "SKU-1"})
		require.ErrorContains(t, err, "delete cart item")
		require.ErrorIs(t, err, repoErr)
		require.Equal(t, domain.Cart{}, got)
	})
}

func TestGetActiveCartReturnsCachedCart(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	item, err := domain.NewCartItem("SKU-1", "Item 1", 2, 500, "USD", now, now)
	require.NoError(t, err)

	cachedCart := domain.Cart{
		ID:          cartID,
		UserID:      userID,
		Status:      domain.CartStatusActive,
		Currency:    "USD",
		Items:       []domain.CartItem{item},
		TotalAmount: 1000,
	}

	carts := &stubCartRepository{}
	items := &stubCartItemRepository{}
	cache := &stubCartCache{
		getActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, bool, error) {
			require.Equal(t, userID, gotUserID)
			return cachedCart, true, nil
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	got, err := svc.GetActiveCart(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, cachedCart, got)
	require.Equal(t, 0, carts.getActiveByUserIDCalls)
	require.Equal(t, 0, cache.setActiveByUserIDCalls)
}

func TestGetActiveCartInvalidCachedPayloadFallsBackToStorage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		cachedCart domain.Cart
	}{
		{
			name: "cached cart belongs to another user",
			cachedCart: domain.Cart{
				ID:       uuid.New(),
				UserID:   uuid.New(),
				Status:   domain.CartStatusActive,
				Currency: "USD",
				Items:    []domain.CartItem{},
			},
		},
		{
			name: "cached cart is not active",
			cachedCart: domain.Cart{
				ID:       uuid.New(),
				Status:   domain.CartStatusCheckedOut,
				Currency: "USD",
				Items:    []domain.CartItem{},
			},
		},
		{
			name: "cached cart has invalid item",
			cachedCart: domain.Cart{
				ID:       uuid.New(),
				Status:   domain.CartStatusActive,
				Currency: "USD",
				Items: []domain.CartItem{
					{
						SKU:       "SKU-1",
						Name:      "Product 1",
						Quantity:  0,
						UnitPrice: 500,
						Currency:  "USD",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			cartID := uuid.New()
			now := time.Now().UTC()

			storageItem, err := domain.NewCartItem("SKU-1", "Product 1", 2, 500, "USD", now, now)
			require.NoError(t, err)

			carts := &stubCartRepository{
				getActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, error) {
					require.Equal(t, userID, gotUserID)

					return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
				},
			}

			items := &stubCartItemRepository{
				listByCartIDFn: func(_ context.Context, gotCartID uuid.UUID) ([]domain.CartItem, error) {
					require.Equal(t, cartID, gotCartID)

					return []domain.CartItem{storageItem}, nil
				},
			}

			cache := &stubCartCache{
				getActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, bool, error) {
					require.Equal(t, userID, gotUserID)
					cachedCart := tc.cachedCart
					if cachedCart.UserID == uuid.Nil {
						cachedCart.UserID = userID
					}

					return cachedCart, true, nil
				},
			}

			svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

			got, err := svc.GetActiveCart(context.Background(), userID)
			require.NoError(t, err)
			require.Equal(t, int64(1000), got.TotalAmount)
			require.Equal(t, 1, carts.getActiveByUserIDCalls)
			require.Equal(t, 1, cache.setActiveByUserIDCalls)
		})
	}
}

func TestGetActiveCartHydratesFromStorageOnCacheMissAndSetsCache(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	item, err := domain.NewCartItem("SKU-1", "Item 1", 3, 200, "USD", now, now)
	require.NoError(t, err)

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	items := &stubCartItemRepository{
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			item.LineTotal = 0
			return []domain.CartItem{item}, nil
		},
	}

	cache := &stubCartCache{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, bool, error) {
			return domain.Cart{}, false, nil
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	got, err := svc.GetActiveCart(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, int64(600), got.TotalAmount)
	require.Equal(t, 1, cache.setActiveByUserIDCalls)
	require.Equal(t, got, cache.lastSetCart)
	require.Equal(t, userID, cache.lastSetUserID)
}

func TestGetActiveCartReturnsEmptyCartWhenStorageMissAndCachesIt(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{}, outbound.ErrCartNotFound
		},
	}

	cache := &stubCartCache{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, bool, error) {
			return domain.Cart{}, false, nil
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, &stubCartItemRepository{}, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	got, err := svc.GetActiveCart(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, got.ID)
	require.Equal(t, userID, got.UserID)
	require.Equal(t, domain.CartStatusActive, got.Status)
	require.Equal(t, int64(0), got.TotalAmount)
	require.Len(t, got.Items, 0)
	require.Equal(t, 1, cache.setActiveByUserIDCalls)
	require.Equal(t, got, cache.lastSetCart)
}

func TestGetActiveCartCacheFailuresDoNotFailRequest(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	item, err := domain.NewCartItem("SKU-1", "Item 1", 1, 300, "USD", now, now)
	require.NoError(t, err)

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	items := &stubCartItemRepository{
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			return []domain.CartItem{item}, nil
		},
	}

	cache := &stubCartCache{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, bool, error) {
			return domain.Cart{}, false, errors.New("redis unavailable")
		},
		setActiveByUserIDFn: func(_ context.Context, _ uuid.UUID, _ domain.Cart, _ time.Duration) error {
			return errors.New("redis timeout")
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	got, err := svc.GetActiveCart(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, int64(300), got.TotalAmount)
	require.Equal(t, 1, cache.setActiveByUserIDCalls)
}

func TestAddCartItemRefreshesCacheAfterWrite(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	snapshot, err := domain.NewProductSnapshot("SKU-1", nil, "Product 1", 700, "USD", now, now)
	require.NoError(t, err)

	item, err := domain.NewCartItem("SKU-1", "Product 1", 2, 700, "USD", now, now)
	require.NoError(t, err)

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	items := &stubCartItemRepository{
		insertFn: func(_ context.Context, _ outbound.CartItemInsertParams) (domain.CartItem, error) {
			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			return []domain.CartItem{item}, nil
		},
	}

	snapshots := &stubProductSnapshotRepository{
		getBySKUFn: func(_ context.Context, _ string) (domain.ProductSnapshot, error) {
			return snapshot, nil
		},
	}

	cache := &stubCartCache{
		setActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID, gotCart domain.Cart, _ time.Duration) error {
			require.Equal(t, userID, gotUserID)
			require.Equal(t, int64(1400), gotCart.TotalAmount)
			return nil
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, items, snapshots, nil, cache, time.Minute)

	got, err := svc.AddCartItem(context.Background(), AddCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 2})
	require.NoError(t, err)
	require.Equal(t, int64(1400), got.TotalAmount)
	require.Equal(t, 1, cache.setActiveByUserIDCalls)
}

func TestUpdateCartItemRefreshesCacheAfterWrite(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	cachedItem, err := domain.NewCartItem("SKU-1", "Product 1", 1, 500, "USD", now, now)
	require.NoError(t, err)

	updatedItem, err := domain.NewCartItem("SKU-1", "Product 1", 2, 500, "USD", now, now)
	require.NoError(t, err)

	cachedCart := domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD", Items: []domain.CartItem{cachedItem}, TotalAmount: 500}

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	items := &stubCartItemRepository{
		updateQuantityFn: func(_ context.Context, gotCartID uuid.UUID, gotSKU string, gotQty int64) (domain.CartItem, error) {
			require.Equal(t, cartID, gotCartID)
			require.Equal(t, "SKU-1", gotSKU)
			require.Equal(t, int64(2), gotQty)
			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			return []domain.CartItem{updatedItem}, nil
		},
	}

	cache := &stubCartCache{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, bool, error) {
			return cachedCart, true, nil
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	got, err := svc.UpdateCartItem(context.Background(), UpdateCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 2})
	require.NoError(t, err)
	require.Equal(t, int64(1000), got.TotalAmount)
	require.Equal(t, 1, cache.setActiveByUserIDCalls)
	require.Equal(t, int64(1000), cache.lastSetCart.TotalAmount)
}

func TestUpdateCartItemSetCacheFailureDeletesStaleCacheAndNextReadFallsBackToStorage(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	staleItem, err := domain.NewCartItem("SKU-1", "Product 1", 1, 500, "USD", now, now)
	require.NoError(t, err)

	updatedItem, err := domain.NewCartItem("SKU-1", "Product 1", 2, 500, "USD", now, now)
	require.NoError(t, err)

	staleCart := domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD", Items: []domain.CartItem{staleItem}, TotalAmount: 500}

	itemsListCalls := 0
	items := &stubCartItemRepository{
		updateQuantityFn: func(_ context.Context, gotCartID uuid.UUID, gotSKU string, gotQty int64) (domain.CartItem, error) {
			require.Equal(t, cartID, gotCartID)
			require.Equal(t, "SKU-1", gotSKU)
			require.Equal(t, int64(2), gotQty)

			return domain.CartItem{}, nil
		},
		listByCartIDFn: func(_ context.Context, gotCartID uuid.UUID) ([]domain.CartItem, error) {
			require.Equal(t, cartID, gotCartID)
			itemsListCalls++

			if itemsListCalls == 1 {
				return []domain.CartItem{staleItem}, nil
			}

			return []domain.CartItem{updatedItem}, nil
		},
	}

	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, error) {
			require.Equal(t, userID, gotUserID)

			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	hasStaleCache := true
	cache := &stubCartCache{
		getActiveByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) (domain.Cart, bool, error) {
			require.Equal(t, userID, gotUserID)
			if hasStaleCache {
				return staleCart, true, nil
			}

			return domain.Cart{}, false, nil
		},
		setActiveByUserIDFn: func(_ context.Context, _ uuid.UUID, _ domain.Cart, _ time.Duration) error {
			return errors.New("set cache failed")
		},
		deleteByUserIDFn: func(_ context.Context, gotUserID uuid.UUID) error {
			require.Equal(t, userID, gotUserID)
			hasStaleCache = false

			return nil
		},
	}

	svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	updated, err := svc.UpdateCartItem(context.Background(), UpdateCartItemInput{UserID: userID, SKU: "SKU-1", Quantity: 2})
	require.NoError(t, err)
	require.Equal(t, int64(1000), updated.TotalAmount)
	require.Equal(t, 1, cache.deleteActiveByUserIDCalls)

	got, err := svc.GetActiveCart(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, int64(1000), got.TotalAmount)
	require.Equal(t, 3, carts.getActiveByUserIDCalls)
}

func TestRemoveCartItemRefreshesCacheAfterWrite(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	cartID := uuid.New()
	now := time.Now().UTC()

	item, err := domain.NewCartItem("SKU-1", "Product 1", 1, 500, "USD", now, now)
	require.NoError(t, err)

	listCalls := 0
	carts := &stubCartRepository{
		getActiveByUserIDFn: func(_ context.Context, _ uuid.UUID) (domain.Cart, error) {
			return domain.Cart{ID: cartID, UserID: userID, Status: domain.CartStatusActive, Currency: "USD"}, nil
		},
	}

	items := &stubCartItemRepository{
		listByCartIDFn: func(_ context.Context, _ uuid.UUID) ([]domain.CartItem, error) {
			listCalls++
			if listCalls == 1 {
				return []domain.CartItem{item}, nil
			}

			return []domain.CartItem{}, nil
		},
		deleteFn: func(_ context.Context, gotCartID uuid.UUID, gotSKU string) error {
			require.Equal(t, cartID, gotCartID)
			require.Equal(t, "SKU-1", gotSKU)
			return nil
		},
	}

	cache := &stubCartCache{}

	svc := NewCartServiceWithCatalogAndCache(carts, items, &stubProductSnapshotRepository{}, nil, cache, time.Minute)

	got, err := svc.RemoveCartItem(context.Background(), RemoveCartItemInput{UserID: userID, SKU: "SKU-1"})
	require.NoError(t, err)
	require.Equal(t, int64(0), got.TotalAmount)
	require.Len(t, got.Items, 0)
	require.Equal(t, 1, cache.setActiveByUserIDCalls)
}

type stubCartRepository struct {
	getActiveByUserIDFn func(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
	createActiveFn      func(ctx context.Context, userID uuid.UUID, currency string) (domain.Cart, error)

	createActiveCalls      int
	getActiveByUserIDCalls int
}

func (s *stubCartRepository) GetActiveByUserID(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	if s.getActiveByUserIDFn == nil {
		return domain.Cart{}, errors.New("unexpected GetActiveByUserID call")
	}

	s.getActiveByUserIDCalls++

	return s.getActiveByUserIDFn(ctx, userID)
}

func (s *stubCartRepository) CreateActive(ctx context.Context, userID uuid.UUID, currency string) (domain.Cart, error) {
	if s.createActiveFn == nil {
		return domain.Cart{}, errors.New("unexpected CreateActive call")
	}

	s.createActiveCalls++

	return s.createActiveFn(ctx, userID, currency)
}

type stubCartItemRepository struct {
	listByCartIDFn   func(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error)
	insertFn         func(ctx context.Context, params outbound.CartItemInsertParams) (domain.CartItem, error)
	updateQuantityFn func(ctx context.Context, cartID uuid.UUID, sku string, quantity int64) (domain.CartItem, error)
	deleteFn         func(ctx context.Context, cartID uuid.UUID, sku string) error
}

func (s *stubCartItemRepository) ListByCartID(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error) {
	if s.listByCartIDFn == nil {
		return nil, errors.New("unexpected ListByCartID call")
	}

	return s.listByCartIDFn(ctx, cartID)
}

func (s *stubCartItemRepository) Insert(ctx context.Context, params outbound.CartItemInsertParams) (domain.CartItem, error) {
	if s.insertFn == nil {
		return domain.CartItem{}, errors.New("unexpected Insert call")
	}

	return s.insertFn(ctx, params)
}

func (s *stubCartItemRepository) UpdateQuantity(ctx context.Context, cartID uuid.UUID, sku string, quantity int64) (domain.CartItem, error) {
	if s.updateQuantityFn == nil {
		return domain.CartItem{}, errors.New("unexpected UpdateQuantity call")
	}

	return s.updateQuantityFn(ctx, cartID, sku, quantity)
}

func (s *stubCartItemRepository) Delete(ctx context.Context, cartID uuid.UUID, sku string) error {
	if s.deleteFn == nil {
		return errors.New("unexpected Delete call")
	}

	return s.deleteFn(ctx, cartID, sku)
}

type stubProductSnapshotRepository struct {
	getBySKUFn func(ctx context.Context, sku string) (domain.ProductSnapshot, error)
	upsertFn   func(ctx context.Context, snapshot domain.ProductSnapshot) (domain.ProductSnapshot, error)

	upsertCalls int
}

func (s *stubProductSnapshotRepository) GetBySKU(ctx context.Context, sku string) (domain.ProductSnapshot, error) {
	if s.getBySKUFn == nil {
		return domain.ProductSnapshot{}, errors.New("unexpected GetBySKU call")
	}

	return s.getBySKUFn(ctx, sku)
}

func (s *stubProductSnapshotRepository) Upsert(ctx context.Context, snapshot domain.ProductSnapshot) (domain.ProductSnapshot, error) {
	if s.upsertFn == nil {
		return domain.ProductSnapshot{}, errors.New("unexpected Upsert call")
	}

	s.upsertCalls++

	return s.upsertFn(ctx, snapshot)
}

type stubCatalogReader struct {
	getProductBySKUFn    func(ctx context.Context, sku string) (outbound.CatalogProduct, error)
	getProductBySKUCalls int
}

func (s *stubCatalogReader) GetProductBySKU(ctx context.Context, sku string) (outbound.CatalogProduct, error) {
	if s.getProductBySKUFn == nil {
		return outbound.CatalogProduct{}, errors.New("unexpected GetProductBySKU call")
	}

	s.getProductBySKUCalls++

	return s.getProductBySKUFn(ctx, sku)
}

type stubCartCache struct {
	getActiveByUserIDFn func(ctx context.Context, userID uuid.UUID) (domain.Cart, bool, error)
	setActiveByUserIDFn func(ctx context.Context, userID uuid.UUID, cart domain.Cart, ttl time.Duration) error
	deleteByUserIDFn    func(ctx context.Context, userID uuid.UUID) error

	setActiveByUserIDCalls    int
	deleteActiveByUserIDCalls int
	lastSetUserID             uuid.UUID
	lastSetCart               domain.Cart
	lastSetTTL                time.Duration
	lastDeletedUserID         uuid.UUID
}

func (s *stubCartCache) GetActiveByUserID(ctx context.Context, userID uuid.UUID) (domain.Cart, bool, error) {
	if s.getActiveByUserIDFn == nil {
		return domain.Cart{}, false, nil
	}

	return s.getActiveByUserIDFn(ctx, userID)
}

func (s *stubCartCache) SetActiveByUserID(ctx context.Context, userID uuid.UUID, cart domain.Cart, ttl time.Duration) error {
	s.setActiveByUserIDCalls++
	s.lastSetUserID = userID
	s.lastSetCart = cart
	s.lastSetTTL = ttl

	if s.setActiveByUserIDFn == nil {
		return nil
	}

	return s.setActiveByUserIDFn(ctx, userID, cart, ttl)
}

func (s *stubCartCache) DeleteActiveByUserID(ctx context.Context, userID uuid.UUID) error {
	s.deleteActiveByUserIDCalls++
	s.lastDeletedUserID = userID

	if s.deleteByUserIDFn == nil {
		return nil
	}

	return s.deleteByUserIDFn(ctx, userID)
}
