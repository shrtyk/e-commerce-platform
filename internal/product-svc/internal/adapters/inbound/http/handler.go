package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

const (
	defaultPublishedListLimit int32 = 100
	defaultPatchMaxBodyBytes  int64 = 1 << 20
)

type CatalogHandlerConfig struct {
	PublishedListLimit   int32
	PatchMaxBodySizeByte int64
}

type catalogService interface {
	CreateProduct(ctx context.Context, input catalog.CreateProductInput) (catalog.CreateProductResult, error)
	GetProduct(ctx context.Context, productID uuid.UUID) (catalog.GetProductResult, error)
	ListProducts(ctx context.Context, params outbound.ProductListParams) ([]domain.Product, error)
	UpdateProduct(ctx context.Context, input catalog.UpdateProductInput) (catalog.UpdateProductResult, error)
}

type CatalogHandler struct {
	dto.Unimplemented

	catalogService catalogService
	config         CatalogHandlerConfig
}

func NewCatalogHandler(catalogService catalogService, configs ...CatalogHandlerConfig) *CatalogHandler {
	cfg := CatalogHandlerConfig{
		PublishedListLimit:   defaultPublishedListLimit,
		PatchMaxBodySizeByte: defaultPatchMaxBodyBytes,
	}

	if len(configs) > 0 {
		if configs[0].PublishedListLimit > 0 {
			cfg.PublishedListLimit = configs[0].PublishedListLimit
		}
		if configs[0].PatchMaxBodySizeByte > 0 {
			cfg.PatchMaxBodySizeByte = configs[0].PatchMaxBodySizeByte
		}
	}

	return &CatalogHandler{catalogService: catalogService, config: cfg}
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

	if result.Product.Status != domain.ProductStatusPublished {
		h.writeError(w, r, commonerrors.NotFound("product_not_found", "product not found"))
		return
	}

	h.writeJSON(w, http.StatusOK, toDTOProduct(result.Product))
}

func (h *CatalogHandler) ListPublishedProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.catalogService.ListProducts(r.Context(), outbound.ProductListParams{Limit: h.config.PublishedListLimit})
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

func (h *CatalogHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var request dto.CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	input, err := toCreateProductInput(request)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	result, err := h.catalogService.CreateProduct(r.Context(), input)
	if err != nil {
		h.writeError(w, r, mapServiceError(err))
		return
	}

	h.writeJSON(w, http.StatusCreated, toDTOProductWriteResponse(result.Product, result.Stock))
}

func (h *CatalogHandler) UpdateProductById(w http.ResponseWriter, r *http.Request, productID dto.ProductId) {
	id, err := uuid.Parse(productID)
	if err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid product id"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, h.config.PatchMaxBodySizeByte+1))
	if err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	if int64(len(body)) > h.config.PatchMaxBodySizeByte {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	if rawCategoryID, ok := raw["categoryId"]; ok {
		if string(rawCategoryID) == "null" {
			h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid category id"))
			return
		}

		var categoryID string
		if err := json.Unmarshal(rawCategoryID, &categoryID); err != nil {
			h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid category id"))
			return
		}

		if strings.TrimSpace(categoryID) == "" {
			h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid category id"))
			return
		}
	}

	var request dto.UpdateProductRequest
	if err := json.Unmarshal(body, &request); err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request body"))
		return
	}

	input, err := toUpdateProductInput(id, request)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	result, err := h.catalogService.UpdateProduct(r.Context(), input)
	if err != nil {
		h.writeError(w, r, mapServiceError(err))
		return
	}

	h.writeJSON(w, http.StatusOK, toDTOProductWriteResponse(result.Product, result.Stock))
}

func (h *CatalogHandler) DeleteProductById(w http.ResponseWriter, r *http.Request, productID dto.ProductId) {
	id, err := uuid.Parse(productID)
	if err != nil {
		h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid product id"))
		return
	}

	status := domain.ProductStatusArchived
	result, err := h.catalogService.UpdateProduct(r.Context(), catalog.UpdateProductInput{ID: id, Status: &status})
	if err != nil {
		h.writeError(w, r, mapServiceError(err))
		return
	}

	h.writeJSON(w, http.StatusOK, toDTOProductWriteResponse(result.Product, result.Stock))
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
	case errors.Is(err, outbound.ErrProductAlreadyExists):
		return commonerrors.Conflict("product_already_exists", "product already exists")
	case errors.Is(err, outbound.ErrStockRecordNotFound):
		return commonerrors.NotFound("stock_not_found", "stock record not found")
	case errors.Is(err, catalog.ErrInvalidStockInput),
		errors.Is(err, catalog.ErrInvalidCreateProductInput),
		errors.Is(err, catalog.ErrInvalidUpdateProductInput),
		errors.Is(err, outbound.ErrInvalidCurrency),
		errors.Is(err, outbound.ErrInvalidStockUpdate):
		return commonerrors.BadRequest("invalid_request", "invalid request")
	default:
		return commonerrors.InternalError("internal_error")
	}
}

func toCreateProductInput(request dto.CreateProductRequest) (catalog.CreateProductInput, error) {
	currencyCode := strings.TrimSpace(request.CurrencyCode)
	if currencyCode == "" {
		return catalog.CreateProductInput{}, commonerrors.BadRequest("invalid_request", "invalid currency")
	}

	categoryID, err := parseOptionalOpenAPIUUID(request.CategoryId)
	if err != nil {
		return catalog.CreateProductInput{}, commonerrors.BadRequest("invalid_request", "invalid category id")
	}

	status := domain.ProductStatusUnknown
	if request.Status != nil {
		status = domain.ProductStatus(*request.Status)
	}

	description := ""
	if request.Description != nil {
		description = strings.TrimSpace(*request.Description)
	}

	return catalog.CreateProductInput{
		Product: domain.Product{
			SKU:         strings.TrimSpace(request.Sku),
			Name:        strings.TrimSpace(request.Name),
			Description: description,
			Price:       int64(request.Price),
			Currency:    currencyCode,
			CategoryID:  categoryID,
			Status:      status,
		},
		InitialQuantity: int32(request.InitialQuantity),
	}, nil
}

func toUpdateProductInput(id uuid.UUID, request dto.UpdateProductRequest) (catalog.UpdateProductInput, error) {
	input := catalog.UpdateProductInput{ID: id}

	if request.Sku != nil {
		sku := strings.TrimSpace(*request.Sku)
		input.SKU = &sku
	}

	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		input.Name = &name
	}

	if request.Description != nil {
		description := strings.TrimSpace(*request.Description)
		input.Description = &description
	}

	if request.Price != nil {
		price := int64(*request.Price)
		input.Price = &price
	}

	if request.CategoryId != nil {
		categoryID, err := parseOptionalOpenAPIUUID(request.CategoryId)
		if err != nil {
			return catalog.UpdateProductInput{}, commonerrors.BadRequest("invalid_request", "invalid category id")
		}
		input.CategoryID = categoryID
	}

	if request.Status != nil {
		status := domain.ProductStatus(*request.Status)
		input.Status = &status
	}

	return input, nil
}

func parseOptionalOpenAPIUUID(value *openapi_types.UUID) (*uuid.UUID, error) {
	if value == nil {
		return nil, nil
	}

	parsed := uuid.UUID(*value)

	if parsed == uuid.Nil {
		return nil, errors.New("uuid is nil")
	}

	return &parsed, nil
}

func toDTOProductWriteResponse(product domain.Product, stock domain.StockRecord) dto.ProductWriteResponse {
	return dto.ProductWriteResponse{
		Product: toDTOProduct(product),
		Stock: dto.StockSummary{
			Quantity:  int(stock.Quantity),
			Reserved:  int(stock.Reserved),
			Available: int(stock.Available),
			Status:    dto.StockSummaryStatus(stock.Status),
		},
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
		categoryID := openapi_types.UUID(*product.CategoryID)
		response.CategoryId = &categoryID
	}

	return response
}

func toDTOProductStatus(status domain.ProductStatus) dto.ProductStatus {
	switch status {
	case domain.ProductStatusDraft:
		return dto.ProductStatusDraft
	case domain.ProductStatusPublished:
		return dto.ProductStatusPublished
	case domain.ProductStatusArchived:
		return dto.ProductStatusArchived
	default:
		return dto.ProductStatusDraft
	}
}
