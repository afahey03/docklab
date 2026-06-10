package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestPricingFallbackRates(t *testing.T) {
	svc := NewPricingService(false, slog.Default())

	rate := svc.GetRate(context.Background(), "t3.micro", "us-east-1")
	if rate.Source != "static_fallback" {
		t.Fatalf("expected static fallback source, got %s", rate.Source)
	}
	if rate.HourlyRateUSD != fallbackHourlyRatesUSD["t3.micro"] {
		t.Fatalf("expected fallback rate %f, got %f", fallbackHourlyRatesUSD["t3.micro"], rate.HourlyRateUSD)
	}
}

func TestPricingUnknownInstanceTypeIsZero(t *testing.T) {
	svc := NewPricingService(false, slog.Default())

	rate := svc.GetRate(context.Background(), "z99.mega", "us-east-1")
	if rate.HourlyRateUSD != 0 {
		t.Fatalf("expected zero rate for unknown type, got %f", rate.HourlyRateUSD)
	}
}

func TestPricingAPIRateIsCached(t *testing.T) {
	svc := NewPricingService(true, slog.Default())

	calls := 0
	svc.queryAPI = func(_ context.Context, _, _ string) (float64, error) {
		calls++
		return 0.123, nil
	}

	first := svc.GetRate(context.Background(), "t3.medium", "eu-west-1")
	second := svc.GetRate(context.Background(), "t3.medium", "eu-west-1")

	if first.Source != "aws_pricing_api" || first.HourlyRateUSD != 0.123 {
		t.Fatalf("expected api-sourced rate, got %+v", first)
	}
	if second.HourlyRateUSD != 0.123 {
		t.Fatalf("expected cached rate, got %+v", second)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one API call, got %d", calls)
	}
}

func TestPricingAPIErrorFallsBack(t *testing.T) {
	svc := NewPricingService(true, slog.Default())
	svc.queryAPI = func(_ context.Context, _, _ string) (float64, error) {
		return 0, errors.New("api unavailable")
	}

	rate := svc.GetRate(context.Background(), "t3.micro", "us-east-1")
	if rate.Source != "static_fallback" {
		t.Fatalf("expected fallback when API errors, got %s", rate.Source)
	}
}

func TestParseOnDemandHourlyRate(t *testing.T) {
	doc := `{"terms":{"OnDemand":{"x":{"priceDimensions":{"y":{"unit":"Hrs","pricePerUnit":{"USD":"0.0416"}}}}}}}`
	rate, err := parseOnDemandHourlyRate(doc)
	if err != nil {
		t.Fatalf("expected parse to succeed, got %v", err)
	}
	if rate != 0.0416 {
		t.Fatalf("expected 0.0416, got %f", rate)
	}
}
