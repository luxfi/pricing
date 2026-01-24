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
	// CoinGecko Pro API
	coingeckoBaseURL = "https://pro-api.coingecko.com/api/v3"

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
	CurrentPrice                 float64 `json:"current_price"`
	MarketCap                    float64 `json:"market_cap"`
	TotalVolume                  float64 `json:"total_volume"`
	PriceChangePercentage24h     float64 `json:"price_change_percentage_24h"`
	LastUpdated                  string  `json:"last_updated"`
}

// NewPriceCache creates a new price cache
func NewPriceCache(apiKey string) *PriceCache {
	return &PriceCache{
		prices: make(map[string]*CachedPrice),
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
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
		coingeckoBaseURL, currency, tokenID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-cg-pro-api-key", pc.apiKey)
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
		coingeckoBaseURL, currency, ids)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("x-cg-pro-api-key", pc.apiKey)
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
	// Get API key from environment or use default
	apiKey := os.Getenv("COINGECKO_API_KEY")
	if apiKey == "" {
		apiKey = "***REDACTED***"
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

	// Add CORS middleware
	handler := corsMiddleware(mux)

	log.Printf("Starting pricing API server on port %s", port)
	log.Printf("Cache TTL: %v", cacheTTL)
	log.Printf("Endpoints:")
	log.Printf("  GET /health - Health check")
	log.Printf("  GET /price/{token_id}?currency=usd - Get single token price")
	log.Printf("  GET /prices?ids=bitcoin,ethereum&currency=usd - Get multiple prices")
	log.Printf("  GET /simple/price?ids=bitcoin&vs_currencies=usd - CoinGecko compatible")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
