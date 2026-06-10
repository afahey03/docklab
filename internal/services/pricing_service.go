package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// fallbackHourlyRatesUSD are on-demand Linux rates (us-east-1) used when the AWS
// Pricing API is unavailable or disabled.
var fallbackHourlyRatesUSD = map[string]float64{
	"t3.nano":     0.0052,
	"t3.micro":    0.0104,
	"t3.small":    0.0208,
	"t3.medium":   0.0416,
	"t3.large":    0.0832,
	"t3.xlarge":   0.1664,
	"t3.2xlarge":  0.3328,
	"t2.micro":    0.0116,
	"t2.small":    0.023,
	"t2.medium":   0.0464,
	"m5.large":    0.096,
	"m5.xlarge":   0.192,
	"c5.large":    0.085,
	"c5.xlarge":   0.17,
	"r5.large":    0.126,
	"m7i.large":   0.10081,
	"c7i.large":   0.08925,
	"t4g.micro":   0.0084,
	"t4g.small":   0.0168,
	"t4g.medium":  0.0336,
	"m6i.large":   0.096,
	"m6i.xlarge":  0.192,
	"g4dn.xlarge": 0.526,
}

// regionPricingLocations maps AWS region codes to Pricing API location names.
var regionPricingLocations = map[string]string{
	"us-east-1":      "US East (N. Virginia)",
	"us-east-2":      "US East (Ohio)",
	"us-west-1":      "US West (N. California)",
	"us-west-2":      "US West (Oregon)",
	"eu-west-1":      "EU (Ireland)",
	"eu-west-2":      "EU (London)",
	"eu-central-1":   "EU (Frankfurt)",
	"ap-southeast-1": "Asia Pacific (Singapore)",
	"ap-southeast-2": "Asia Pacific (Sydney)",
	"ap-northeast-1": "Asia Pacific (Tokyo)",
	"ca-central-1":   "Canada (Central)",
	"sa-east-1":      "South America (Sao Paulo)",
}

type InstanceRate struct {
	InstanceType  string  `json:"instance_type"`
	Region        string  `json:"region"`
	HourlyRateUSD float64 `json:"hourly_rate_usd"`
	Source        string  `json:"source"` // "aws_pricing_api" or "static_fallback"
}

// PricingService resolves EC2 on-demand hourly rates, preferring the AWS Pricing API
// with an in-memory cache and falling back to a static table.
type PricingService struct {
	apiEnabled bool
	log        *slog.Logger

	mu    sync.Mutex
	cache map[string]InstanceRate

	queryAPI func(ctx context.Context, instanceType, region string) (float64, error)
}

func NewPricingService(apiEnabled bool, log *slog.Logger) *PricingService {
	s := &PricingService{
		apiEnabled: apiEnabled,
		log:        log,
		cache:      make(map[string]InstanceRate),
	}
	s.queryAPI = s.queryAWSPricingAPI
	return s
}

// GetRate returns the best-known hourly rate for an instance type in a region.
func (s *PricingService) GetRate(ctx context.Context, instanceType, region string) InstanceRate {
	instanceType = strings.ToLower(strings.TrimSpace(instanceType))
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-east-1"
	}

	cacheKey := instanceType + "|" + region

	s.mu.Lock()
	if cached, ok := s.cache[cacheKey]; ok {
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	if s.apiEnabled && instanceType != "" {
		apiCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		rate, err := s.queryAPI(apiCtx, instanceType, region)
		cancel()
		if err == nil && rate > 0 {
			result := InstanceRate{InstanceType: instanceType, Region: region, HourlyRateUSD: rate, Source: "aws_pricing_api"}
			s.mu.Lock()
			s.cache[cacheKey] = result
			s.mu.Unlock()
			return result
		}
		if err != nil {
			s.log.Debug("pricing: AWS Pricing API lookup failed; using static fallback", "instance_type", instanceType, "error", err)
		}
	}

	rate, ok := fallbackHourlyRatesUSD[instanceType]
	result := InstanceRate{InstanceType: instanceType, Region: region, HourlyRateUSD: rate, Source: "static_fallback"}
	if !ok {
		result.HourlyRateUSD = 0
	}
	// Cache fallback results too so unknown types do not re-hit the API on every call.
	s.mu.Lock()
	s.cache[cacheKey] = result
	s.mu.Unlock()
	return result
}

func (s *PricingService) queryAWSPricingAPI(ctx context.Context, instanceType, region string) (float64, error) {
	location, ok := regionPricingLocations[region]
	if !ok {
		return 0, fmt.Errorf("no pricing location mapping for region %s", region)
	}

	// Default credential chain (env keys, shared config, IAM/ECS roles).
	// The Pricing API is only served from us-east-1 and ap-south-1.
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		return 0, fmt.Errorf("load aws config for pricing: %w", err)
	}
	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		return 0, ErrEC2CredentialsMissing
	}

	client := pricing.NewFromConfig(cfg)
	output, err := client.GetProducts(ctx, &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		MaxResults:  aws.Int32(10),
		Filters: []pricingtypes.Filter{
			{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("instanceType"), Value: aws.String(instanceType)},
			{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("operatingSystem"), Value: aws.String("Linux")},
			{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("tenancy"), Value: aws.String("Shared")},
			{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("preInstalledSw"), Value: aws.String("NA")},
			{Type: pricingtypes.FilterTypeTermMatch, Field: aws.String("capacitystatus"), Value: aws.String("Used")},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("pricing GetProducts: %w", err)
	}

	for _, priceItem := range output.PriceList {
		rate, parseErr := parseOnDemandHourlyRate(priceItem)
		if parseErr == nil && rate > 0 {
			return rate, nil
		}
	}

	return 0, fmt.Errorf("no on-demand price found for %s in %s", instanceType, region)
}

// parseOnDemandHourlyRate digs the USD-per-hour figure out of a Pricing API price list
// document.
func parseOnDemandHourlyRate(priceJSON string) (float64, error) {
	var doc struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					Unit         string `json:"unit"`
					PricePerUnit struct {
						USD string `json:"USD"`
					} `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}

	if err := json.Unmarshal([]byte(priceJSON), &doc); err != nil {
		return 0, err
	}

	for _, term := range doc.Terms.OnDemand {
		for _, dimension := range term.PriceDimensions {
			if !strings.EqualFold(dimension.Unit, "Hrs") {
				continue
			}
			rate, err := strconv.ParseFloat(dimension.PricePerUnit.USD, 64)
			if err != nil {
				continue
			}
			if rate > 0 {
				return rate, nil
			}
		}
	}

	return 0, fmt.Errorf("no hourly on-demand dimension in price document")
}
