package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

const defaultPublishedListLimit int32 = 100

type catalogService interface {
	GetProduct(ctx context.Context, productID uuid.UUID) (catalog.GetProductResult, error)
	ListProducts(ctx context.Context, params outbound.ProductListParams) ([]domain.Product, error)
}

type CatalogHandler struct {
	dto.Unimplemented

	catalogService catalogService
}

func NewCatalogHandler(catalogService catalogService) *CatalogHandler {
	return &CatalogHandler{catalogService: catalogService}
}

func (h *CatalogHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *CatalogHandler) GetProductById(w http.ResponseWriter, r *http.Request, productID dto.ProductId) {
	id, err := uuid.Parse(productID)
	if err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid product id"))
		return
	}

	result, err := h.catalogService.GetProduct(r.Context(), id)
	if err != nil {
		h.writeError(w, r, mapServiceError(err))
		return
	}

	h.writeJSON(w, http.StatusOK, toDTOProduct(result.Product))
}

func (h *CatalogHandler) ListPublishedProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.catalogService.ListProducts(r.Context(), outbound.ProductListParams{Limit: defaultPublishedListLimit})
	if err != nil {
		h.writeError(w, r, mapServiceError(err))
		return
	}

	response := dto.ProductListResponse{Items: make([]dto.Product, 0, len(products))}
	for _, product := range products {
		if product.Status != domain.ProductStatusPublished {
			continue
		}

		response.Items = append(response.Items, toDTOProduct(product))
	}

	h.writeJSON(w, http.StatusOK, response)
}

func (h *CatalogHandler) HandleOpenAPIError(w http.ResponseWriter, r *http.Request, _ error) {
	h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request parameters"))
}

func (h *CatalogHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpErr := commonerrors.FromError(err)
	commonerrors.WriteJSON(w, httpErr, transport.RequestIDFromContext(r.Context()))
}

func (h *CatalogHandler) writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func mapServiceError(err error) error {
	switch {
	case errors.Is(err, outbound.ErrProductNotFound):
		return commonerrors.NotFound("product_not_found", "product not found")
	case errors.Is(err, outbound.ErrStockRecordNotFound):
		return commonerrors.NotFound("stock_not_found", "stock record not found")
	case errors.Is(err, catalog.ErrInvalidStockInput),
		errors.Is(err, catalog.ErrInvalidCreateProductInput),
		errors.Is(err, catalog.ErrInvalidUpdateProductInput),
		errors.Is(err, outbound.ErrInvalidStockUpdate):
		return commonerrors.BadRequest("invalid_request", "invalid request")
	default:
		return commonerrors.InternalError("internal_error")
	}
}

func toDTOProduct(product domain.Product) dto.Product {
	response := dto.Product{
		ProductId: product.ID.String(),
		Sku:       product.SKU,
		Name:      product.Name,
		Price:     int(product.Price),
		Currency:  product.Currency,
		Status:    toDTOProductStatus(product.Status),
	}

	if product.Description != "" {
		description := product.Description
		response.Description = &description
	}

	if product.CategoryID != nil {
		categoryID := product.CategoryID.String()
		response.CategoryId = &categoryID
	}

	return response
}

func toDTOProductStatus(status domain.ProductStatus) dto.ProductStatus {
	switch status {
	case domain.ProductStatusDraft:
		return dto.Draft
	case domain.ProductStatusPublished:
		return dto.Published
	case domain.ProductStatusArchived:
		return dto.Archived
	default:
		return dto.Draft
	}
}
