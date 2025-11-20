// Copyright 2025 Lumina Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nextdoor/lumina/internal/cache"
)

// DebugHandler provides HTTP endpoints for inspecting internal caches.
// These endpoints are useful for debugging and should only be enabled in development/staging.
//
// Available endpoints:
//   - GET /debug/cache/ec2              - List all EC2 instances in cache
//   - GET /debug/cache/risp             - List all RIs and SPs in cache
//   - GET /debug/cache/pricing/ondemand - List all on-demand prices in cache
//   - GET /debug/cache/pricing/sp       - List all SP rates in cache
//   - GET /debug/cache/pricing/sp?sp=<arn> - Filter SP rates by SP ARN
//   - GET /debug/cache/pricing/sp/lookup?instance_type=<type>&region=<region>&tenancy=<tenancy>&os=<os>&sp=<arn> - Lookup specific SP rate
//   - GET /debug/cache/pricing/spot     - List all spot prices in cache
//   - GET /debug/cache/stats            - Show cache statistics
type DebugHandler struct {
	EC2Cache     *cache.EC2Cache
	RISPCache    *cache.RISPCache
	PricingCache *cache.PricingCache
}

// ServeHTTP implements http.Handler interface.
func (h *DebugHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse path to determine which cache to inspect
	path := strings.TrimPrefix(r.URL.Path, "/debug/cache/")

	switch path {
	case "ec2":
		h.handleEC2(w, r)
	case "risp":
		h.handleRISP(w, r)
	case "pricing/ondemand":
		h.handlePricingOnDemand(w, r)
	case "pricing/sp/lookup":
		h.handlePricingSPLookup(w, r)
	case "pricing/sp":
		h.handlePricingSP(w, r)
	case "pricing/spot":
		h.handlePricingSpot(w, r)
	case "stats":
		h.handleStats(w, r)
	default:
		h.handleIndex(w, r)
	}
}

// handleIndex shows available endpoints.
func (h *DebugHandler) handleIndex(w http.ResponseWriter, _ *http.Request) {
	response := map[string]interface{}{
		"endpoints": []string{
			"/debug/cache/ec2              - List all EC2 instances",
			"/debug/cache/risp             - List all RIs and SPs",
			"/debug/cache/pricing/ondemand - List all on-demand prices",
			"/debug/cache/pricing/sp       - List all SP rates",
			"/debug/cache/pricing/sp?sp=<arn> - Filter SP rates by ARN",
			"/debug/cache/pricing/sp/lookup?instance_type=<type>&region=<region>&tenancy=<tenancy>&os=<os>&sp=<arn> - Lookup specific SP rate",
			"/debug/cache/pricing/spot     - List all spot prices",
			"/debug/cache/stats            - Show cache statistics",
		},
	}
	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handleEC2 returns all EC2 instances in cache.
func (h *DebugHandler) handleEC2(w http.ResponseWriter, _ *http.Request) {
	if h.EC2Cache == nil {
		http.Error(w, "EC2 cache not available", http.StatusServiceUnavailable)
		return
	}

	instances := h.EC2Cache.GetAllInstances()
	lastUpdate := h.EC2Cache.GetLastUpdateTime()

	// Group by region for easier inspection
	byRegion := make(map[string][]interface{})
	for _, inst := range instances {
		byRegion[inst.Region] = append(byRegion[inst.Region], map[string]interface{}{
			"instance_id":   inst.InstanceID,
			"instance_type": inst.InstanceType,
			"state":         inst.State,
			"az":            inst.AvailabilityZone,
			"lifecycle":     inst.Lifecycle,
			"platform":      inst.Platform,
			"tenancy":       inst.Tenancy, // Added tenancy field for debugging
		})
	}

	response := map[string]interface{}{
		"total_count": len(instances),
		"last_update": lastUpdate,
		"age_seconds": time.Since(lastUpdate).Seconds(),
		"by_region":   byRegion,
	}

	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handleRISP returns all Reserved Instances and Savings Plans in cache.
func (h *DebugHandler) handleRISP(w http.ResponseWriter, _ *http.Request) {
	if h.RISPCache == nil {
		http.Error(w, "RISP cache not available", http.StatusServiceUnavailable)
		return
	}

	ris := h.RISPCache.GetAllReservedInstances()
	sps := h.RISPCache.GetAllSavingsPlans()
	stats := h.RISPCache.GetStats()

	response := map[string]interface{}{
		"last_update": stats.LastUpdate,
		"age_seconds": time.Since(stats.LastUpdate).Seconds(),
		"reserved_instances": map[string]interface{}{
			"count": len(ris),
			"items": ris,
		},
		"savings_plans": map[string]interface{}{
			"count": len(sps),
			"items": sps,
		},
	}

	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handlePricingOnDemand returns all on-demand prices in cache.
func (h *DebugHandler) handlePricingOnDemand(w http.ResponseWriter, _ *http.Request) {
	if h.PricingCache == nil {
		http.Error(w, "Pricing cache not available", http.StatusServiceUnavailable)
		return
	}

	prices := h.PricingCache.GetAllOnDemandPrices()
	stats := h.PricingCache.GetStats()

	response := map[string]interface{}{
		"total_count": len(prices),
		"last_update": stats.LastUpdated,
		"age_seconds": time.Since(stats.LastUpdated).Seconds(),
		"prices":      prices,
	}

	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handlePricingSpot returns all spot prices in cache with individual timestamps.
func (h *DebugHandler) handlePricingSpot(w http.ResponseWriter, _ *http.Request) {
	if h.PricingCache == nil {
		http.Error(w, "Pricing cache not available", http.StatusServiceUnavailable)
		return
	}

	// Get spot prices with timestamps for debugging
	pricesWithTimestamps := h.PricingCache.GetAllSpotPricesWithTimestamps()
	stats := h.PricingCache.GetSpotStats()

	// Convert to a more readable format for the debug endpoint
	// Each price includes its individual timestamp
	prices := make(map[string]interface{}, len(pricesWithTimestamps))
	for key, sp := range pricesWithTimestamps {
		prices[key] = map[string]interface{}{
			"price":               sp.SpotPrice,
			"timestamp":           sp.Timestamp,
			"age_seconds":         time.Since(sp.Timestamp).Seconds(),
			"availability_zone":   sp.AvailabilityZone,
			"instance_type":       sp.InstanceType,
			"product_description": sp.ProductDescription,
		}
	}

	response := map[string]interface{}{
		"total_count": len(prices),
		"stats": map[string]interface{}{
			"is_populated":       stats.IsPopulated,
			"spot_price_count":   stats.SpotPriceCount,
			"cache_last_updated": stats.LastUpdated,
			"cache_age_seconds":  time.Since(stats.LastUpdated).Seconds(),
		},
		"prices": prices,
	}

	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handlePricingSP returns all SP rates in cache, optionally filtered by SP ARN.
func (h *DebugHandler) handlePricingSP(w http.ResponseWriter, r *http.Request) {
	if h.PricingCache == nil {
		http.Error(w, "Pricing cache not available", http.StatusServiceUnavailable)
		return
	}

	spArn := r.URL.Query().Get("sp")
	allRates := h.PricingCache.GetAllSPRates()
	stats := h.PricingCache.GetSPRateStats()

	if spArn != "" {
		// Filter rates by SP ARN
		filteredRates := make(map[string]float64)
		prefix := strings.ToLower(spArn + ":")
		for key, rate := range allRates {
			if strings.HasPrefix(strings.ToLower(key), prefix) {
				filteredRates[key] = rate
			}
		}

		response := map[string]interface{}{
			"sp_arn":      spArn,
			"total_count": len(filteredRates),
			"last_update": stats.LastUpdated,
			"age_seconds": time.Since(stats.LastUpdated).Seconds(),
			"rates":       filteredRates,
		}
		_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
		return
	}

	// Return all rates grouped by SP ARN
	// Key format: "spArn,instanceType,region,tenancy,os" (comma-separated)
	// Using commas avoids conflicts with colons in AWS ARNs
	bySpArn := make(map[string]map[string]float64)
	for key, rate := range allRates {
		// Split key into parts using comma separator
		parts := strings.Split(key, ",")
		if len(parts) != 5 {
			continue // Invalid key format
		}

		spArn := parts[0]
		instanceType := parts[1]
		region := parts[2]
		tenancy := parts[3]
		os := parts[4]

		// Create the remainder key for display
		remainder := fmt.Sprintf("%s,%s,%s,%s", instanceType, region, tenancy, os)

		if bySpArn[spArn] == nil {
			bySpArn[spArn] = make(map[string]float64)
		}
		bySpArn[spArn][remainder] = rate
	}

	response := map[string]interface{}{
		"total_rates": len(allRates),
		"sp_count":    len(bySpArn),
		"last_update": stats.LastUpdated,
		"age_seconds": time.Since(stats.LastUpdated).Seconds(),
		"by_sp_arn":   bySpArn,
		"key_format":  "spArn,instanceType,region,tenancy,os (comma-separated)",
	}

	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handlePricingSPLookup looks up a specific SP rate for a given instance type, region, tenancy, OS, and SP ARN.
// This is useful for debugging why an instance is getting estimated vs accurate pricing.
//
// Query parameters:
//   - instance_type (required): Instance type to look up (e.g., "m7g.2xlarge")
//   - region (required): AWS region (e.g., "us-west-2")
//   - tenancy (optional): Tenancy value, defaults to "default" (can be "default", "dedicated", "host")
//   - os (optional): Operating system, defaults to "linux" (can be "linux", "windows")
//   - sp (required): Savings Plan ARN to check
//
// Example: /debug/cache/pricing/sp/lookup?instance_type=m7g.2xlarge&region=us-west-2&tenancy=default&os=linux&sp=arn:aws:savingsplans::123456789012:savingsplan/sp-abc123
func (h *DebugHandler) handlePricingSPLookup(w http.ResponseWriter, r *http.Request) {
	if h.PricingCache == nil {
		http.Error(w, "Pricing cache not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	instanceType := r.URL.Query().Get("instance_type")
	region := r.URL.Query().Get("region")
	tenancy := r.URL.Query().Get("tenancy")
	os := r.URL.Query().Get("os")
	spArn := r.URL.Query().Get("sp")

	// Validate required parameters
	if instanceType == "" {
		http.Error(w, "Missing required parameter: instance_type", http.StatusBadRequest)
		return
	}
	if region == "" {
		http.Error(w, "Missing required parameter: region", http.StatusBadRequest)
		return
	}
	if spArn == "" {
		http.Error(w, "Missing required parameter: sp", http.StatusBadRequest)
		return
	}

	// Default tenancy to "default" if not specified
	if tenancy == "" {
		tenancy = "default"
	}

	// Default OS to "linux" if not specified
	if os == "" {
		os = "linux"
	}

	// Perform the lookup using the pricing cache
	rate, found := h.PricingCache.GetSPRate(spArn, instanceType, region, tenancy, os)

	// Build the expected cache key for debugging (using comma separator)
	expectedKey := fmt.Sprintf("%s,%s,%s,%s,%s", spArn, instanceType, region, tenancy, os)
	normalizedKey := strings.ToLower(expectedKey)

	response := map[string]interface{}{
		"query": map[string]string{
			"instance_type": instanceType,
			"region":        region,
			"tenancy":       tenancy,
			"os":            os,
			"sp_arn":        spArn,
		},
		"expected_cache_key": expectedKey,
		"normalized_key":     normalizedKey,
		"found":              found,
	}

	if found {
		response["rate"] = rate
		response["pricing_accuracy"] = "accurate"
		response["message"] = "Found accurate SP rate in cache"
	} else {
		response["rate"] = nil
		response["pricing_accuracy"] = "estimated"
		response["message"] = "SP rate not found in cache - would use estimated pricing"

		// Try to provide helpful debugging info
		allRates := h.PricingCache.GetAllSPRates()
		similarKeys := []string{}
		for key := range allRates {
			// Find keys with same SP ARN (using comma separator)
			if strings.HasPrefix(strings.ToLower(key), strings.ToLower(spArn+",")) {
				similarKeys = append(similarKeys, key)
			}
		}
		if len(similarKeys) > 0 {
			response["similar_keys_found"] = similarKeys
			response["debug_hint"] = "Found rates for this SP ARN but with different instance_type/region/tenancy combination"
		} else {
			response["debug_hint"] = "No rates found for this SP ARN at all - SP may not be in cache yet"
		}
	}

	_ = json.NewEncoder(w).Encode(response) // Best-effort encoding for debug endpoint
}

// handleStats returns cache statistics.
func (h *DebugHandler) handleStats(w http.ResponseWriter, _ *http.Request) {
	stats := make(map[string]interface{})

	if h.EC2Cache != nil {
		instances := h.EC2Cache.GetAllInstances()
		stats["ec2"] = map[string]interface{}{
			"total_instances": len(instances),
		}
	}

	if h.RISPCache != nil {
		ris := h.RISPCache.GetAllReservedInstances()
		sps := h.RISPCache.GetAllSavingsPlans()
		stats["risp"] = map[string]interface{}{
			"reserved_instances": len(ris),
			"savings_plans":      len(sps),
		}
	}

	if h.PricingCache != nil {
		onDemand := h.PricingCache.GetAllOnDemandPrices()
		spRates := h.PricingCache.GetAllSPRates()
		spotStats := h.PricingCache.GetSpotStats()
		stats["pricing"] = map[string]interface{}{
			"ondemand_prices": len(onDemand),
			"sp_rates":        len(spRates),
			"spot_prices":     spotStats.SpotPriceCount,
		}
	}

	_ = json.NewEncoder(w).Encode(stats) // Best-effort encoding for debug endpoint
}

// NewDebugHandler creates a new DebugHandler with the provided caches.
func NewDebugHandler(
	ec2Cache *cache.EC2Cache,
	rispCache *cache.RISPCache,
	pricingCache *cache.PricingCache,
) *DebugHandler {
	return &DebugHandler{
		EC2Cache:     ec2Cache,
		RISPCache:    rispCache,
		PricingCache: pricingCache,
	}
}

// RegisterDebugEndpoints registers debug endpoints on the provided HTTP server.
// This should be called from main() or cmd/ setup code.
//
// Example usage:
//
//	mux := http.NewServeMux()
//	controller.RegisterDebugEndpoints(mux, ec2Cache, rispCache, pricingCache)
//	http.ListenAndServe(":8080", mux)
func RegisterDebugEndpoints(
	mux *http.ServeMux,
	ec2Cache *cache.EC2Cache,
	rispCache *cache.RISPCache,
	pricingCache *cache.PricingCache,
) {
	handler := NewDebugHandler(ec2Cache, rispCache, pricingCache)

	// Register all debug endpoints under /debug/cache/
	mux.HandleFunc("/debug/cache/", func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET requests
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler.ServeHTTP(w, r)
	})

	fmt.Println("Registered debug endpoints:")
	fmt.Println("  GET /debug/cache/              - Index of available endpoints")
	fmt.Println("  GET /debug/cache/ec2           - List all EC2 instances")
	fmt.Println("  GET /debug/cache/risp          - List all RIs and SPs")
	fmt.Println("  GET /debug/cache/pricing/ondemand - List all on-demand prices")
	fmt.Println("  GET /debug/cache/pricing/sp    - List all SP rates")
	fmt.Println("  GET /debug/cache/pricing/sp?sp=<arn> - Filter SP rates by ARN")
	fmt.Println("  GET /debug/cache/pricing/sp/lookup?instance_type=<type>&region=<region>&tenancy=<tenancy>&os=<os>&sp=<arn> - Lookup specific SP rate")
	fmt.Println("  GET /debug/cache/stats         - Show cache statistics")
}
