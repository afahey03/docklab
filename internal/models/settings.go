package models

import "time"

// UserSettings holds per-user preferences such as cost budgets.
type UserSettings struct {
	UserEmail           string    `json:"user_email"`
	MonthlyBudgetUSD    float64   `json:"monthly_budget_usd"`
	BudgetAlertsEnabled bool      `json:"budget_alerts_enabled"`
	UpdatedAt           time.Time `json:"updated_at"`
}
