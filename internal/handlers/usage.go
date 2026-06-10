package handlers

import (
	"net/http"

	"github.com/afahey03/docklab/internal/services"
	"github.com/gin-gonic/gin"
)

type UsageHandler struct {
	usageService   *services.UsageService
	pricingService *services.PricingService
}

type UpdateBudgetRequest struct {
	MonthlyBudgetUSD    float64 `json:"monthly_budget_usd"`
	BudgetAlertsEnabled bool    `json:"budget_alerts_enabled"`
}

func NewUsageHandler(usageService *services.UsageService, pricingService *services.PricingService) *UsageHandler {
	return &UsageHandler{
		usageService:   usageService,
		pricingService: pricingService,
	}
}

// GetUsage returns the user's usage session history with totals.
func (h *UsageHandler) GetUsage(c *gin.Context) {
	userEmail := c.GetString("user_email")
	summary, err := h.usageService.GetUsageSummary(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load usage history"})
		return
	}

	c.JSON(http.StatusOK, summary)
}

// GetBillingSummary returns the current month's per-environment cost rollup and budget
// state.
func (h *UsageHandler) GetBillingSummary(c *gin.Context) {
	userEmail := c.GetString("user_email")
	summary, err := h.usageService.GetBillingSummary(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load billing summary"})
		return
	}

	c.JSON(http.StatusOK, summary)
}

func (h *UsageHandler) GetBudget(c *gin.Context) {
	userEmail := c.GetString("user_email")
	settings, err := h.usageService.GetSettings(c.Request.Context(), userEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load budget settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

func (h *UsageHandler) UpdateBudget(c *gin.Context) {
	var req UpdateBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	userEmail := c.GetString("user_email")
	settings, err := h.usageService.UpdateSettings(c.Request.Context(), userEmail, req.MonthlyBudgetUSD, req.BudgetAlertsEnabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update budget settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

// GetPricing resolves the hourly on-demand rate for an instance type and region.
func (h *UsageHandler) GetPricing(c *gin.Context) {
	instanceType := c.Query("instance_type")
	region := c.Query("region")
	if instanceType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instance_type query parameter is required"})
		return
	}

	rate := h.pricingService.GetRate(c.Request.Context(), instanceType, region)
	c.JSON(http.StatusOK, rate)
}
