# Lux Pricing API

Cryptocurrency pricing API gateway with CoinGecko Pro integration and intelligent caching.

## Features

- CoinGecko Pro API integration
- 1-hour cache TTL to minimize API calls
- CoinGecko-compatible `/simple/price` endpoint
- CORS support
- Docker-ready deployment

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /price/{token_id}?currency=usd` | Single token price |
| `GET /prices?ids=bitcoin,ethereum&currency=usd` | Multiple token prices |
| `GET /simple/price?ids=bitcoin&vs_currencies=usd` | CoinGecko-compatible format |

## Usage

```bash
# Single token
curl https://fx.lux.network/price/bitcoin

# Multiple tokens
curl "https://fx.lux.network/prices?ids=bitcoin,ethereum,solana&currency=usd"

# CoinGecko compatible
curl "https://fx.lux.network/simple/price?ids=bitcoin&vs_currencies=usd,eur"
```

## Response Format

```json
{
  "id": "bitcoin",
  "symbol": "btc",
  "name": "Bitcoin",
  "price": 97234.56,
  "currency": "usd",
  "change_24h": 2.34,
  "market_cap": 1923456789012,
  "volume_24h": 45678901234,
  "updated_at": "2025-01-24T12:00:00Z",
  "cached": true
}
```

## Development

```bash
# Run locally
export COINGECKO_API_KEY=your-api-key
go run main.go

# Docker
docker compose up -d
```

## Deployment

Automatically deploys to fx.lux.network on push to main branch.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `COINGECKO_API_KEY` | - | CoinGecko Pro API key |
| `PORT` | 8080 | Server port |

## License

MIT © Lux Partners Limited
