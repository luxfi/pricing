// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// CoinGecko API URLs
	coingeckoProURL  = "https://pro-api.coingecko.com/api/v3"
	coingeckoDemoURL = "https://api.coingecko.com/api/v3"

	// Cache TTL - 1 hour
	cacheTTL = 1 * time.Hour

	// Default port
	defaultPort = "8080"
)

// PriceCache holds cached price data
type PriceCache struct {
	mu        sync.RWMutex
	prices    map[string]*CachedPrice
	apiKey    string
	baseURL   string
	client    *http.Client
}

// CachedPrice holds a single cached price entry
type CachedPrice struct {
	Price     float64   `json:"price"`
	Currency  string    `json:"currency"`
	UpdatedAt time.Time `json:"updated_at"`
	Change24h float64   `json:"change_24h,omitempty"`
	MarketCap float64   `json:"market_cap,omitempty"`
	Volume24h float64   `json:"volume_24h,omitempty"`
}

// PriceResponse is the API response format
type PriceResponse struct {
	ID        string    `json:"id"`
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Price     float64   `json:"price"`
	Currency  string    `json:"currency"`
	Change24h float64   `json:"change_24h"`
	MarketCap float64   `json:"market_cap"`
	Volume24h float64   `json:"volume_24h"`
	UpdatedAt time.Time `json:"updated_at"`
	Cached    bool      `json:"cached"`
}

// MultiPriceResponse for multiple tokens
type MultiPriceResponse struct {
	Prices    map[string]*PriceResponse `json:"prices"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

// CoinGecko API response structures
type CoinGeckoPrice struct {
	ID                           string  `json:"id"`
	Symbol                       string  `json:"symbol"`
	Name                         string  `json:"name"`
	Image                        string  `json:"image"`
	CurrentPrice                 float64 `json:"current_price"`
	MarketCap                    float64 `json:"market_cap"`
	MarketCapRank                int     `json:"market_cap_rank"`
	FullyDilutedValuation        float64 `json:"fully_diluted_valuation"`
	TotalVolume                  float64 `json:"total_volume"`
	High24h                      float64 `json:"high_24h"`
	Low24h                       float64 `json:"low_24h"`
	PriceChange24h               float64 `json:"price_change_24h"`
	PriceChangePercentage24h     float64 `json:"price_change_percentage_24h"`
	PriceChangePercentage7d      float64 `json:"price_change_percentage_7d_in_currency"`
	MarketCapChange24h           float64 `json:"market_cap_change_24h"`
	MarketCapChangePercentage24h float64 `json:"market_cap_change_percentage_24h"`
	CirculatingSupply            float64 `json:"circulating_supply"`
	TotalSupply                  float64 `json:"total_supply"`
	MaxSupply                    float64 `json:"max_supply"`
	ATH                          float64 `json:"ath"`
	ATHChangePercentage          float64 `json:"ath_change_percentage"`
	ATL                          float64 `json:"atl"`
	ATLChangePercentage          float64 `json:"atl_change_percentage"`
	LastUpdated                  string  `json:"last_updated"`
}

// StakingData holds staking-specific information
type StakingData struct {
	APY                float64 `json:"apy"`
	APYChange7d        float64 `json:"apy_change_7d"`
	StakingRatio       float64 `json:"staking_ratio"`
	StakedTokens       float64 `json:"staked_tokens"`
	StakedTokensChange float64 `json:"staked_tokens_change_7d"`
	TVL                float64 `json:"tvl"`
	TVLChange7d        float64 `json:"tvl_change_7d"`
	ValidatorFee       float64 `json:"validator_fee"`
	MinStake           float64 `json:"min_stake"`
	UnbondingDays      int     `json:"unbonding_days"`
	ValidatorCount     int     `json:"validator_count"`
}

// MarketAsset represents comprehensive asset data
type MarketAsset struct {
	ID                    string       `json:"id"`
	Symbol                string       `json:"symbol"`
	Name                  string       `json:"name"`
	Image                 string       `json:"image"`
	Price                 float64      `json:"price"`
	PriceChange24h        float64      `json:"price_change_24h"`
	PriceChange7d         float64      `json:"price_change_7d"`
	MarketCap             float64      `json:"market_cap"`
	MarketCapRank         int          `json:"market_cap_rank"`
	Volume24h             float64      `json:"volume_24h"`
	CirculatingSupply     float64      `json:"circulating_supply"`
	TotalSupply           float64      `json:"total_supply"`
	ATH                   float64      `json:"ath"`
	ATHChangePercentage   float64      `json:"ath_change_percentage"`
	Staking               *StakingData `json:"staking,omitempty"`
	Score                 float64      `json:"score"`
	ScoreBreakdown        ScoreData    `json:"score_breakdown"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

// ScoreData breaks down the asset score
type ScoreData struct {
	MarketScore    float64 `json:"market_score"`
	StakingScore   float64 `json:"staking_score"`
	SecurityScore  float64 `json:"security_score"`
	AdoptionScore  float64 `json:"adoption_score"`
	TechScore      float64 `json:"tech_score"`
}

// StakingRewardsData from external staking APIs
var stakingDataCache = map[string]*StakingData{
	"ethereum":          {APY: 3.13, StakingRatio: 30.46, ValidatorFee: 0, UnbondingDays: 27, MinStake: 32},
	"solana":            {APY: 6.15, StakingRatio: 68.65, ValidatorFee: 8, UnbondingDays: 3, MinStake: 0.01},
	"binancecoin":       {APY: 5.01, StakingRatio: 18.45, ValidatorFee: 0, UnbondingDays: 7, MinStake: 1},
	"cardano":           {APY: 2.28, StakingRatio: 58.13, ValidatorFee: 2, UnbondingDays: 20, MinStake: 10},
	"avalanche-2":       {APY: 7.00, StakingRatio: 48.38, ValidatorFee: 2, UnbondingDays: 14, MinStake: 25},
	"polkadot":          {APY: 11.68, StakingRatio: 52.59, ValidatorFee: 10, UnbondingDays: 28, MinStake: 120},
	"cosmos":            {APY: 20.21, StakingRatio: 61.02, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"near":              {APY: 4.47, StakingRatio: 47.20, ValidatorFee: 0, UnbondingDays: 2, MinStake: 0.01},
	"aptos":             {APY: 7.00, StakingRatio: 96.73, ValidatorFee: 0, UnbondingDays: 30, MinStake: 10},
	"sui":               {APY: 1.75, StakingRatio: 74.45, ValidatorFee: 2, UnbondingDays: 0, MinStake: 1},
	"celestia":          {APY: 6.45, StakingRatio: 35.49, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"injective-protocol":{APY: 6.57, StakingRatio: 55.71, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"sei-network":       {APY: 7.39, StakingRatio: 36.36, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"the-open-network":  {APY: 3.99, StakingRatio: 6.78, ValidatorFee: 0, UnbondingDays: 36, MinStake: 10000},
	"hedera-hashgraph":  {APY: 2.50, StakingRatio: 31.87, ValidatorFee: 0, UnbondingDays: 0, MinStake: 0},
	"filecoin":          {APY: 12.24, StakingRatio: 1.29, ValidatorFee: 0, UnbondingDays: 0, MinStake: 0},
	"fantom":            {APY: 4.00, StakingRatio: 40.00, ValidatorFee: 15, UnbondingDays: 7, MinStake: 1},
	"crypto-com-chain":  {APY: 1.79, StakingRatio: 13.44, ValidatorFee: 0, UnbondingDays: 28, MinStake: 5000},
	"moonbeam":          {APY: 56.94, StakingRatio: 22.13, ValidatorFee: 20, UnbondingDays: 7, MinStake: 50},
	"kava":              {APY: 9.88, StakingRatio: 9.34, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"osmosis":           {APY: 1.95, StakingRatio: 34.94, ValidatorFee: 5, UnbondingDays: 14, MinStake: 0.01},
	"secret":            {APY: 24.00, StakingRatio: 42.11, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"akash-network":     {APY: 10.74, StakingRatio: 37.80, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"starknet":          {APY: 8.83, StakingRatio: 21.86, ValidatorFee: 0, UnbondingDays: 21, MinStake: 20000},
	"dydx-chain":        {APY: 2.74, StakingRatio: 22.48, ValidatorFee: 5, UnbondingDays: 30, MinStake: 0.01},
	"axelar":            {APY: 10.49, StakingRatio: 36.31, ValidatorFee: 5, UnbondingDays: 7, MinStake: 0.01},
	"band-protocol":     {APY: 18.45, StakingRatio: 52.92, ValidatorFee: 3, UnbondingDays: 21, MinStake: 0.01},
	"livepeer":          {APY: 51.05, StakingRatio: 53.58, ValidatorFee: 0, UnbondingDays: 7, MinStake: 0.01},
	"radix":             {APY: 6.77, StakingRatio: 33.20, ValidatorFee: 2, UnbondingDays: 14, MinStake: 0.01},
	"waves":             {APY: 5.09, StakingRatio: 17.44, ValidatorFee: 0, UnbondingDays: 0, MinStake: 0.01},
	"casper-network":    {APY: 16.74, StakingRatio: 49.47, ValidatorFee: 5, UnbondingDays: 14, MinStake: 500},
	"tron":              {APY: 3.25, StakingRatio: 46.48, ValidatorFee: 0, UnbondingDays: 14, MinStake: 0.01},
	"bittensor":         {APY: 14.73, StakingRatio: 76.22, ValidatorFee: 18, UnbondingDays: 0, MinStake: 0.01},
	"elrond-erd-2":      {APY: 8.61, StakingRatio: 48.65, ValidatorFee: 0, UnbondingDays: 10, MinStake: 1},
	"iota":              {APY: 11.55, StakingRatio: 50.53, ValidatorFee: 0, UnbondingDays: 0, MinStake: 0},
	"blockstack":        {APY: 9.70, StakingRatio: 29.58, ValidatorFee: 0, UnbondingDays: 14, MinStake: 90},
	"fetch-ai":          {APY: 5.46, StakingRatio: 22.12, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"zetachain":         {APY: 6.98, StakingRatio: 27.52, ValidatorFee: 5, UnbondingDays: 21, MinStake: 0.01},
	"skale":             {APY: 10.00, StakingRatio: 32.37, ValidatorFee: 0, UnbondingDays: 7, MinStake: 0.01},
	"tezos":             {APY: 8.51, StakingRatio: 58.80, ValidatorFee: 5, UnbondingDays: 0, MinStake: 1},
	"algorand":          {APY: 4.95, StakingRatio: 22.52, ValidatorFee: 0, UnbondingDays: 0, MinStake: 0.01},
	"harmony":           {APY: 12.10, StakingRatio: 20.15, ValidatorFee: 5, UnbondingDays: 7, MinStake: 100},
}

// NewPriceCache creates a new price cache
func NewPriceCache(apiKey string) *PriceCache {
	// Detect API type from key prefix
	// Pro keys start with "CG-" followed by alphanumeric
	// Demo keys also start with "CG-" but use demo API
	// If no key, use demo API
	baseURL := coingeckoDemoURL
	if apiKey != "" && strings.HasPrefix(apiKey, "CG-") && len(apiKey) > 10 {
		// Check if it's a pro key by trying pro first
		// For now, assume demo unless explicitly marked
		baseURL = coingeckoDemoURL
	}

	return &PriceCache{
		prices:  make(map[string]*CachedPrice),
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// GetPrice returns the price for a token, fetching if cache expired
func (pc *PriceCache) GetPrice(ctx context.Context, tokenID, currency string) (*PriceResponse, error) {
	cacheKey := fmt.Sprintf("%s:%s", tokenID, currency)

	// Check cache first
	pc.mu.RLock()
	cached, exists := pc.prices[cacheKey]
	pc.mu.RUnlock()

	if exists && time.Since(cached.UpdatedAt) < cacheTTL {
		return &PriceResponse{
			ID:        tokenID,
			Price:     cached.Price,
			Currency:  cached.Currency,
			Change24h: cached.Change24h,
			MarketCap: cached.MarketCap,
			Volume24h: cached.Volume24h,
			UpdatedAt: cached.UpdatedAt,
			Cached:    true,
		}, nil
	}

	// Fetch from CoinGecko
	price, err := pc.fetchFromCoinGecko(ctx, tokenID, currency)
	if err != nil {
		// Return stale cache if available
		if exists {
			return &PriceResponse{
				ID:        tokenID,
				Price:     cached.Price,
				Currency:  cached.Currency,
				Change24h: cached.Change24h,
				MarketCap: cached.MarketCap,
				Volume24h: cached.Volume24h,
				UpdatedAt: cached.UpdatedAt,
				Cached:    true,
			}, nil
		}
		return nil, err
	}

	// Update cache
	pc.mu.Lock()
	pc.prices[cacheKey] = &CachedPrice{
		Price:     price.CurrentPrice,
		Currency:  currency,
		UpdatedAt: time.Now(),
		Change24h: price.PriceChangePercentage24h,
		MarketCap: price.MarketCap,
		Volume24h: price.TotalVolume,
	}
	pc.mu.Unlock()

	return &PriceResponse{
		ID:        tokenID,
		Symbol:    price.Symbol,
		Name:      price.Name,
		Price:     price.CurrentPrice,
		Currency:  currency,
		Change24h: price.PriceChangePercentage24h,
		MarketCap: price.MarketCap,
		Volume24h: price.TotalVolume,
		UpdatedAt: time.Now(),
		Cached:    false,
	}, nil
}

// GetMultiplePrices fetches prices for multiple tokens
func (pc *PriceCache) GetMultiplePrices(ctx context.Context, tokenIDs []string, currency string) (*MultiPriceResponse, error) {
	response := &MultiPriceResponse{
		Prices:    make(map[string]*PriceResponse),
		UpdatedAt: time.Now(),
	}

	// Check which tokens need fetching
	var toFetch []string
	for _, id := range tokenIDs {
		cacheKey := fmt.Sprintf("%s:%s", id, currency)

		pc.mu.RLock()
		cached, exists := pc.prices[cacheKey]
		pc.mu.RUnlock()

		if exists && time.Since(cached.UpdatedAt) < cacheTTL {
			response.Prices[id] = &PriceResponse{
				ID:        id,
				Price:     cached.Price,
				Currency:  cached.Currency,
				Change24h: cached.Change24h,
				MarketCap: cached.MarketCap,
				Volume24h: cached.Volume24h,
				UpdatedAt: cached.UpdatedAt,
				Cached:    true,
			}
		} else {
			toFetch = append(toFetch, id)
		}
	}

	// Fetch missing prices in batch
	if len(toFetch) > 0 {
		prices, err := pc.fetchMultipleFromCoinGecko(ctx, toFetch, currency)
		if err != nil {
			log.Printf("Error fetching prices: %v", err)
		} else {
			for _, p := range prices {
				cacheKey := fmt.Sprintf("%s:%s", p.ID, currency)

				pc.mu.Lock()
				pc.prices[cacheKey] = &CachedPrice{
					Price:     p.CurrentPrice,
					Currency:  currency,
					UpdatedAt: time.Now(),
					Change24h: p.PriceChangePercentage24h,
					MarketCap: p.MarketCap,
					Volume24h: p.TotalVolume,
				}
				pc.mu.Unlock()

				response.Prices[p.ID] = &PriceResponse{
					ID:        p.ID,
					Symbol:    p.Symbol,
					Name:      p.Name,
					Price:     p.CurrentPrice,
					Currency:  currency,
					Change24h: p.PriceChangePercentage24h,
					MarketCap: p.MarketCap,
					Volume24h: p.TotalVolume,
					UpdatedAt: time.Now(),
					Cached:    false,
				}
			}
		}
	}

	return response, nil
}

// fetchFromCoinGecko fetches a single price from CoinGecko
func (pc *PriceCache) fetchFromCoinGecko(ctx context.Context, tokenID, currency string) (*CoinGeckoPrice, error) {
	url := fmt.Sprintf("%s/coins/markets?vs_currency=%s&ids=%s&order=market_cap_desc&per_page=1&page=1&sparkline=false",
		pc.baseURL, currency, tokenID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-cg-demo-api-key", pc.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CoinGecko API error: %d - %s", resp.StatusCode, string(body))
	}

	var prices []CoinGeckoPrice
	if err := json.NewDecoder(resp.Body).Decode(&prices); err != nil {
		return nil, err
	}

	if len(prices) == 0 {
		return nil, fmt.Errorf("token not found: %s", tokenID)
	}

	return &prices[0], nil
}

// fetchMultipleFromCoinGecko fetches multiple prices in one request
func (pc *PriceCache) fetchMultipleFromCoinGecko(ctx context.Context, tokenIDs []string, currency string) ([]CoinGeckoPrice, error) {
	ids := strings.Join(tokenIDs, ",")
	url := fmt.Sprintf("%s/coins/markets?vs_currency=%s&ids=%s&order=market_cap_desc&per_page=250&page=1&sparkline=false",
		pc.baseURL, currency, ids)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-cg-demo-api-key", pc.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CoinGecko API error: %d - %s", resp.StatusCode, string(body))
	}

	var prices []CoinGeckoPrice
	if err := json.NewDecoder(resp.Body).Decode(&prices); err != nil {
		return nil, err
	}

	return prices, nil
}

// Server holds the HTTP server and price cache
type Server struct {
	cache *PriceCache
}

// NewServer creates a new server
func NewServer(apiKey string) *Server {
	return &Server{
		cache: NewPriceCache(apiKey),
	}
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// handlePrice returns price for a single token
func (s *Server) handlePrice(w http.ResponseWriter, r *http.Request) {
	// Parse token ID from path: /price/{token_id}
	path := strings.TrimPrefix(r.URL.Path, "/price/")
	tokenID := strings.TrimSuffix(path, "/")

	if tokenID == "" {
		http.Error(w, `{"error":"token_id required"}`, http.StatusBadRequest)
		return
	}

	// Get currency from query param, default to usd
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "usd"
	}

	price, err := s.cache.GetPrice(r.Context(), tokenID, currency)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(price)
}

// handlePrices returns prices for multiple tokens
func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	// Get token IDs from query param
	ids := r.URL.Query().Get("ids")
	if ids == "" {
		http.Error(w, `{"error":"ids query parameter required"}`, http.StatusBadRequest)
		return
	}

	tokenIDs := strings.Split(ids, ",")

	// Get currency from query param, default to usd
	currency := r.URL.Query().Get("currency")
	if currency == "" {
		currency = "usd"
	}

	prices, err := s.cache.GetMultiplePrices(r.Context(), tokenIDs, currency)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(prices)
}

// handleSimplePrice returns simple price map (CoinGecko compatible)
func (s *Server) handleSimplePrice(w http.ResponseWriter, r *http.Request) {
	ids := r.URL.Query().Get("ids")
	if ids == "" {
		http.Error(w, `{"error":"ids query parameter required"}`, http.StatusBadRequest)
		return
	}

	vsCurrencies := r.URL.Query().Get("vs_currencies")
	if vsCurrencies == "" {
		vsCurrencies = "usd"
	}

	tokenIDs := strings.Split(ids, ",")
	currencies := strings.Split(vsCurrencies, ",")

	result := make(map[string]map[string]float64)

	for _, currency := range currencies {
		prices, _ := s.cache.GetMultiplePrices(r.Context(), tokenIDs, currency)
		for id, p := range prices.Prices {
			if result[id] == nil {
				result[id] = make(map[string]float64)
			}
			result[id][currency] = p.Price
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	json.NewEncoder(w).Encode(result)
}

// calculateScore computes a comprehensive asset score (0-100)
func calculateScore(price *CoinGeckoPrice, staking *StakingData) (float64, ScoreData) {
	breakdown := ScoreData{}

	// Market Score (25 points max) - based on market cap rank and volume
	if price.MarketCapRank > 0 && price.MarketCapRank <= 10 {
		breakdown.MarketScore = 25
	} else if price.MarketCapRank <= 25 {
		breakdown.MarketScore = 22
	} else if price.MarketCapRank <= 50 {
		breakdown.MarketScore = 18
	} else if price.MarketCapRank <= 100 {
		breakdown.MarketScore = 14
	} else if price.MarketCapRank <= 250 {
		breakdown.MarketScore = 10
	} else {
		breakdown.MarketScore = 5
	}

	// Staking Score (25 points max) - based on APY and reliability
	if staking != nil {
		if staking.APY >= 10 {
			breakdown.StakingScore = 20
		} else if staking.APY >= 5 {
			breakdown.StakingScore = 15
		} else if staking.APY >= 2 {
			breakdown.StakingScore = 10
		} else {
			breakdown.StakingScore = 5
		}
		// Bonus for high staking ratio (network security)
		if staking.StakingRatio >= 50 {
			breakdown.StakingScore += 5
		}
	}

	// Security Score (20 points max) - based on network maturity and staking ratio
	if price.MarketCap > 10000000000 { // > $10B
		breakdown.SecurityScore = 20
	} else if price.MarketCap > 1000000000 { // > $1B
		breakdown.SecurityScore = 16
	} else if price.MarketCap > 100000000 { // > $100M
		breakdown.SecurityScore = 12
	} else {
		breakdown.SecurityScore = 8
	}

	// Adoption Score (15 points max) - based on volume and supply distribution
	volumeToMcap := price.TotalVolume / price.MarketCap
	if volumeToMcap > 0.1 {
		breakdown.AdoptionScore = 15
	} else if volumeToMcap > 0.05 {
		breakdown.AdoptionScore = 12
	} else if volumeToMcap > 0.01 {
		breakdown.AdoptionScore = 9
	} else {
		breakdown.AdoptionScore = 5
	}

	// Tech Score (15 points max) - based on ATH recovery and market position
	if price.ATHChangePercentage > -20 {
		breakdown.TechScore = 15
	} else if price.ATHChangePercentage > -50 {
		breakdown.TechScore = 12
	} else if price.ATHChangePercentage > -80 {
		breakdown.TechScore = 8
	} else {
		breakdown.TechScore = 4
	}

	total := breakdown.MarketScore + breakdown.StakingScore + breakdown.SecurityScore + breakdown.AdoptionScore + breakdown.TechScore
	return total, breakdown
}

// handleMarkets returns comprehensive market data with staking info
func (s *Server) handleMarkets(w http.ResponseWriter, r *http.Request) {
	// Get all staking tokens
	var tokenIDs []string
	for id := range stakingDataCache {
		tokenIDs = append(tokenIDs, id)
	}

	// Fetch all prices with extended data
	url := fmt.Sprintf("%s/coins/markets?vs_currency=usd&ids=%s&order=market_cap_desc&per_page=250&page=1&sparkline=false&price_change_percentage=7d",
		s.cache.baseURL, strings.Join(tokenIDs, ","))

	req, err := http.NewRequestWithContext(r.Context(), "GET", url, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	req.Header.Set("x-cg-demo-api-key", s.cache.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.cache.client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var cgPrices []CoinGeckoPrice
	if err := json.NewDecoder(resp.Body).Decode(&cgPrices); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Build comprehensive market assets
	var assets []MarketAsset
	for _, p := range cgPrices {
		staking := stakingDataCache[p.ID]

		// Calculate TVL and staked tokens if we have staking data
		var stakingData *StakingData
		if staking != nil {
			stakedTokens := p.CirculatingSupply * (staking.StakingRatio / 100)
			tvl := stakedTokens * p.CurrentPrice
			stakingData = &StakingData{
				APY:            staking.APY,
				StakingRatio:   staking.StakingRatio,
				StakedTokens:   stakedTokens,
				TVL:            tvl,
				ValidatorFee:   staking.ValidatorFee,
				MinStake:       staking.MinStake,
				UnbondingDays:  staking.UnbondingDays,
			}
		}

		score, breakdown := calculateScore(&p, staking)

		assets = append(assets, MarketAsset{
			ID:                  p.ID,
			Symbol:              strings.ToUpper(p.Symbol),
			Name:                p.Name,
			Image:               p.Image,
			Price:               p.CurrentPrice,
			PriceChange24h:      p.PriceChangePercentage24h,
			PriceChange7d:       p.PriceChangePercentage7d,
			MarketCap:           p.MarketCap,
			MarketCapRank:       p.MarketCapRank,
			Volume24h:           p.TotalVolume,
			CirculatingSupply:   p.CirculatingSupply,
			TotalSupply:         p.TotalSupply,
			ATH:                 p.ATH,
			ATHChangePercentage: p.ATHChangePercentage,
			Staking:             stakingData,
			Score:               score,
			ScoreBreakdown:      breakdown,
			UpdatedAt:           time.Now(),
		})
	}

	// Sort by score
	for i := 0; i < len(assets)-1; i++ {
		for j := i + 1; j < len(assets); j++ {
			if assets[j].Score > assets[i].Score {
				assets[i], assets[j] = assets[j], assets[i]
			}
		}
	}

	response := map[string]interface{}{
		"assets":     assets,
		"count":      len(assets),
		"updated_at": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300") // 5 min cache
	json.NewEncoder(w).Encode(response)
}

// handleStaking returns staking-specific data
func (s *Server) handleStaking(w http.ResponseWriter, r *http.Request) {
	// Reuse markets endpoint but filter for staking assets
	s.handleMarkets(w, r)
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	// Get API key from environment (required)
	apiKey := os.Getenv("COINGECKO_API_KEY")
	if apiKey == "" {
		log.Fatal("COINGECKO_API_KEY environment variable is required")
	}

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	server := NewServer(apiKey)

	// Set up routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/price/", server.handlePrice)
	mux.HandleFunc("/prices", server.handlePrices)
	mux.HandleFunc("/simple/price", server.handleSimplePrice)
	mux.HandleFunc("/v1/markets", server.handleMarkets)
	mux.HandleFunc("/v1/staking", server.handleStaking)
	mux.HandleFunc("/markets", server.handleMarkets)
	mux.HandleFunc("/staking", server.handleStaking)

	// Add CORS middleware
	handler := corsMiddleware(mux)

	log.Printf("Starting pricing API server on port %s", port)
	log.Printf("Cache TTL: %v", cacheTTL)
	log.Printf("Endpoints:")
	log.Printf("  GET /health - Health check")
	log.Printf("  GET /price/{token_id}?currency=usd - Get single token price")
	log.Printf("  GET /prices?ids=bitcoin,ethereum&currency=usd - Get multiple prices")
	log.Printf("  GET /simple/price?ids=bitcoin&vs_currencies=usd - CoinGecko compatible")
	log.Printf("  GET /v1/markets - Full market data with staking info and scores")
	log.Printf("  GET /v1/staking - Staking rewards and TVL data")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
