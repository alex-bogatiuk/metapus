package handlers

import (
	"github.com/gin-gonic/gin"
	"metapus/internal/application/exchangerateapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	er "metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/http/v1/dto"
	"strconv"
	"time"
)

type ExchangeRatesHandler struct {
	*BaseHandler
	service *exchangerateapp.RegisterService
}

func NewExchangeRatesHandler(service *exchangerateapp.RegisterService) *ExchangeRatesHandler {
	return &ExchangeRatesHandler{NewBaseHandler(), service}
}
func rateID(value string) (*id.ID, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := id.Parse(value)
	if err != nil || id.IsNil(parsed) {
		return nil, apperror.NewValidation("Некорректный UUID")
	}
	return &parsed, nil
}
func rateTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, apperror.NewValidation("Время должно быть в формате RFC3339")
	}
	t = t.UTC()
	return &t, nil
}
func (h *ExchangeRatesHandler) Current(c *gin.Context) {
	currency, err := rateID(c.Query("currencyId"))
	if err != nil {
		h.Error(c, err)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		h.Error(c, apperror.NewValidation("Некорректный размер страницы"))
		return
	}
	out, err := h.service.Current(c.Request.Context(), exchangerateapp.CurrentRequest{CurrencyID: currency, Status: er.Status(c.Query("status")), Cursor: c.Query("cursor"), Limit: limit})
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, out)
}
func (h *ExchangeRatesHandler) History(c *gin.Context) {
	currency, err := rateID(c.Query("currencyId"))
	if err != nil {
		h.Error(c, err)
		return
	}
	if currency == nil {
		h.Error(c, apperror.NewValidation("Укажите валюту"))
		return
	}
	base, err := rateID(c.Query("baseCurrencyId"))
	if err != nil {
		h.Error(c, err)
		return
	}
	source, err := rateID(c.Query("sourceId"))
	if err != nil {
		h.Error(c, err)
		return
	}
	from, err := rateTime(c.Query("from"))
	if err != nil {
		h.Error(c, err)
		return
	}
	to, err := rateTime(c.Query("to"))
	if err != nil {
		h.Error(c, err)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		h.Error(c, apperror.NewValidation("Некорректный размер страницы"))
		return
	}
	out, err := h.service.History(c.Request.Context(), exchangerateapp.HistoryRequest{CurrencyID: *currency, BaseCurrencyID: base, SourceID: source, From: from, To: to, Cursor: c.Query("cursor"), Limit: limit})
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, out)
}
func (h *ExchangeRatesHandler) pathID(c *gin.Context, key string) *id.ID {
	value, err := rateID(c.Param(key))
	if err != nil {
		h.Error(c, err)
	}
	return value
}
func (h *ExchangeRatesHandler) Quote(c *gin.Context) {
	value := h.pathID(c, "id")
	if value == nil {
		return
	}
	out, err := h.service.Quote(c.Request.Context(), *value)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, out)
}
func (h *ExchangeRatesHandler) Mappings(c *gin.Context) {
	value := h.pathID(c, "id")
	if value == nil {
		return
	}
	out, err := h.service.Mappings(c.Request.Context(), *value)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, out)
}
func (h *ExchangeRatesHandler) FeedState(c *gin.Context) {
	value := h.pathID(c, "id")
	if value == nil {
		return
	}
	out, err := h.service.FeedState(c.Request.Context(), *value)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, out)
}
func (h *ExchangeRatesHandler) PutMapping(c *gin.Context) {
	source := h.pathID(c, "id")
	if source == nil {
		return
	}
	currency := h.pathID(c, "currencyId")
	if currency == nil {
		return
	}
	base := h.pathID(c, "baseCurrencyId")
	if base == nil {
		return
	}
	var body dto.UpdateRateMappingRequest
	if !h.BindJSON(c, &body) {
		return
	}
	out, err := h.service.PutMapping(c.Request.Context(), er.Mapping{Currency: er.CurrencyRef{ID: *currency}, BaseCurrency: er.CurrencyRef{ID: *base}, SourceID: *source, ExternalID: body.ExternalID, IsActive: *body.IsActive}, *body.ExpectedVersion)
	if err != nil {
		h.Error(c, err)
		return
	}
	h.OK(c, out)
}
